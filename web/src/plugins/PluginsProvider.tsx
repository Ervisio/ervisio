import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { apiUrl, call, useSession } from '../api';
import type { PluginManifest, PluginPageInfo, PluginSnippet, PluginWidgetInfo } from './types';

export type RegisteredPage = PluginPageInfo;
export interface RailPluginPage {
  plugin: string;
  page: string;
  title: string;
  icon: string;
  /** URL of the plugin's logo, drawn instead of the icon. */
  logo?: string;
  color?: string;
}

interface PluginsValue {
  /** Enabled plugins the user may use (plugins.list without disabled/blocked entries). */
  plugins: PluginManifest[];
  loading: boolean;
  /** Frames that failed to start, by plugin id. */
  errors: Record<string, string>;
  /** Pages declared by manifests: used for the rail and More sheet. */
  railPages: RailPluginPage[];
  pages: PluginPageInfo[];
  widgets: PluginWidgetInfo[];
  /** Terminal snippets declared in manifests (plain data, no plugin code runs). */
  snippets: (PluginSnippet & { plugin: string })[];
  /** Changes on every reload, so open plugin frames restart with the new code. */
  generation: number;
  reload(): void;
  reportError(plugin: string, message: string | null): void;
}
const Ctx = createContext<PluginsValue | null>(null);

/**
 * Lists the plugins and exposes what their manifests contribute. Plugin code never runs in the app:
 * pages and widgets render in sandboxed frames (PluginFrame), which talk to the app only through the broker.
 */
export function PluginsProvider({ children }: { children: ReactNode }) {
  const { status } = useSession();
  const [plugins, setPlugins] = useState<PluginManifest[]>([]);
  const [loading, setLoading] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [generation, setGeneration] = useState(0);

  const reload = useCallback(() => setGeneration((n) => n + 1), []);
  const reportError = useCallback((plugin: string, message: string | null) => {
    setErrors((e) => {
      if (message === null) {
        if (!(plugin in e)) return e;
        const next = { ...e };
        delete next[plugin];
        return next;
      }
      return e[plugin] === message ? e : { ...e, [plugin]: message };
    });
  }, []);

  useEffect(() => {
    if (status !== 'authed') {
      setPlugins([]);
      return;
    }
    let live = true;
    setLoading(true);
    setErrors({});
    call<PluginManifest[] | { plugins: PluginManifest[] } | null>('plugins.list', {})
      .then((r) => (Array.isArray(r) ? r : r?.plugins ?? []).filter((p) => p.enabled !== false && !p.blocked))
      .catch(() => [] as PluginManifest[]) // module not built yet or no plugins: not an error
      .then((list) => {
        if (!live) return;
        setPlugins(list);
        setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [status, generation]);

  const value = useMemo<PluginsValue>(() => {
    const railPages = plugins.flatMap((p) =>
      (p.contributes?.pages ?? []).map((g) => ({ plugin: p.id, page: g.id, title: g.title, icon: g.icon ?? p.icon ?? 'plugins', logo: pluginLogoUrl(p), color: p.color })),
    );
    const pages = plugins.flatMap((p) => (p.contributes?.pages ?? []).map((g) => ({ plugin: p.id, id: g.id, title: g.title, icon: g.icon })));
    const widgets = plugins.flatMap((p) => (p.contributes?.widgets ?? []).map((w) => ({ plugin: p.id, id: w.id, title: w.title, icon: w.icon })));
    const snippets = plugins.flatMap((p) => (p.contributes?.snippets ?? []).map((s) => ({ ...s, plugin: p.id })));
    return { plugins, loading, errors, railPages, pages, widgets, snippets, generation, reload, reportError };
  }, [plugins, loading, errors, generation, reload, reportError]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

/** URL of an installed plugin's logo (the version busts the browser cache after an update), or undefined. */
export function pluginLogoUrl(p: { id: string; version: string; logo?: string }): string | undefined {
  return p.logo ? apiUrl(`/plugins/${p.id}/${p.logo}?v=${encodeURIComponent(p.version)}`) : undefined;
}

export function usePlugins(): PluginsValue {
  const v = useContext(Ctx);
  if (!v) throw new Error('usePlugins must be used inside <PluginsProvider>');
  return v;
}
