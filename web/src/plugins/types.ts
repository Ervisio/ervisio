import type { ComponentType } from 'react';

export interface PluginManifest {
  id: string;
  name: string;
  version: string;
  author?: string;
  entry: string;
  icon?: string;
  color?: string;
  enabled?: boolean;
  contributes?: {
    pages?: { id: string; title: string; icon?: string }[];
    widgets?: { id: string; title: string; icon?: string }[];
    snippets?: { name: string; command: string }[];
  };
}

export interface PluginPageContext {
  /** Container the page owns (only for the framework-free `render` form). */
  sdk: PluginSDK;
}

export type PluginPageDef =
  | ComponentType<{ sdk: PluginSDK }>
  | { render(container: HTMLElement, sdk: PluginSDK): void | (() => void) };

export interface PluginWidgetDef {
  id: string;
  title: string;
  icon?: string;
  /** Preferred size in grid columns (of 12). */
  cols?: number;
  render: PluginPageDef;
}

export interface PluginSnippet {
  name: string;
  command: string;
}

export interface PluginSDK {
  /** SDK contract version. */
  version: 1;
  plugin: { id: string; name: string; version: string; baseUrl: string };
  api: {
    call: typeof import('../api').call;
    stream: typeof import('../api').stream;
    /** Run a command declared in the manifest: call('plugins.exec', {plugin, command, args}). */
    exec<T = unknown>(command: string, args?: string[], opts?: { admin?: boolean }): Promise<T>;
  };
  /** The component kit, same objects the app uses. */
  ui: typeof import('../ui');
  /** The host's React, so plugin bundles do not ship their own copy. */
  react: typeof import('react');
  registerPage(id: string, page: PluginPageDef): void;
  registerWidget(def: PluginWidgetDef): void;
  registerSnippet(s: PluginSnippet): void;
  /** Provide your own translations: { en: {key: 'text'}, it: {...} }. */
  registerStrings(dicts: Record<string, Record<string, string>>): void;
  t(key: string, vars?: Record<string, string | number>): string;
  theme: {
    /** Current theme id, kind and resolved CSS variables. */
    get(): { id: string; name: string; kind: 'dark' | 'light'; vars: Record<string, string> };
    onChange(cb: () => void): () => void;
  };
}

export type PluginModule = ((sdk: PluginSDK) => void | Promise<void>) | { activate(sdk: PluginSDK): void | Promise<void> };
