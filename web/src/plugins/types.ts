import type { ComponentType } from 'react';

/** A plugins.list entry, as far as the app shell needs it. */
export interface PluginManifest {
  id: string;
  name: string;
  version: string;
  author?: string;
  entry: string;
  icon?: string;
  /** Logo file at the root of the plugin folder (logo.svg or logo.png), served at /plugins/<id>/<logo>. */
  logo?: string;
  color?: string;
  enabled?: boolean;
  blocked?: boolean;
  /** Unsigned plugin from a dev folder, running because developer mode is on. */
  devUnsigned?: boolean;
  location?: string;
  capabilities?: {
    commands?: { name: string; admin?: boolean; adminUnlessGroup?: string; args?: unknown[]; pty?: boolean }[];
    http?: { name: string; socket: string; admin?: boolean; adminUnlessGroup?: string; headers?: string[]; rules?: { methods: string[]; path: string }[]; maxBody?: number; timeoutSec?: number }[];
    /** A path (SDK v2) or {path, admin, adminUnlessGroup, create} (SDK v3). */
    files?: { read?: PluginFolder[]; write?: PluginFolder[] };
    sockets?: string[];
    network?: string[];
  };
  contributes?: {
    pages?: { id: string; title: string; icon?: string }[];
    widgets?: { id: string; title: string; icon?: string }[];
    snippets?: { name: string; command: string }[];
  };
}

export type PluginFolder = string | { path: string; admin?: boolean; adminUnlessGroup?: string; create?: boolean };

/** A page contributed by a plugin (from its manifest). It renders in the plugin's sandboxed frame. */
export interface PluginPageInfo {
  plugin: string;
  id: string;
  title: string;
  icon?: string;
}

/** A widget contributed by a plugin (from its manifest). It renders in a small sandboxed frame. */
export interface PluginWidgetInfo {
  plugin: string;
  id: string;
  title: string;
  icon?: string;
  /** Preferred width in grid columns (not declared by manifests yet: the Overview picks its default). */
  cols?: number;
}

export interface PluginSnippet {
  name: string;
  command: string;
}

/* ------------------------------------------------------------------------------------------------
 * The SDK object a plugin module receives inside its frame (contract version 3). Documented, with TypeScript
 * types for plugin authors, in https://github.com/Ervisio/plugin-sdk (keep both in step).
 * Kept here as the reference typing for plugin authors; the implementation is web/src/plugins/frame/runtime.tsx.
 * ---------------------------------------------------------------------------------------------- */

export interface ExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  truncated?: boolean;
}

export interface PluginError extends Error {
  code: string;
}

/** Request of sdk.api.http / httpStream. An object body is sent as JSON. */
export interface HttpRequestOptions {
  method: string;
  /** URL path, matched against the manifest's rules; no query string. */
  path: string;
  query?: Record<string, string | string[]> | string;
  headers?: Record<string, string>;
  body?: string | Uint8Array | object;
}

export interface HttpResponse {
  status: number;
  headers: Record<string, string>;
  body: string;
  json(): any;
  bytes(): Uint8Array;
}

export type PluginViewDef<S = PluginSDK> = ComponentType<{ sdk: S }> | { render(container: HTMLElement, sdk: S): void | (() => void) };

export interface PluginSDK {
  version: 3;
  plugin: { id: string; name: string; version: string };
  /** What this frame shows: one page or one widget of the plugin. */
  view: { kind: 'page' | 'widget'; id: string };
  react: typeof import('react');
  /** The component kit, same look as the app (Button, Table, Card, StatCard, Dialog, Tabs, Badge, toast, ...). */
  ui: Record<string, unknown>;
  api: {
    /** Run a command declared in the manifest. Admin commands open the app's unlock dialog when needed. */
    exec(command: string, args?: string[]): Promise<ExecResult>;
    /** Same, streamed line by line. */
    execStream(
      command: string,
      args: string[],
      h: { onLine?(stream: 'stdout' | 'stderr', line: string): void; onExit?(code: number): void; onError?(e: PluginError): void },
    ): { close(): void };
    /** SDK v3: an HTTP request to a capabilities.http entry. A non-2xx status is a normal result. */
    http(name: string, req: HttpRequestOptions): Promise<HttpResponse>;
    /** SDK v3: same, with the body streamed as it arrives. Closing ends the connection. */
    httpStream(
      name: string,
      req: HttpRequestOptions,
      h: { onStart?(status: number, headers: Record<string, string>): void; onData(chunk: Uint8Array): void; onEnd(): void; onError(e: PluginError): void },
    ): { close(): void };
    /** SDK v3: runs a command declared `pty: true` in a terminal. Closing kills it. */
    pty(
      command: string,
      args: string[],
      o: { cols: number; rows: number; onData(chunk: Uint8Array): void; onExit(code: number): void; onError(e: PluginError): void },
    ): { write(data: string | Uint8Array): void; resize(cols: number, rows: number): void; close(): void };
  };
  /** Only inside capabilities.files (read: read+write folders; write: write folders), with the user's own rights (admin folders: administrator rights when needed). */
  files: {
    read(path: string): Promise<string>;
    readBytes(path: string): Promise<Uint8Array>;
    write(path: string, data: string | Uint8Array): Promise<void>;
    list(path: string): Promise<{ name: string; type: 'file' | 'dir' | 'link' | 'other'; size: number; mtime: number }[]>;
    /** SDK v3: creates a folder (and missing parents) inside a write folder. */
    mkdir(path: string): Promise<void>;
    /** SDK v3: removes a file or an empty folder inside a write folder. */
    remove(path: string): Promise<void>;
  };
  /** A blob: URL for a file of the plugin's own folder (images, CSS...). */
  asset(path: string): Promise<string>;
  /** Open one of the plugin's own pages in the app. */
  open(pageId: string): void;
  /** Opens an http(s) address in a new browser tab (the frame itself cannot open pop-ups). */
  openExternal(url: string): Promise<void>;
  registerPage(id: string, page: PluginViewDef): void;
  registerWidget(def: { id: string; title?: string; render: PluginViewDef }): void;
  /** Deprecated: snippets come from manifest.contributes.snippets. No-op. */
  registerSnippet(s: PluginSnippet): void;
  registerStrings(dicts: Record<string, Record<string, string>>): void;
  t(key: string, vars?: Record<string, string | number>): string;
  lang(): string;
  theme: {
    get(): { id: string; name: string; kind: 'dark' | 'light'; vars: Record<string, string> };
    onChange(cb: () => void): () => void;
  };
}

export type PluginModule = ((sdk: PluginSDK) => void | Promise<void>) | { activate(sdk: PluginSDK): void | Promise<void> };
