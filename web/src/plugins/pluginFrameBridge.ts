import type { MutableRefObject, RefObject } from 'react';
import type { NavigateFunction } from 'react-router-dom';
import { runShellCommand } from '../shell/commands';
import {
  ApiError,
  call,
  fromBase64,
  stream,
  toBase64,
  type StreamHandle,
  errorMessage,
} from '../api';
import { apiUrl, consoleOrigin, FETCH_CREDENTIALS } from '../api/base';
import {
  saveDownload,
  sendUpload,
  startTransfer,
  watchDownloadEnd,
  type TransferStart,
} from '../api/transfer';
import {
  authorize,
  authorizeHttpStream,
  authorizePty,
  authorizeStream,
  authorizeUpload,
  SaveLimiter,
  type BrokerUser,
  type Plan,
} from './broker';
import {
  isFrameMessage,
  type FrameError,
  type FrameHttpResult,
  type FrameTheme,
  type FrameToHost,
  type FrameUploadResult,
  type FrameView,
  type HostToFrame,
} from './protocol';
import { toast } from '../ui';
import { askNetwork } from './networkApproval';
import type { PluginManifest } from './types';

// Per frame. A page may follow many containers' stats at once; the app's WebSocket allows 128 channels per session.
const MAX_STREAMS = 32;
const MAX_PENDING = 32;
// Uploads running at once per frame (the daemon also limits a session's transfers).
const MAX_UPLOADS = 4;
const WIDGET_MIN = 48;
const WIDGET_MAX = 720;
const MAX_PTY_INPUT = 256 << 10;
const MAX_PTY_SIZE = 1000;
const SAVE_URL_TTL_MS = 60_000;

const toFrameError = (e: unknown): FrameError =>
  e instanceof ApiError
    ? { code: String(e.code), message: e.message }
    : { code: 'internal', message: errorMessage(e) };

const encodeAsset = (p: string) => p.split('/').map(encodeURIComponent).join('/');

/** plugins.http result → what the frame gets: text for UTF-8 bodies, a Uint8Array for binary ones. */
const toHttpResult = (r: {
  status: number;
  headers?: Record<string, string>;
  body?: string;
  b64?: boolean;
}): FrameHttpResult =>
  r.b64
    ? { status: r.status, headers: r.headers ?? {}, bytes: fromBase64(r.body ?? '') }
    : { status: r.status, headers: r.headers ?? {}, text: r.body ?? '' };

type HttpCallResult = {
  status: number;
  headers?: Record<string, string>;
  body?: string;
  b64?: boolean;
};
type StreamKind = 'exec' | 'http' | 'pty';
type StreamOpen = Extract<FrameToHost, { t: 'stream-open' }>;

const clampWidgetHeight = (h: number) => Math.max(WIDGET_MIN, Math.min(WIDGET_MAX, Math.ceil(h)));

const isValidPtySize = (
  r: { cols: number; rows: number } | undefined,
): r is { cols: number; rows: number } =>
  !!r &&
  Number.isInteger(r.cols) &&
  Number.isInteger(r.rows) &&
  r.cols >= 1 &&
  r.rows >= 1 &&
  r.cols <= MAX_PTY_SIZE &&
  r.rows <= MAX_PTY_SIZE;

/** The download id the frame passed to hear when the browser's download ends; 0 when it did not ask. */
const downloadId = (args: Record<string, unknown> | undefined): number =>
  args && typeof (args as { did?: unknown }).did === 'number' ? (args as { did: number }).did : 0;

function saveAsDownload(data: Blob | Uint8Array | string, mime: string, filename: string) {
  const url = URL.createObjectURL(new Blob([data as BlobPart], { type: mime }));
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.rel = 'noopener';
  a.style.display = 'none';
  document.body.appendChild(a);
  a.click();
  window.setTimeout(() => {
    a.remove();
    URL.revokeObjectURL(url);
  }, SAVE_URL_TTL_MS);
}

