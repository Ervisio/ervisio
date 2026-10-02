/**
 * Plugin frame runtime, built by `npm run build:runtime` into public/plugin-runtime.js (vite.runtime.config.ts)
 * and loaded by the daemon's /plugin-frame/<id> page inside <iframe sandbox="allow-scripts allow-forms">.
 *
 * It waits for the app's `init` message (plugin code, view, theme, language), imports the plugin module from a
 * blob: URL, calls its activate(sdk) and renders the requested page or widget. Everything outside the frame
 * goes through the app's broker (channel.ts); the frame itself has an opaque origin and no network.
 */
import * as React from 'react';
import { Component, useEffect, useRef, useSyncExternalStore, type ComponentType, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import tokensCss from '../../styles/tokens.css?inline';
import uiCss from '../../ui/ui.css?inline';
import frameCss from './frame.css?inline';
import type { FrameHttpRequest, FrameHttpResult, FrameTheme, FrameUploadResult, FrameView, HostToFrame } from '../protocol';
import { handleReply, openHttpStream, openPty, openStream, openUpload, PluginError, request, send, type HttpStreamCallbacks, type PtyCallbacks, type UploadCallbacks } from './channel';
import { getLang, setLang, subscribeLang } from './i18n-shim';
import { jobsApi, notifyApi } from './jobs';
import * as kit from './kit';

type ViewDef<S> = ComponentType<{ sdk: S }> | { render(container: HTMLElement, sdk: S): void | (() => void) };

const style = document.createElement('style');
style.textContent = `${tokensCss}\n${uiCss}\n${frameCss}`;
document.head.appendChild(style);

/* ---------- theme ---------- */
let theme: FrameTheme = { id: '', name: '', kind: 'dark', vars: {} };
const themeListeners = new Set<() => void>();
function applyTheme(t: FrameTheme) {
  theme = t;
  const root = document.documentElement;
  for (const [k, v] of Object.entries(t.vars ?? {})) if (k.startsWith('--')) root.style.setProperty(k, String(v));
  root.style.colorScheme = t.kind === 'light' ? 'light' : 'dark';
  if (t.density) root.dataset.density = t.density;
  themeListeners.forEach((f) => f());
}

/* ---------- helpers ---------- */
const b64ToBytes = (s: string) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
function bytesToB64(u: Uint8Array): string {
  let s = '';
  for (let i = 0; i < u.length; i += 0x8000) s += String.fromCharCode(...u.subarray(i, i + 0x8000));
  return btoa(s);
}

/* ---------- HTTP (SDK v3) ---------- */
interface HttpOptions {
  method: string;
  path: string;
  query?: Record<string, string | string[]> | string;
  headers?: Record<string, string>;
  body?: string | Uint8Array | object;
  /** Id of an environment (sdk.envs.list()). */
  env?: string;
}

function httpRequest(name: string, o: HttpOptions): FrameHttpRequest {
  if (!o || typeof o !== 'object') throw new PluginError({ code: 'invalid', message: 'Give {method, path}.' });
  let query = '';
  if (typeof o.query === 'string') query = o.query.replace(/^\?/, '');
  else if (o.query && typeof o.query === 'object') {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(o.query)) for (const x of Array.isArray(v) ? v : [v]) q.append(k, String(x));
    query = q.toString();
  }
  const req: FrameHttpRequest = { name, method: String(o.method ?? 'GET').toUpperCase(), path: String(o.path ?? '') };
  if (query) req.query = query;
  if (o.headers) req.headers = { ...o.headers };
  if (o.env) req.env = String(o.env);
  const b = o.body;
  if (typeof b === 'string' || b instanceof Uint8Array) req.body = b;
  else if (b !== undefined && b !== null) {
    req.body = JSON.stringify(b);
    req.json = true;
  }
  return req;
}

/** Options of sdk.api.upload. With onResponseData or onResponseStart the response is streamed and the result's body is empty. */
interface UploadOptions {
  onProgress?(p: { loaded: number; total: number }): void;
  onResponseStart?(status: number, headers: Record<string, string>): void;
  onResponseData?(chunk: Uint8Array): void;
}

