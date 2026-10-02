/**
 * Types for the plugin SDK as this plugin uses it (contract version 3, docs/dev/docker-v2-plan.md section 1),
 * and the module-level holder for the SDK object. The SDK exists only after activate() ran: call getSdk() inside
 * functions and components, never at import time.
 */
import type * as ReactNS from 'react';

export interface PluginError extends Error {
  /** forbidden, invalid, not_found, needs_admin, unavailable, ... */
  code: string;
}

export interface ExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  truncated?: boolean;
}

export type Query = Record<string, string | string[]> | string;

export interface HttpRequest {
  method: string;
  /** URL path without query string; it must match a rule of the manifest. */
  path: string;
  query?: Query;
  /** Only headers listed in the manifest's `headers`. */
  headers?: Record<string, string>;
  /** An object is sent as JSON. */
  body?: string | Uint8Array | object;
}

export interface HttpResponse {
  status: number;
  headers: Record<string, string>;
  body: string;
  json(): any;
  bytes(): Uint8Array;
}

export interface HttpStreamHandlers {
  onStart?(status: number, headers: Record<string, string>): void;
  onData(chunk: Uint8Array): void;
  onEnd(): void;
  onError(err: PluginError): void;
}

export interface PtyOptions {
  cols: number;
  rows: number;
  onData(chunk: Uint8Array): void;
  onExit(code: number): void;
  onError(err: PluginError): void;
}

export interface FileEntry {
  name: string;
  type: 'file' | 'dir' | 'link' | 'other';
  size: number;
  mtime: number;
}

export interface Theme {
  id: string;
  name: string;
  kind: 'dark' | 'light';
  vars: Record<string, string>;
}

export interface PluginSDK {
  version: number;
  plugin: { id: string; name: string; version: string };
  view: { kind: 'page' | 'widget'; id: string };
  react: typeof ReactNS;
  /** The app's component kit. Use the typed wrappers in src/kit.ts instead of reading this directly. */
  ui: Record<string, any>;
  api: {
    exec(command: string, args?: string[]): Promise<ExecResult>;
    execStream(
      command: string,
      args: string[],
      h: { onLine?(stream: 'stdout' | 'stderr', line: string): void; onExit?(code: number): void; onError?(e: PluginError): void },
    ): { close(): void };
    http(name: string, req: HttpRequest): Promise<HttpResponse>;
    httpStream(name: string, req: HttpRequest, h: HttpStreamHandlers): { close(): void };
    pty(command: string, args: string[], o: PtyOptions): { write(data: string | Uint8Array): void; resize(cols: number, rows: number): void; close(): void };
  };
  files: {
    read(path: string): Promise<string>;
    readBytes(path: string): Promise<Uint8Array>;
    write(path: string, data: string | Uint8Array): Promise<void>;
    list(path: string): Promise<FileEntry[]>;
    mkdir(path: string): Promise<void>;
    remove(path: string): Promise<void>;
  };
  asset(path: string): Promise<string>;
  open(pageId: string): void;
  registerPage(id: string, view: unknown): void;
  registerWidget(def: { id: string; title?: string; render: unknown }): void;
  registerStrings(dicts: Record<string, Record<string, string>>): void;
  t(key: string, vars?: Record<string, string | number>): string;
  lang(): string;
  theme: { get(): Theme; onChange(cb: () => void): () => void };
}

let current: PluginSDK | undefined;

export function setSdk(s: PluginSDK): void {
  current = s;
}

export function getSdk(): PluginSDK {
  if (!current) throw new Error('The SDK is not ready: it is set in activate().');
  return current;
}