const fetchAsset = (pluginId: string, path: string) =>
  fetch(apiUrl(`/plugins/${pluginId}/${encodeAsset(path)}`), {
    credentials: FETCH_CREDENTIALS,
  }).then(async (r) => {
    if (!r.ok)
      throw new ApiError(
        r.status === 404 ? 'not_found' : 'unavailable',
        `${path}: HTTP ${r.status}`,
      );
    return {
      data: await r.arrayBuffer(),
      type: r.headers.get('Content-Type') ?? 'application/octet-stream',
    };
  });

interface PlanContext {
  plugin: PluginManifest;
  args: Record<string, unknown>;
  go: NavigateFunction;
  saves: SaveLimiter;
  watchDownload(did: number, start: TransferStart): void;
}

const runCall = (plan: Extract<Plan, { kind: 'call' }>) =>
  plan.method === 'plugins.http'
    ? call<HttpCallResult>(plan.method, plan.params, { admin: plan.admin }).then(toHttpResult)
    : call(plan.method, plan.params, { admin: plan.admin });

// plugins.download: the daemon checks the request, then the app (not the sandboxed frame) starts the browser's download.
const runTransfer = (plan: Extract<Plan, { kind: 'transfer' }>, ctx: PlanContext) =>
  startTransfer(plan.body, plan.admin).then((r) => {
    saveDownload(r);
    // The browser fetches the file by itself: the daemon says how that ended, and the frame hears it.
    const did = downloadId(ctx.args);
    if (did) ctx.watchDownload(did, r);
    return { filename: r.filename ?? 'download', size: r.size, status: r.status };
  });

// Data the plugin holds in memory, saved from the app (the sandboxed frame may not download).
function runSave(plan: Extract<Plan, { kind: 'save' }>, ctx: PlanContext): Promise<unknown> {
  if (!ctx.saves.allow(plan.size)) {
    return Promise.reject(
      new ApiError(
        'unavailable',
        'Too many files saved at once. Wait a few seconds and try again.',
      ),
    );
  }
  saveAsDownload(plan.data, plan.mime, plan.filename);
  return Promise.resolve({ filename: plan.filename, size: plan.size });
}

/** Carries out an authorized request plan; the promise settles with the reply the frame gets. */
function runPlan(plan: Plan, ctx: PlanContext): Promise<unknown> {
  const p = ctx.plugin;
  switch (plan.kind) {
    case 'deny':
      return Promise.reject(new ApiError(plan.code, plan.message));
    case 'call':
      return runCall(plan);
    case 'transfer':
      return runTransfer(plan, ctx);
    case 'save':
      return runSave(plan, ctx);
    case 'asset':
      return fetchAsset(p.id, plan.path);
    case 'toast':
      toast[plan.tone](`${p.name}: ${plan.title}`, plan.detail);
      return Promise.resolve(null);
    case 'open':
      ctx.go(plan.to);
      return Promise.resolve(null);
    case 'shell.command':
      return runShellCommand(plan.text);
    case 'openUrl':
      // The frame has no allow-popups; the app opens the tab, detached from both the app and the frame.
      // With noopener window.open always returns null, so a blocked pop-up cannot be detected here.
      window.open(plan.url, '_blank', 'noopener,noreferrer');
      return Promise.resolve(null);
    case 'network':
      return askNetwork({ id: p.id, name: p.name }, plan.host, plan.scheme);
    case 'stream':
      return Promise.reject(new ApiError('invalid', 'Use stream-open.'));
  }
}

function authorizeStreamOpen(m: StreamOpen, p: PluginManifest, user: BrokerUser): Plan {
  if (m.kind === 'http') return authorizeHttpStream(p, m.req, user);
  if (m.kind === 'pty') return authorizePty(p, m.command, m.args, m.cols, m.rows, user, m.env);
  return authorizeStream(p, m.command, m.args, user, m.env);
}

