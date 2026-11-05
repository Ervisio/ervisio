import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react';
import { useNavigate } from 'react-router-dom';
import { useSession } from '../api';
import { apiUrl } from '../api/base';
import { useI18n, useT } from '../i18n';
import { themeVars, useTheme } from '../theme';
import { useWindowActivity } from '../lib/windowActivity';
import { EmptyState, Skeleton } from '../ui';
import type { BrokerUser } from './broker';
import type { FrameTheme, FrameView, HostToFrame } from './protocol';
import { connectFrame } from './pluginFrameBridge';
import { usePlugins } from './PluginsProvider';
import type { PluginManifest } from './types';

/**
 * Hosts one view (page or widget) of a plugin in an <iframe sandbox="allow-scripts allow-forms"> served by the daemon at
 * /plugin-frame/<id>. The frame has an opaque origin: it cannot read cookies, the app's DOM or storage, or call
 * the API. Everything it needs goes through this component, which applies the broker policy (./broker.ts) and
 * then calls the daemon, which checks the manifest again.
 */
export function PluginFrame(props: FrameProps) {
  const { generation } = usePlugins();
  // A new frame (fresh sandbox, fresh code) for every plugin/view and every plugin reload.
  return (
    <FrameSession
      key={`${props.plugin.id}/${props.view.kind}/${props.view.id}/${props.view.shell ?? 'classic'}/${generation}`}
      {...props}
    />
  );
}

interface FrameProps {
  plugin: PluginManifest;
  view: FrameView;
  title?: string;
  style?: CSSProperties;
  /** Keeps the frame mounted while suspending presentation timers when minimized. */
  active?: boolean;
}

function FrameSession({ plugin, view, title, style, active: requestedActive = true }: FrameProps) {
  const windowActive = useWindowActivity();
  const active = requestedActive && windowActive;
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
  const live = useRef({ themeSnap, lang, user: {} as BrokerUser, plugin, nav, active });
  live.current = {
    themeSnap,
    lang,
    user: {
      groups: session?.groups ?? [],
      isRoot: !!session?.isRoot,
      home: session?.home,
      appOrigin: window.location.origin,
    },
    plugin,
    nav,
    active,
  };

  const post = useCallback(
    (m: HostToFrame, transfer?: Transferable[]) =>
      frameRef.current?.contentWindow?.postMessage(m, '*', transfer ?? []),
    [],
  );

  // Runs once: FrameSession is keyed on plugin, view and generation.
  useEffect(
    () =>
      connectFrame({
        frameRef,
        plugin,
        view,
        platform: session?.os ?? 'linux',
        live,
        post,
        onLoaded: () => {
          setPhase('running');
          reportError(plugin.id, null);
        },
        onFailed: (msg) => {
          setFailure(msg);
          setPhase('failed');
          reportError(plugin.id, msg);
        },
        onHeight: setHeight,
        navigatedMessage: () => t('frame.navigated'),
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  // Keep the frame in step with the app's theme and language.
  useEffect(() => {
    if (phase === 'running') post({ la: 'plugin', t: 'theme', theme: themeSnap() });
  }, [phase, themeSnap, post]);
  useEffect(() => {
    if (phase === 'running') post({ la: 'plugin', t: 'lang', lang });
  }, [phase, lang, post]);
  useEffect(() => {
    post({ la: 'plugin', t: 'visibility', active });
  }, [active, phase, post]);

  if (phase === 'failed') {
    return (
      <EmptyState
        icon="plugins"
        hue="plg"
        title={t('frame.failed', { name: plugin.name })}
        text={failure}
      />
    );
  }
  const page = view.kind === 'page';
  return (
    <div
      className={`plugin-frame plugin-frame--${view.kind}`}
      style={{
        position: 'relative',
        ...(page ? { flex: 1, minHeight: 'min(70vh, 480px)', display: 'flex' } : {}),
        ...style,
      }}
    >
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
        <div
          style={{ position: 'absolute', inset: 0, padding: page ? 28 : 4 }}
          aria-busy="true"
          aria-label={t('frame.loading')}
        >
          <Skeleton lines={page ? 5 : 2} />
        </div>
      )}
    </div>
  );
}