const saveFile = (filename: string, data: string | Uint8Array | Blob, mime?: string) =>
  request<{ filename: string; size: number }>('saveFile', { filename, data, ...(mime ? { mime } : {}) });

/** The file name of a download: a string, or taken from the path when the plugin gives none. */
const downloadName = (name: unknown, req: { path?: string }): string => {
  if (typeof name === 'string' && name) return name;
  const last = String(req.path ?? '').split('/').filter(Boolean).pop();
  return last || 'download';
};

const textDecoder = new TextDecoder();
const textEncoder = new TextEncoder();
function httpResponse(r: FrameHttpResult & { truncated?: boolean }) {
  const body = r.text ?? textDecoder.decode(r.bytes ?? new Uint8Array());
  return {
    status: r.status,
    headers: r.headers ?? {},
    body,
    json: () => JSON.parse(body),
    bytes: () => (r.bytes ? r.bytes.slice() : textEncoder.encode(body)),
  };
}

/** One line of the activity log. */
interface AuditEntry {
  time: string;
  user: string;
  ip?: string;
  source: 'plugin' | 'core';
  plugin?: string;
  action: string;
  via?: string;
  target?: string;
  result: 'ok' | 'failed' | 'denied' | 'error';
  code?: number;
  bytes?: number;
  admin?: boolean;
  detail?: string;
}