/** One stream event from the daemon → the message the frame gets, or null when the frame does not hear about it. */
function toStreamMessage(
  kind: StreamKind,
  sid: number,
  d: unknown,
): { msg: HostToFrame; transfer?: Transferable[] } | null {
  if (!d || typeof d !== 'object') return null;
  if (d instanceof Uint8Array) {
    // httpStream body chunks and pty output arrive as bytes; handed over, not copied.
    return kind !== 'exec'
      ? { msg: { la: 'plugin', t: 'stream', sid, ev: 'data', chunk: d }, transfer: [d.buffer] }
      : null;
  }
  const e = d as {
    stream?: string;
    line?: string;
    exit?: number;
    type?: string;
    code?: number;
    status?: number;
    headers?: Record<string, string>;
  };
  if (kind === 'exec') {
    if (typeof e.exit === 'number')
      return { msg: { la: 'plugin', t: 'stream', sid, ev: 'exit', code: e.exit } };
    if (typeof e.line === 'string')
      return {
        msg: {
          la: 'plugin',
          t: 'stream',
          sid,
          ev: 'line',
          stream: e.stream === 'stderr' ? 'stderr' : 'stdout',
          line: e.line,
        },
      };
    return null;
  }
  if (kind === 'pty') {
    return e.type === 'exit' && typeof e.code === 'number'
      ? { msg: { la: 'plugin', t: 'stream', sid, ev: 'exit', code: e.code } }
      : null;
  }
  return typeof e.status === 'number'
    ? {
        msg: {
          la: 'plugin',
          t: 'stream',
          sid,
          ev: 'start',
          status: e.status,
          headers: e.headers ?? {},
        },
      }
    : null;
}

/** What the bridge reads from the latest render: the frame session itself is keyed and never re-created. */
export interface FrameLive {
  themeSnap(): FrameTheme;
  lang: string;
  user: BrokerUser;
  plugin: PluginManifest;
  nav: NavigateFunction;
}

export type PostToFrame = (m: HostToFrame, transfer?: Transferable[]) => void;

// Mutable state of one frame session, shared by the message handlers below.
interface Bridge {
  dead: boolean;
  pending: number;
  // Every stream the frame opened; all are closed when the frame fails or goes away.
  streams: Map<number, { h: StreamHandle; kind: StreamKind }>;
  saves: SaveLimiter;
  // Uploads in progress; aborted with the frame.
  uploads: Map<number, AbortController>;
  // Downloads whose end the frame wants to hear about; stopped with the frame.
  watchers: AbortController;
  post: PostToFrame;
  live: MutableRefObject<FrameLive>;
}

type UploadEvent =
  | { ev: 'progress'; loaded: number; total: number }
  | { ev: 'start'; status: number; headers: Record<string, string> }
  | { ev: 'data'; chunk: Uint8Array }
  | { ev: 'done'; result: FrameUploadResult }
  | { ev: 'error'; error: FrameError };

function stopAll(b: Bridge) {
  for (const s of b.streams.values()) s.h.close();
  b.streams.clear();
  for (const u of b.uploads.values()) u.abort();
  b.uploads.clear();
  b.watchers.abort();
}

function reply(b: Bridge, id: number, p: Promise<unknown>) {
  b.pending++;
  p.then(
    (value) => {
      if (b.dead) return;
      const bytes = (value as FrameHttpResult | null)?.bytes;
      b.post(
        { la: 'plugin', t: 'res', id, ok: true, value },
        bytes instanceof Uint8Array ? [bytes.buffer] : undefined,
      );
    },
    (e) => !b.dead && b.post({ la: 'plugin', t: 'res', id, ok: false, error: toFrameError(e) }),
  ).finally(() => b.pending--);
}

function watchDownload(b: Bridge, did: number, r: TransferStart) {
  void watchDownloadEnd(r, b.watchers.signal).then((o) => {
    if (!b.dead) b.post({ la: 'plugin', t: 'download-done', did, ...o });
  });
}

