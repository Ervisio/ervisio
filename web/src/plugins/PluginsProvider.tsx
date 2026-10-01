import * as React from 'react';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { call, stream, useSession } from '../api';
import { apiUrl } from '../api/base';
import { useI18n } from '../i18n';
import { themeVars, useTheme } from '../theme';
import * as ui from '../ui';
import type { PluginManifest, PluginModule, PluginPageDef, PluginSDK, PluginSnippet, PluginWidgetDef } from './types';

export interface RegisteredPage {
  plugin: string;
  id: string;
  def: PluginPageDef;
  sdk: PluginSDK;
}
export interface RailPluginPage {
  plugin: string;
  page: string;
  title: string;
  icon: string;
  color?: string;
}

interface PluginsValue {
  plugins: PluginManifest[];
  loading: boolean;
  errors: Record<string, string>;
  /** Pages declared by manifests: used for the rail and More sheet even before the module has loaded. */
  railPages: RailPluginPage[];
  pages: RegisteredPage[];
  widgets: (PluginWidgetDef & { plugin: string })[];
  snippets: (PluginSnippet & { plugin: string })[];
  reload(): void;
}
const Ctx = createContext<PluginsValue | null>(null);

export function PluginsProvider({ children }: { children: ReactNode }) {
  const { status } = useSession();
  const { lang } = useI18n();
  const theme = useTheme();
  const [plugins, setPlugins] = useState<PluginManifest[]>([]);
  const [loading, setLoading] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [pages, setPages] = useState<RegisteredPage[]>([]);
  const [widgets, setWidgets] = useState<PluginsValue['widgets']>([]);
  const [snippets, setSnippets] = useState<PluginsValue['snippets']>([]);
  const [nonce, setNonce] = useState(0);
  const langRef = useRef(lang);
  langRef.current = lang;
  const themeRef = useRef(theme);
  themeRef.current = theme;
  const listeners = useRef(new Set<() => void>());
  useEffect(() => listeners.current.forEach((l) => l()), [theme.theme, theme.colourMode, theme.distroColour]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);

  useEffect(() => {
    if (status !== 'authed') {
      setPlugins([]);
      return;
    }
    let live = true;
    setLoading(true);
    setPages([]);
    setWidgets([]);
    setSnippets([]);
    setErrors({});
    (async () => {
      let list: PluginManifest[];
      try {
        const r = await call<PluginManifest[] | { plugins: PluginManifest[] } | null>('plugins.list', {});
        list = (Array.isArray(r) ? r : r?.plugins ?? []).filter((p) => p.enabled !== false);
      } catch {
        list = []; // module not built yet or no plugins: not an error
      }
      if (!live) return;
      setPlugins(list);
      await Promise.all(
        list.map(async (m) => {
          const strings: Record<string, Record<string, string>> = {};
          const sdk: PluginSDK = {
            version: 1,
            plugin: { id: m.id, name: m.name, version: m.version, baseUrl: apiUrl(`/plugins/${m.id}/`) },
            api: {
              call,
              stream,
              exec: (command, args = [], opts) => call('plugins.exec', { plugin: m.id, command, args }, opts),
            },
            ui,
            react: React,
            registerPage: (id, def) => live && setPages((p) => [...p.filter((x) => !(x.plugin === m.id && x.id === id)), { plugin: m.id, id, def, sdk }]),
            registerWidget: (def) => live && setWidgets((w) => [...w.filter((x) => !(x.plugin === m.id && x.id === def.id)), { ...def, plugin: m.id }]),
            registerSnippet: (s) => live && setSnippets((x) => [...x, { ...s, plugin: m.id }]),
            registerStrings: (d) => Object.assign(strings, d),
            t: (key, vars) => {
              const s = strings[langRef.current]?.[key] ?? strings.en?.[key] ?? key;
              return vars ? s.replace(/\{(\w+)\}/g, (x, k) => (k in vars ? String(vars[k]) : x)) : s;
            },
            theme: {
              get: () => {
                const t = themeRef.current;
                return { id: t.theme.id, name: t.theme.name, kind: t.theme.kind, vars: themeVars(t.theme, t.colourMode, t.distroColour) };
              },
              onChange: (cb) => {
                listeners.current.add(cb);
                return () => void listeners.current.delete(cb);
              },
            },
          };
          try {
            const mod = (await import(/* @vite-ignore */ apiUrl(`/plugins/${m.id}/${m.entry}`))) as { default?: PluginModule; activate?: (sdk: PluginSDK) => void };
            const fn = mod.default ?? mod.activate;
            if (typeof fn === 'function') await fn(sdk);
            else if (fn && typeof (fn as { activate?: unknown }).activate === 'function') await (fn as { activate(s: PluginSDK): void }).activate(sdk);
          } catch (e) {
            console.warn(`plugin ${m.id} failed to load`, e);
            if (live) setErrors((x) => ({ ...x, [m.id]: e instanceof Error ? e.message : String(e) }));
          }
        }),
      );
      if (live) setLoading(false);
    })();
    return () => {
      live = false;
    };
  }, [status, nonce]);

  const railPages = useMemo<RailPluginPage[]>(
    () =>
      plugins.flatMap((p) =>
        (p.contributes?.pages ?? []).map((g) => ({ plugin: p.id, page: g.id, title: g.title, icon: g.icon ?? p.icon ?? 'plugins', color: p.color })),
      ),
    [plugins],
  );

  const value = useMemo(() => ({ plugins, loading, errors, railPages, pages, widgets, snippets, reload }), [plugins, loading, errors, railPages, pages, widgets, snippets, reload]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function usePlugins(): PluginsValue {
  const v = useContext(Ctx);
  if (!v) throw new Error('usePlugins must be used inside <PluginsProvider>');
  return v;
}