/* ---------- SDK ---------- */
function makeSdk(plugin: { id: string; name: string; version: string }, view: FrameView) {
  const pages = new Map<string, ViewDef<unknown>>();
  const widgets = new Map<string, ViewDef<unknown>>();
  const strings: Record<string, Record<string, string>> = {};
  const assets = new Map<string, Promise<string>>();
  const sdk = {
    version: 3 as const,
    plugin,
    view,
    react: React,
    ui: kit,
    api: {
      exec: (command: string, args: string[] = [], o?: { env?: string }) =>
        request<{ stdout: string; stderr: string; exitCode: number; truncated?: boolean }>('exec', { command, args, ...(o?.env ? { env: o.env } : {}) }),
      execStream: (command: string, args: string[], h: Parameters<typeof openStream>[2], o?: { env?: string }) => openStream(command, args ?? [], h ?? {}, o?.env),
      async http(name: string, o: HttpOptions) {
        return httpResponse(await request<FrameHttpResult>('http', httpRequest(name, o) as unknown as Record<string, unknown>));
      },
      httpStream: (name: string, o: HttpOptions, h: HttpStreamCallbacks) => openHttpStream(httpRequest(name, o), h ?? {}),
      /** SDK 0.2: the browser saves the response of a GET as a file; it streams, there is no size limit. Resolves when the download starts. */
      async download(name: string, o: HttpOptions, filename?: string) {
        const req = httpRequest(name, { ...o, method: o?.method ?? 'GET' });
        return request<{ filename: string; size?: number; status?: number }>('download', { req, filename: downloadName(filename, req) });
      },
      /** SDK 0.2: same for the standard output of a declared command. */
      async downloadCommand(command: string, args: string[], filename?: string, o?: { env?: string }) {
        return request<{ filename: string; size?: number; status?: number }>('download', { command, args: args ?? [], filename: downloadName(filename, { path: command }), ...(o?.env ? { env: o.env } : {}) });
      },
      /** SDK 0.2: sends a File or Blob as the body of a POST or PUT, streamed with progress; cancel() stops it. */
      upload(name: string, o: HttpOptions, file: Blob, opts?: UploadOptions | ((p: { loaded: number; total: number }) => void)) {
        if (!(file instanceof Blob)) throw new PluginError({ code: 'invalid', message: 'Give a File or Blob.' });
        const req = httpRequest(name, { ...o, method: o?.method ?? 'POST', body: undefined });
        const u: UploadOptions = typeof opts === 'function' ? { onProgress: opts } : opts ?? {};
        const cb: UploadCallbacks = {
          onProgress: u.onProgress ? (loaded, total) => u.onProgress!({ loaded, total }) : undefined,
          onResponseStart: u.onResponseStart,
          onResponseData: u.onResponseData,
        };
        const h = openUpload(req, file, cb);
        const done = h.result.then((r: FrameUploadResult) => ({ ...httpResponse(r), truncated: !!r.truncated }));
        return Object.assign(done, { cancel: h.cancel });
      },
      /** SDK 0.2: saves data the plugin holds in memory (a string, bytes or a Blob, at most 64 MiB) as a browser download. */
      saveFile: (filename: string, data: string | Uint8Array | Blob, mime?: string) => saveFile(filename, data, mime),
      pty: (command: string, args: string[], o: { cols?: number; rows?: number; env?: string } & PtyCallbacks) =>
        openPty(command, args ?? [], Math.floor(o?.cols ?? 80), Math.floor(o?.rows ?? 24), o ?? {}, o?.env),
      /** Background jobs and notifications (SDK 0.2; needs capabilities.jobs / capabilities.notify). */
      jobs: jobsApi,
      notify: notifyApi,
      call: () => Promise.reject(new PluginError({ code: 'forbidden', message: 'sdk.api.call is not available to plugins (SDK v2+): use sdk.api.exec with a declared command.' })),
      stream: () => {
        throw new PluginError({ code: 'forbidden', message: 'sdk.api.stream is not available to plugins (SDK v2+): use sdk.api.execStream.' });
      },
    },
    saveFile,
    /** SDK 0.2: this plugin's entries of the activity log, newest first. Everyone sees their own; administrators see all users. */
    audit: {
      list: (q: { user?: string; action?: string; text?: string; since?: number | string; until?: number | string; limit?: number; cursor?: string } = {}) =>
        request<{ entries: AuditEntry[]; next: string; enabled: boolean }>('auditList', { ...q }),
    },
    files: {
      async read(path: string): Promise<string> {
        const r = await request<{ data: string; b64?: boolean }>('readFile', { path });
        return r.b64 ? new TextDecoder().decode(b64ToBytes(r.data)) : r.data;
      },
      async readBytes(path: string): Promise<Uint8Array> {
        const r = await request<{ data: string; b64?: boolean }>('readFile', { path, b64: true });
        return r.b64 ? b64ToBytes(r.data) : new TextEncoder().encode(r.data);
      },
      async write(path: string, data: string | Uint8Array): Promise<void> {
        await request('writeFile', typeof data === 'string' ? { path, data } : { path, data: bytesToB64(data), b64: true });
      },
      async list(path: string) {
        const r = await request<{ entries: { name: string; type: string; size: number; mtime: number }[] }>('listDir', { path });
        return r.entries ?? [];
      },
      async mkdir(path: string): Promise<void> {
        await request('mkdir', { path });
      },
      async remove(path: string): Promise<void> {
        await request('remove', { path });
      },
    },
    envs: {
      list: () => request<unknown[]>('envs'),
    },
    network: {
      request: (host: string, o?: { scheme?: 'https' | 'http' }) =>
        request<{ host: string; approved: true; reloading: boolean }>('network', { host, ...(o?.scheme ? { scheme: o.scheme } : {}) }),
    },
    asset(path: string): Promise<string> {
      let p = assets.get(path);
      if (!p) {
        p = request<{ data: ArrayBuffer; type: string }>('asset', { path }).then((r) => URL.createObjectURL(new Blob([r.data], { type: r.type })));
        assets.set(path, p);
      }
      return p;
    },
    open(pageId: string) {
      void request('open', { page: pageId }).catch((e) => console.warn(e));
    },
    openExternal(url: string): Promise<void> {
      return request<null>('openUrl', { url }).then(() => undefined);
    },
    registerPage(id: string, def: ViewDef<unknown>) {
      pages.set(id, def);
    },
    registerWidget(def: { id: string; render: ViewDef<unknown> }) {
      if (def && def.id) widgets.set(def.id, def.render);
    },
    registerSnippet() {
      /* snippets are declared in manifest.contributes.snippets */
    },
    registerStrings(d: Record<string, Record<string, string>>) {
      for (const [l, dict] of Object.entries(d ?? {})) strings[l] = { ...strings[l], ...dict };
    },
    t(key: string, vars?: Record<string, string | number>): string {
      const s = strings[getLang()]?.[key] ?? strings.en?.[key] ?? key;
      return vars ? s.replace(/\{(\w+)\}/g, (x, k) => (k in vars ? String(vars[k]) : x)) : s;
    },
    lang: getLang,
    theme: {
      get: () => ({ id: theme.id, name: theme.name, kind: theme.kind, vars: { ...theme.vars } }),
      onChange(cb: () => void) {
        themeListeners.add(cb);
        return () => void themeListeners.delete(cb);
      },
    },
  };
  return { sdk, pages, widgets };
}
type Sdk = ReturnType<typeof makeSdk>['sdk'];