function onRequest(b: Bridge, m: Extract<FrameToHost, { t: 'req' }>) {
  if (typeof m.id !== 'number') return;
  if (b.pending >= MAX_PENDING) {
    b.post({
      la: 'plugin',
      t: 'res',
      id: m.id,
      ok: false,
      error: { code: 'unavailable', message: 'Too many requests at once.' },
    });
    return;
  }
  const { user, plugin: p, nav: go } = b.live.current;
  const plan = authorize(p, m.op, m.args, user);
  reply(
    b,
    m.id,
    runPlan(plan, {
      plugin: p,
      args: m.args,
      go,
      saves: b.saves,
      watchDownload: (did, r) => watchDownload(b, did, r),
    }),
  );
}

function onStreamOpen(b: Bridge, m: StreamOpen) {
  const sid = m.sid;
  if (typeof sid !== 'number' || b.streams.has(sid)) return;
  const err = (e: FrameError) => b.post({ la: 'plugin', t: 'stream', sid, ev: 'error', error: e });
  if (b.streams.size >= MAX_STREAMS)
    return err({ code: 'unavailable', message: 'Too many streams open.' });
  const { user, plugin: p } = b.live.current;
  const kind = m.kind === 'http' || m.kind === 'pty' ? m.kind : 'exec';
  const plan = authorizeStreamOpen(m, p, user);
  if (plan.kind !== 'stream')
    return err(
      plan.kind === 'deny'
        ? { code: plan.code, message: plan.message }
        : { code: 'invalid', message: 'bad stream' },
    );
  const onData = (d: unknown) => {
    if (b.dead) return;
    const out = toStreamMessage(kind, sid, d);
    if (out) b.post(out.msg, out.transfer);
  };
  const h = stream(plan.method, plan.params, {
    admin: plan.admin,
    onData,
    onEnd: () => {
      b.streams.delete(sid);
      if (!b.dead) b.post({ la: 'plugin', t: 'stream', sid, ev: 'end' });
    },
    onError: (e) => {
      b.streams.delete(sid);
      if (!b.dead) err(toFrameError(e));
    },
  });
  b.streams.set(sid, { h, kind });
}

function onStreamInput(b: Bridge, m: Extract<FrameToHost, { t: 'stream-input' }>) {
  const s = b.streams.get(m.sid);
  if (!s || s.kind !== 'pty') return;
  if (m.data instanceof Uint8Array && m.data.length > 0 && m.data.length <= MAX_PTY_INPUT)
    s.h.send({ type: 'input', data: toBase64(m.data) });
  const r = m.resize;
  if (isValidPtySize(r)) s.h.send({ type: 'resize', cols: r.cols, rows: r.rows });
}

function onStreamClose(b: Bridge, sid: number) {
  const s = b.streams.get(sid);
  b.streams.delete(sid);
  s?.h.close();
}

function onUploadOpen(b: Bridge, m: Extract<FrameToHost, { t: 'upload-open' }>) {
  const uid = m.uid;
  if (typeof uid !== 'number' || b.uploads.has(uid)) return;
  const out = (msg: UploadEvent, transfer?: Transferable[]) =>
    !b.dead && b.post({ la: 'plugin', t: 'upload', uid, ...msg } as HostToFrame, transfer);
  const fail = (e: unknown) => out({ ev: 'error', error: toFrameError(e) });
  if (b.uploads.size >= MAX_UPLOADS)
    return fail(new ApiError('unavailable', 'Too many uploads at once.'));
  if (!(m.file instanceof Blob))
    return fail(new ApiError('invalid', 'Give a file (a File or Blob).'));
  const { user, plugin: p } = b.live.current;
  const isStream = m.stream === true;
  const plan = authorizeUpload(p, m.req, m.file.size, user, isStream);
  if (plan.kind === 'deny') return fail(new ApiError(plan.code, plan.message));
  if (plan.kind !== 'transfer') return fail(new ApiError('invalid', 'bad upload'));
  const ctl = new AbortController();
  b.uploads.set(uid, ctl);
  const file = m.file;
  startTransfer(plan.body, plan.admin)
    .then((r) =>
      sendUpload(r, file, {
        signal: ctl.signal,
        onProgress: (loaded, total) => out({ ev: 'progress', loaded, total }),
        ...(isStream
          ? {
              stream: {
                onStart: (status, headers) => out({ ev: 'start', status, headers }),
                onData: (chunk) => out({ ev: 'data', chunk }, [chunk.buffer]),
              },
            }
          : {}),
      }),
    )
    .then((result) =>
      out({
        ev: 'done',
        result: { ...toHttpResult(result), ...(result.truncated ? { truncated: true } : {}) },
      }),
    )
    .catch(fail)
    .finally(() => b.uploads.delete(uid));
}

