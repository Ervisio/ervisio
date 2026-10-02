import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react';
import { useNavigate } from 'react-router-dom';
import { ApiError, call, fromBase64, stream, toBase64, useSession, type StreamHandle } from '../api';
import { apiUrl, FETCH_CREDENTIALS } from '../api/base';
import { useI18n, useT } from '../i18n';
import { themeVars, useTheme } from '../theme';
import { EmptyState, Skeleton, toast } from '../ui';
import { authorize, authorizeHttpStream, authorizePty, authorizeStream, type BrokerUser, type Plan } from './broker';
import { isFrameMessage, type FrameError, type FrameHttpResult, type FrameTheme, type FrameToHost, type FrameView, type HostToFrame } from './protocol';
import { usePlugins } from './PluginsProvider';
import type { PluginManifest } from './types';

// Per frame. A page may follow many containers' stats at once; the app's WebSocket allows 128 channels per session.
const MAX_STREAMS = 32;
const MAX_PENDING = 32;
const WIDGET_MIN = 48;
const WIDGET_MAX = 720;

const toFrameError = (e: unknown): FrameError =>
  e instanceof ApiError ? { code: String(e.code), message: e.message } : { code: 'internal', message: e instanceof Error ? e.message : String(e) };

const encodeAsset = (p: string) => p.split('/').map(encodeURIComponent).join('/');

/** plugins.http result → what the frame gets: text for UTF-8 bodies, a Uint8Array for binary ones. */
const toHttpResult = (r: { status: number; headers?: Record<string, string>; body?: string; b64?: boolean }): FrameHttpResult =>
  r.b64 ? { status: r.status, headers: r.headers ?? {}, bytes: fromBase64(r.body ?? '') } : { status: r.status, headers: r.headers ?? {}, text: r.body ?? '' };

/**
 * Hosts one view (page or widget) of a plugin in an <iframe sandbox="allow-scripts allow-forms"> served by the daemon at
 * /plugin-frame/<id>. The frame has an opaque origin: it cannot read cookies, the app's DOM or storage, or call
 * the API. Everything it needs goes through this component, which applies the broker policy (./broker.ts) and
 * then calls the daemon, which checks the manifest again.
 */
export function PluginFrame(props: FrameProps) {
  const { generation } = usePlugins();
  // A new frame (fresh sandbox, fresh code) for every plugin/view and every plugin reload.
  return <FrameSession key={`${props.plugin.id}/${props.view.kind}/${props.view.id}/${generation}`} {...props} />;
}

interface FrameProps {
  plugin: PluginManifest;
  view: FrameView;
  title?: string;
  style?: CSSProperties;
}

