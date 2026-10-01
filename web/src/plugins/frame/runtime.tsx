/**
 * Plugin frame runtime, built by `npm run build:runtime` into public/plugin-runtime.js (vite.runtime.config.ts)
 * and loaded by the daemon's /plugin-frame/<id> page inside <iframe sandbox="allow-scripts">.
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
import type { FrameTheme, FrameView, HostToFrame } from '../protocol';
import { handleReply, openStream, PluginError, request, send } from './channel';
import { getLang, setLang, subscribeLang } from './i18n-shim';
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

/* ---------- SDK ---------- */
function makeSdk(plugin: { id: string; name: string; version: string }, view: FrameView) {
  const pages = new Map<string, ViewDef<unknown>>();
  const widgets = new Map<string, ViewDef<unknown>>();
  const strings: Record<string, Record<string, string>> = {};
  const assets = new Map<string, Promise<string>>();
  const sdk = {
    version: 2 as const,
    plugin,
    view,
    react: React,
    ui: kit,
    api: {
      exec: (command: string, args: string[] = []) => request<{ stdout: string; stderr: string; exitCode: number; truncated?: boolean }>('exec', { command, args }),
      execStream: (command: string, args: string[], h: Parameters<typeof openStream>[2]) => openStream(command, args ?? [], h ?? {}),
      call: () => Promise.reject(new PluginError({ code: 'forbidden', message: 'sdk.api.call is not available to plugins (SDK v2): use sdk.api.exec with a declared command.' })),
      stream: () => {
        throw new PluginError({ code: 'forbidden', message: 'sdk.api.stream is not available to plugins (SDK v2): use sdk.api.execStream.' });
      },
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
