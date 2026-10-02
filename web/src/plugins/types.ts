import type { ComponentType } from 'react';

/** A plugins.list entry, as far as the app shell needs it. */
export interface PluginManifest {
  id: string;
  name: string;
  version: string;
  author?: string;
  entry: string;
  icon?: string;
  color?: string;
  enabled?: boolean;
  blocked?: boolean;
  /** Unsigned plugin from a dev folder, running because developer mode is on. */
  devUnsigned?: boolean;
  location?: string;
  capabilities?: {
    commands?: { name: string; admin?: boolean; adminUnlessGroup?: string; args?: unknown[]; pty?: boolean; remote?: string }[];
    http?: { name: string; socket: string; admin?: boolean; adminUnlessGroup?: string; headers?: string[]; rules?: { methods: string[]; path: string }[]; maxBody?: number; timeoutSec?: number; remote?: string }[];
    /** A path (SDK v2) or {path, admin, adminUnlessGroup, create} (SDK v3). */
    files?: { read?: PluginFolder[]; write?: PluginFolder[] };
    sockets?: string[];
    network?: string[];
    jobs?: { name: string; description?: string; params?: { name: string; pattern: string; description?: string; default?: string }[]; steps?: unknown[] }[];
    notify?: boolean;
    /** The plugin may ask an administrator to approve more hosts (sdk.network.request). */
    userHosts?: boolean;
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
  /** Id of an environment from sdk.envs.list(); the capability must declare `remote`. */
  env?: string;
}

/** An environment an admin configured (Settings > Environments) that the user may use. Never holds secrets. */
export interface PluginEnv {
  id: string;
  name: string;
  kind: 'tcp-tls' | 'ssh' | 'portainer-agent' | 'ervisio';
  status?: { reachable: boolean; engineVersion?: string; apiVersion?: string; latencyMs: number; error?: string; checked: string };
}

export interface HttpResponse {
  status: number;
  headers: Record<string, string>;
  body: string;
  json(): any;
  bytes(): Uint8Array;
}

export interface UploadOptions {
  onProgress?(p: { loaded: number; total: number }): void;
  onResponseStart?(status: number, headers: Record<string, string>): void;
  onResponseData?(chunk: Uint8Array): void;
}

export interface DownloadStarted {
  /** The name the browser saves the file as (cleaned). */
  filename: string;
  /** Bytes, when the service sent a Content-Length. */
  size?: number;
  status?: number;
}

export interface AuditQuery {
  user?: string;
  action?: string;
  /** Substring of the target or message. */
  text?: string;
  /** Milliseconds since the epoch, or an ISO 8601 time. */
  since?: number | string;
  until?: number | string;
  /** 1 to 1000 (default 100). */
  limit?: number;
  /** `next` of the previous page. */
  cursor?: string;
}

export interface AuditEntry {
  time: string;
  user: string;
  ip?: string;
  source: 'plugin' | 'core';
  plugin?: string;
  /** command, pty, http, upload, download, file.write, file.mkdir, file.remove (plugins); login, settings... (console). */
  action: string;
  /** The HTTP API a request went to. */
  via?: string;
  /** "METHOD /path?query" or "command arg arg": secrets are removed. */
  target?: string;
  result: 'ok' | 'failed' | 'denied' | 'error';
  /** Exit code or HTTP status. */
  code?: number;
  bytes?: number;
  admin?: boolean;
  detail?: string;
  /** The environment the call was for, when it was not this machine. */
  env?: string;
  /** "via <server> by <user>" when a paired Ervisio server proxied the call. */
  origin?: string;
}

/** A job instance created by a plugin (see docs/api/jobs.md). */
export interface PluginJobInstance {
  id: string;
  plugin: string;
  job: string;
  name: string;
  params: Record<string, string>;
  schedule?: { every?: number; at?: string[]; days?: number[] };
  owner: string;
  enabled: boolean;
  disabledReason?: string;
  needsAdmin: boolean;
  approval?: { by: string; at: number; valid: boolean };
  webhooks: { id: string; label?: string; created: number; lastUsed?: number }[];
  running: boolean;
  nextRun?: number;
  last?: { id: string; trigger: string; status: string; started: number; ended?: number; error?: string };
}

export interface PluginJobRun {
  id: string;
  instance: string;
  trigger: 'schedule' | 'manual' | 'webhook';
  by?: string;
  started: number;
  ended?: number;
  status: 'queued' | 'running' | 'ok' | 'failed' | 'timeout' | 'cancelled';
  error?: string;
  steps: { id: string; kind: string; status: 'ok' | 'failed' | 'skipped'; exitCode?: number; httpStatus?: number; stdout?: string; stderr?: string; error?: string; admin?: boolean; handled?: boolean }[];
}

export interface PluginJobsApi {
  create(o: {
    job: string;
    name?: string;
    params?: Record<string, string>;
    schedule?: { every: number } | { at: string[]; days?: number[] };
    runAs?: string;
    enabled?: boolean;
    confirmAdmin?: boolean;
  }): Promise<PluginJobInstance>;
  list(o?: { job?: string }): Promise<PluginJobInstance[]>;
  get(id: string): Promise<PluginJobInstance>;
  update(id: string, patch: { name?: string; params?: Record<string, string>; schedule?: { every: number } | { at: string[]; days?: number[] } | null; enabled?: boolean; confirmAdmin?: boolean }): Promise<PluginJobInstance>;
  delete(id: string): Promise<void>;
  runNow(id: string): Promise<{ run: string }>;
  history(id: string, limit?: number): Promise<PluginJobRun[]>;
  webhooks: {
    /** The result's `token` and `path` are shown once; build the URL as location.origin + path. */
    create(id: string, label?: string): Promise<{ id: string; label?: string; token: string; path: string }>;
    regenerate(id: string, webhook: string): Promise<{ id: string; label?: string; token: string; path: string }>;
    revoke(id: string, webhook: string): Promise<void>;
  };
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
    exec(command: string, args?: string[], o?: { env?: string }): Promise<ExecResult>;
    /** Same, streamed line by line. */
    execStream(
      command: string,
      args: string[],
      h: { onLine?(stream: 'stdout' | 'stderr', line: string): void; onExit?(code: number): void; onError?(e: PluginError): void },
      o?: { env?: string },
    ): { close(): void };
    /** SDK v3: an HTTP request to a capabilities.http entry. A non-2xx status is a normal result. */
    http(name: string, req: HttpRequestOptions): Promise<HttpResponse>;
    /** SDK v3: same, with the body streamed as it arrives. Closing ends the connection. */
    httpStream(
      name: string,
      req: HttpRequestOptions,
      h: { onStart?(status: number, headers: Record<string, string>): void; onData(chunk: Uint8Array): void; onEnd(): void; onError(e: PluginError): void },
    ): { close(): void };
    /**
     * SDK 0.2: the browser saves the response of a GET to an HTTP API as a file. The bytes stream from the service to
     * the disk (no memory, no size limit); resolves when the download starts, rejects when the service refuses.
     * The file name is cleaned by the daemon.
     */
    download(name: string, req: HttpRequestOptions, filename?: string): Promise<DownloadStarted>;
    /** SDK 0.2: same for the standard output of a command declared in the manifest (not pty). */
    downloadCommand(command: string, args: string[], filename?: string, o?: { env?: string }): Promise<DownloadStarted>;
    /**
     * SDK 0.2: sends a File or Blob as the body of a POST or PUT to an HTTP API, streamed with progress, up to the API's
     * `maxUpload` (default 20 GiB). The result is the service's answer like `http` (a non-2xx status is a normal result;
     * `truncated` when the body was cut at maxBody). `cancel()` stops it and rejects with code "cancelled".
     * Give `onResponseData` (and/or `onResponseStart`) to receive the response as it arrives, for a service that
     * answers with progress while it reads the file (a Docker build): the result's body is then empty.
     */
    upload(
      name: string,
      req: Omit<HttpRequestOptions, 'body'>,
      file: Blob,
      opts?: UploadOptions | ((p: { loaded: number; total: number }) => void),
    ): Promise<HttpResponse & { truncated: boolean }> & { cancel(): void };
    /**
     * SDK 0.2: saves data the plugin already holds (a string, bytes or a Blob) as a browser download. The app does it:
     * a sandboxed frame cannot download or open a blob: URL. At most 64 MiB; the file name is cleaned; no user gesture is
     * needed, but a frame may save at most 10 files and 256 MiB in 30 seconds. Resolves once the download starts.
     */
    saveFile(filename: string, data: string | Uint8Array | Blob, mime?: string): Promise<{ filename: string; size: number }>;
    /** SDK v3: runs a command declared `pty: true` in a terminal. Closing kills it. */
    pty(
      command: string,
      args: string[],
      o: { cols: number; rows: number; env?: string; onData(chunk: Uint8Array): void; onExit(code: number): void; onError(e: PluginError): void },
    ): { write(data: string | Uint8Array): void; resize(cols: number, rows: number): void; close(): void };
    /** SDK 0.2: background jobs (capabilities.jobs). Absent on consoles older than core 0.6: check before use. */
    jobs?: PluginJobsApi;
    /** SDK 0.2: sends a notification to the channels the administrator configured (needs capabilities.notify). */
    notify?(n: { title: string; body?: string; level?: 'info' | 'success' | 'warn' | 'error'; link?: string }): Promise<{ channels: number; delivered: number; failed: number }>;
  };
  /** Same as `api.saveFile`. */
  saveFile(filename: string, data: string | Uint8Array | Blob, mime?: string): Promise<{ filename: string; size: number }>;
  /** SDK 0.2: the activity log, limited to this plugin's entries. Everyone sees their own; administrators see all users. */
  audit: {
    list(q?: AuditQuery): Promise<{ entries: AuditEntry[]; next: string; enabled: boolean }>;
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
  /** Environments (remote Docker hosts) the signed-in user may use. Pass an id as `env` to http, httpStream, exec, execStream or pty. */
  envs: { list(): Promise<PluginEnv[]> };
  /** Hosts beyond the manifest's capabilities.network list (needs capabilities.network.userHosts). */
  network: {
    /** Asks an administrator once to approve `host` (exact host:port; https unless approved as http). Resolves when approved;
     * the app then reloads the plugin's frames (the new host becomes part of their policy). Rejects with code "forbidden" when refused. */
    request(host: string, o?: { scheme?: 'https' | 'http' }): Promise<{ host: string; approved: true; reloading: boolean }>;
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