function FrameSession({ plugin, view, title, style }: FrameProps) {
  const t = useT('plugins');
  const nav = useNavigate();
  const { session } = useSession();
  const { lang } = useI18n();
  const th = useTheme();
  const { reportError } = usePlugins();
  const frameRef = useRef<HTMLIFrameElement>(null);
  const [phase, setPhase] = useState<'starting' | 'running' | 'failed'>('starting');
  const [failure, setFailure] = useState('');
  const [height, setHeight] = useState(120);

  const themeSnap = useCallback(
    (): FrameTheme => ({
      id: th.theme.id,
      name: th.theme.name,
      kind: th.theme.kind,
      vars: themeVars(th.theme, th.colourMode, th.distroColour),
      density: th.density,
    }),
    [th.theme, th.colourMode, th.distroColour, th.density],
  );
  const live = useRef({ themeSnap, lang, user: {} as BrokerUser, plugin, nav });
  live.current = {
    themeSnap,
    lang,
    user: { groups: session?.groups ?? [], isRoot: !!session?.isRoot, home: session?.home, appOrigin: window.location.origin },
    plugin,
    nav,
  };

  const post = useCallback((m: HostToFrame, transfer?: Transferable[]) => frameRef.current?.contentWindow?.postMessage(m, '*', transfer ?? []), []);

  // Runs once: FrameSession is keyed on plugin, view and generation.
  useEffect(() => {
    let dead = false;
    let started = false;
    let pending = 0;
    // Every stream the frame opened; all are closed when the frame fails or goes away.
    const streams = new Map<number, { h: StreamHandle; kind: 'exec' | 'http' | 'pty' }>();

    const fail = (msg: string) => {
      if (dead) return;
      dead = true;
      for (const s of streams.values()) s.h.close();
      streams.clear();
      setFailure(msg);
      setPhase('failed');
      reportError(plugin.id, msg);
    };

    const start = async () => {
      if (started) return;
      started = true;
      try {
        const r = await fetch(apiUrl(`/plugins/${plugin.id}/${encodeAsset(plugin.entry)}`), { credentials: FETCH_CREDENTIALS, cache: 'no-cache' });
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const code = await r.text();
        if (dead) return;
        const { themeSnap: snap, lang: l } = live.current;
        post({ la: 'plugin', t: 'init', plugin: { id: plugin.id, name: plugin.name, version: plugin.version }, view, lang: l, theme: snap(), code });
      } catch (e) {
        fail(e instanceof Error ? e.message : String(e));
      }
    };

    const reply = (id: number, p: Promise<unknown>) => {
      pending++;
      p.then(
        (value) => {
          if (dead) return;
          const bytes = (value as FrameHttpResult | null)?.bytes;
          post({ la: 'plugin', t: 'res', id, ok: true, value }, bytes instanceof Uint8Array ? [bytes.buffer] : undefined);
        },
        (e) => !dead && post({ la: 'plugin', t: 'res', id, ok: false, error: toFrameError(e) }),
      ).finally(() => pending--);
    };

    const onRequest = (m: Extract<FrameToHost, { t: 'req' }>) => {
      if (typeof m.id !== 'number') return;
      if (pending >= MAX_PENDING) {
        post({ la: 'plugin', t: 'res', id: m.id, ok: false, error: { code: 'unavailable', message: 'Too many requests at once.' } });
        return;
      }
      const { user, plugin: p, nav: go } = live.current;
      const plan = authorize(p, m.op, m.args, user);
      switch (plan.kind) {
        case 'deny':
          reply(m.id, Promise.reject(new ApiError(plan.code, plan.message)));
          return;
        case 'call':
          reply(
            m.id,
            plan.method === 'plugins.http'
              ? call<{ status: number; headers?: Record<string, string>; body?: string; b64?: boolean }>(plan.method, plan.params, { admin: plan.admin }).then(toHttpResult)
              : call(plan.method, plan.params, { admin: plan.admin }),
          );
          return;
        case 'asset':
          reply(
            m.id,
            fetch(apiUrl(`/plugins/${p.id}/${encodeAsset(plan.path)}`), { credentials: FETCH_CREDENTIALS }).then(async (r) => {
              if (!r.ok) throw new ApiError(r.status === 404 ? 'not_found' : 'unavailable', `${plan.path}: HTTP ${r.status}`);
              return { data: await r.arrayBuffer(), type: r.headers.get('Content-Type') ?? 'application/octet-stream' };
            }),
          );
          return;
        case 'toast':
          toast[plan.tone](`${p.name}: ${plan.title}`, plan.detail);
          reply(m.id, Promise.resolve(null));
          return;
        case 'open':
          go(plan.to);
          reply(m.id, Promise.resolve(null));
          return;
        case 'openUrl': {
          // The frame has no allow-popups; the app opens the tab, detached from both the app and the frame.
          // With noopener window.open always returns null, so a blocked pop-up cannot be detected here.
          window.open(plan.url, '_blank', 'noopener,noreferrer');
          reply(m.id, Promise.resolve(null));
          return;
        }
        case 'stream':
          reply(m.id, Promise.reject(new ApiError('invalid', 'Use stream-open.')));
      }
    };

    const onStreamOpen = (m: Extract<FrameToHost, { t: 'stream-open' }>) => {
      const sid = m.sid;
      if (typeof sid !== 'number' || streams.has(sid)) return;
      const err = (e: FrameError) => post({ la: 'plugin', t: 'stream', sid, ev: 'error', error: e });
      if (streams.size >= MAX_STREAMS) return err({ code: 'unavailable', message: 'Too many streams open.' });
      const { user, plugin: p } = live.current;
      const kind = m.kind === 'http' || m.kind === 'pty' ? m.kind : 'exec';
      let plan: Plan;
      if (m.kind === 'http') plan = authorizeHttpStream(p, m.req, user);
      else if (m.kind === 'pty') plan = authorizePty(p, m.command, m.args, m.cols, m.rows, user);
      else plan = authorizeStream(p, m.command, m.args, user);
      if (plan.kind !== 'stream') return err(plan.kind === 'deny' ? { code: plan.code, message: plan.message } : { code: 'invalid', message: 'bad stream' });
      const onData = (d: unknown) => {
        if (dead || !d || typeof d !== 'object') return;
        if (d instanceof Uint8Array) {
          // httpStream body chunks and pty output arrive as bytes; handed over, not copied.
          if (kind !== 'exec') post({ la: 'plugin', t: 'stream', sid, ev: 'data', chunk: d }, [d.buffer]);
          return;
        }
        const e = d as { stream?: string; line?: string; exit?: number; type?: string; code?: number; status?: number; headers?: Record<string, string> };
        if (kind === 'exec') {
          if (typeof e.exit === 'number') post({ la: 'plugin', t: 'stream', sid, ev: 'exit', code: e.exit });
          else if (typeof e.line === 'string') post({ la: 'plugin', t: 'stream', sid, ev: 'line', stream: e.stream === 'stderr' ? 'stderr' : 'stdout', line: e.line });
        } else if (kind === 'pty') {
          if (e.type === 'exit' && typeof e.code === 'number') post({ la: 'plugin', t: 'stream', sid, ev: 'exit', code: e.code });
        } else if (typeof e.status === 'number') {
          post({ la: 'plugin', t: 'stream', sid, ev: 'start', status: e.status, headers: e.headers ?? {} });
        }
      };
      const h = stream(plan.method, plan.params, {
        admin: plan.admin,
        onData,
        onEnd: () => {
          streams.delete(sid);
          if (!dead) post({ la: 'plugin', t: 'stream', sid, ev: 'end' });
        },
        onError: (e) => {
          streams.delete(sid);
          if (!dead) err(toFrameError(e));
        },
      });
      streams.set(sid, { h, kind });
    };

    const onStreamInput = (m: Extract<FrameToHost, { t: 'stream-input' }>) => {
      const s = streams.get(m.sid);
      if (!s || s.kind !== 'pty') return;
      if (m.data instanceof Uint8Array && m.data.length > 0 && m.data.length <= 256 << 10) s.h.send({ type: 'input', data: toBase64(m.data) });
      const r = m.resize;
      if (r && Number.isInteger(r.cols) && Number.isInteger(r.rows) && r.cols >= 1 && r.rows >= 1 && r.cols <= 1000 && r.rows <= 1000) {
        s.h.send({ type: 'resize', cols: r.cols, rows: r.rows });
      }
    };

    const onMessage = (ev: MessageEvent) => {
      const win = frameRef.current?.contentWindow;
      if (dead || !win || ev.source !== win || !isFrameMessage(ev.data)) return;
      const m = ev.data;
      switch (m.t) {
        case 'ready':
          void start();
          break;
        case 'loaded':
          setPhase('running');
          reportError(plugin.id, null);
          break;
        case 'failed':
          fail(String(m.message).slice(0, 500));
          break;
        case 'size':
          if (view.kind === 'widget' && Number.isFinite(m.height)) setHeight(Math.max(WIDGET_MIN, Math.min(WIDGET_MAX, Math.ceil(m.height))));
          break;
        case 'req':
          onRequest(m);
          break;
        case 'stream-open':
          onStreamOpen(m);
          break;
        case 'stream-input':
          onStreamInput(m);
          break;
        case 'stream-close': {
          const s = streams.get(m.sid);
          streams.delete(m.sid);
          s?.h.close();
          break;
        }
      }
    };

    // A second load event means the frame navigated away from /plugin-frame: stop talking to it.
    let loads = 0;
    const el = frameRef.current;
    const onLoad = () => {
      loads++;
      if (loads > 1) fail(t('frame.navigated'));
    };
    el?.addEventListener('load', onLoad);
    window.addEventListener('message', onMessage);
    return () => {
      dead = true;
      window.removeEventListener('message', onMessage);
      el?.removeEventListener('load', onLoad);
      for (const s of streams.values()) s.h.close();
      streams.clear();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Keep the frame in step with the app's theme and language.
  useEffect(() => {
    if (phase === 'running') post({ la: 'plugin', t: 'theme', theme: themeSnap() });
  }, [phase, themeSnap, post]);
  useEffect(() => {
    if (phase === 'running') post({ la: 'plugin', t: 'lang', lang });
  }, [phase, lang, post]);

  if (phase === 'failed') {
    return <EmptyState icon="plugins" hue="plg" title={t('frame.failed', { name: plugin.name })} text={failure} />;
  }
  const page = view.kind === 'page';
  return (
    <div className={`plugin-frame plugin-frame--${view.kind}`} style={{ position: 'relative', ...(page ? { flex: 1, minHeight: 'min(70vh, 480px)', display: 'flex' } : {}), ...style }}>
      <iframe
        ref={frameRef}
        src={apiUrl(`/plugin-frame/${plugin.id}`)}
        sandbox="allow-scripts allow-forms"
        referrerPolicy="no-referrer"
        title={title ?? plugin.name}
        style={{
          border: 0,
          display: 'block',
          width: '100%',
          background: 'transparent',
          colorScheme: th.theme.kind,
          ...(page ? { flex: 1, height: 'auto', minHeight: 'inherit' } : { height }),
          visibility: phase === 'running' ? 'visible' : 'hidden',
        }}
      />
      {phase === 'starting' && (
        <div style={{ position: 'absolute', inset: 0, padding: page ? 28 : 4 }} aria-busy="true" aria-label={t('frame.loading')}>
          <Skeleton lines={page ? 5 : 2} />
        </div>
      )}
    </div>
  );
}