/* ---------- rendering ---------- */
class Boundary extends Component<{ children: ReactNode }, { error: string | null }> {
  state = { error: null as string | null };
  static getDerivedStateFromError(e: unknown) {
    return { error: e instanceof Error ? e.message : String(e) };
  }
  componentDidCatch(e: unknown) {
    send({ la: 'plugin', t: 'failed', message: e instanceof Error ? e.message : String(e) });
  }
  render() {
    return this.state.error ? <div className="la-frame-error">{this.state.error}</div> : this.props.children;
  }
}

function Mount({ def, sdk }: { def: ViewDef<Sdk>; sdk: Sdk }) {
  // Re-render on language changes so sdk.t() output follows the app.
  useSyncExternalStore(subscribeLang, getLang);
  const ref = useRef<HTMLDivElement>(null);
  const isComponent = typeof def === 'function';
  // First commit done: the host can show the frame. (Not requestAnimationFrame: the host keeps the frame
  // hidden until this message, and hidden frames get no animation frames.)
  useEffect(() => send({ la: 'plugin', t: 'loaded' }), []);
  useEffect(() => {
    if (isComponent || !ref.current) return;
    const cleanup = (def as { render(c: HTMLElement, s: Sdk): void | (() => void) }).render(ref.current, sdk);
    return () => {
      if (typeof cleanup === 'function') cleanup();
    };
  }, [def, isComponent, sdk]);
  if (isComponent) {
    const C = def as ComponentType<{ sdk: Sdk }>;
    return <C sdk={sdk} />;
  }
  return <div ref={ref} style={{ display: 'contents' }} />;
}

async function start(m: Extract<HostToFrame, { t: 'init' }>) {
  setLang(m.lang);
  applyTheme(m.theme);
  document.documentElement.dataset.view = m.view.kind;
  document.documentElement.lang = getLang();
  const { sdk, pages, widgets } = makeSdk(m.plugin, m.view);
  const url = URL.createObjectURL(new Blob([m.code], { type: 'text/javascript' }));
  const mod = (await import(/* @vite-ignore */ url)) as { default?: unknown; activate?: unknown };
  URL.revokeObjectURL(url);
  const fn = (mod.default ?? mod.activate) as ((s: Sdk) => unknown) | { activate?(s: Sdk): unknown } | undefined;
  if (typeof fn === 'function') await fn(sdk);
  else if (fn && typeof fn.activate === 'function') await fn.activate(sdk);
  else throw new Error('The plugin module exports no activate function.');

  const def = (m.view.kind === 'page' ? pages : widgets).get(m.view.id) as ViewDef<Sdk> | undefined;
  if (!def) throw new Error(`The plugin did not register the ${m.view.kind} "${m.view.id}" declared in its manifest.`);
  const el = document.getElementById('root')!;
  createRoot(el).render(
    <Boundary>
      <Mount def={def} sdk={sdk} />
    </Boundary>,
  );
  if (m.view.kind === 'widget') {
    const report = () => send({ la: 'plugin', t: 'size', height: el.scrollHeight });
    new ResizeObserver(report).observe(el);
    report();
  }
}

let started = false;
window.addEventListener('message', (ev: MessageEvent) => {
  if (ev.source !== window.parent) return;
  const m = ev.data as HostToFrame;
  if (!m || typeof m !== 'object' || (m as { la?: unknown }).la !== 'plugin') return;
  if (handleReply(m)) return;
  switch (m.t) {
    case 'init':
      if (started) return;
      started = true;
      start(m).catch((e) => send({ la: 'plugin', t: 'failed', message: e instanceof Error ? e.message : String(e) }));
      break;
    case 'theme':
      applyTheme(m.theme);
      break;
    case 'lang':
      setLang(m.lang);
      document.documentElement.lang = getLang();
      break;
  }
});

send({ la: 'plugin', t: 'ready' });