export interface FrameConnection {
  frameRef: RefObject<HTMLIFrameElement>;
  plugin: PluginManifest;
  view: FrameView;
  platform: Extract<HostToFrame, { t: 'init' }>['platform'];
  live: MutableRefObject<FrameLive>;
  post: PostToFrame;
  onLoaded(): void;
  onFailed(message: string): void;
  onHeight(height: number): void;
  navigatedMessage(): string;
}

async function sendInit(b: Bridge, c: FrameConnection, fail: (msg: string) => void) {
  const { plugin } = c;
  try {
    const r = await fetch(apiUrl(`/plugins/${plugin.id}/${encodeAsset(plugin.entry)}`), {
      credentials: FETCH_CREDENTIALS,
      cache: 'no-cache',
    });
    if (!r.ok) throw new Error(`HTTP ${r.status}`);
    const code = await r.text();
    if (b.dead) return;
    const { themeSnap: snap, lang: l } = b.live.current;
    b.post({
      la: 'plugin',
      t: 'init',
      plugin: { id: plugin.id, name: plugin.name, version: plugin.version },
      view: c.view,
      lang: l,
      theme: snap(),
      appOrigin: consoleOrigin(),
      platform: c.platform,
      code,
    });
  } catch (e) {
    fail(errorMessage(e));
  }
}

/** Starts the postMessage bridge with the frame; returns the cleanup that stops it. */
export function connectFrame(c: FrameConnection): () => void {
  const b: Bridge = {
    dead: false,
    pending: 0,
    streams: new Map(),
    saves: new SaveLimiter(),
    uploads: new Map(),
    watchers: new AbortController(),
    post: c.post,
    live: c.live,
  };
  let started = false;

  const fail = (msg: string) => {
    if (b.dead) return;
    b.dead = true;
    stopAll(b);
    c.onFailed(msg);
  };

  const start = () => {
    if (started) return Promise.resolve();
    started = true;
    return sendInit(b, c, fail);
  };

  const onMessage = (ev: MessageEvent) => {
    const win = c.frameRef.current?.contentWindow;
    if (b.dead || !win || ev.source !== win || !isFrameMessage(ev.data)) return;
    const m = ev.data;
    switch (m.t) {
      case 'ready':
        void start();
        break;
      case 'loaded':
        c.onLoaded();
        break;
      case 'failed':
        fail(String(m.message).slice(0, 500));
        break;
      case 'size':
        if (c.view.kind === 'widget' && Number.isFinite(m.height))
          c.onHeight(clampWidgetHeight(m.height));
        break;
      case 'req':
        onRequest(b, m);
        break;
      case 'stream-open':
        onStreamOpen(b, m);
        break;
      case 'stream-input':
        onStreamInput(b, m);
        break;
      case 'upload-open':
        onUploadOpen(b, m);
        break;
      case 'upload-cancel':
        b.uploads.get(m.uid)?.abort();
        break;
      case 'stream-close':
        onStreamClose(b, m.sid);
        break;
    }
  };

  // A second load event means the frame navigated away from /plugin-frame: stop talking to it.
  let loads = 0;
  const el = c.frameRef.current;
  const onLoad = () => {
    loads++;
    if (loads > 1) fail(c.navigatedMessage());
  };
  el?.addEventListener('load', onLoad);
  window.addEventListener('message', onMessage);
  return () => {
    b.dead = true;
    window.removeEventListener('message', onMessage);
    el?.removeEventListener('load', onLoad);
    stopAll(b);
  };
}
