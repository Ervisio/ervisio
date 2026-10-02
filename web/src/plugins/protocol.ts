/**
 * postMessage protocol between the app (host) and a plugin's sandboxed frame.
 * Every message carries `la: 'plugin'`. The frame has an opaque origin, so the host posts with targetOrigin '*'
 * and identifies the frame by `event.source` only; it tears the frame down if it ever navigates.
 * Types only: shared by web/src/plugins (host, broker) and web/src/plugins/frame (runtime).
 */

export type ViewKind = 'page' | 'widget';
export interface FrameView {
  kind: ViewKind;
  id: string;
}

export interface FrameTheme {
  id: string;
  name: string;
  kind: 'dark' | 'light';
  vars: Record<string, string>;
  density?: string;
}

/** Operations a plugin may ask for. Everything else is refused by the broker. */
export type FrameOp = 'exec' | 'http' | 'readFile' | 'writeFile' | 'listDir' | 'mkdir' | 'remove' | 'asset' | 'toast' | 'open' | 'openUrl' | 'envs' | 'network';

/** An HTTP request to a capabilities.http entry (sdk.api.http / httpStream). Binary bodies travel as Uint8Array. */
export interface FrameHttpRequest {
  name: string;
  method: string;
  path: string;
  /** Raw query string, without "?". */
  query?: string;
  headers?: Record<string, string>;
  body?: string | Uint8Array;
  /** The body is JSON (sent with Content-Type: application/json). */
  json?: boolean;
  /** Id of an environment (sdk.envs.list()): the request goes to that remote Docker host. */
  env?: string;
}

/** Result of an `http` request: `text` for UTF-8 bodies, `bytes` for binary ones. */
export interface FrameHttpResult {
  status: number;
  headers: Record<string, string>;
  text?: string;
  bytes?: Uint8Array;
}

export type FrameToHost =
  | { la: 'plugin'; t: 'ready' }
  | { la: 'plugin'; t: 'loaded' }
  | { la: 'plugin'; t: 'failed'; message: string }
  | { la: 'plugin'; t: 'size'; height: number }
  | { la: 'plugin'; t: 'req'; id: number; op: FrameOp; args: Record<string, unknown> }
  /** plugins.execStream (no kind, SDK v2) */
  | { la: 'plugin'; t: 'stream-open'; sid: number; kind?: 'exec'; command: string; args: unknown; env?: string }
  /** plugins.httpStream */
  | { la: 'plugin'; t: 'stream-open'; sid: number; kind: 'http'; req: FrameHttpRequest }
  /** plugins.pty */
  | { la: 'plugin'; t: 'stream-open'; sid: number; kind: 'pty'; command: string; args: unknown; cols: number; rows: number; env?: string }
  /** Input to an open pty: bytes to type, or a new size. */
  | { la: 'plugin'; t: 'stream-input'; sid: number; data?: Uint8Array; resize?: { cols: number; rows: number } }
  | { la: 'plugin'; t: 'stream-close'; sid: number };

export interface FrameError {
  code: string;
  message: string;
}

export type HostToFrame =
  | {
      la: 'plugin';
      t: 'init';
      plugin: { id: string; name: string; version: string };
      view: FrameView;
      lang: string;
      theme: FrameTheme;
      /** Source of the plugin's entry module, fetched by the host (the frame cannot reach the daemon). */
      code: string;
    }
  | { la: 'plugin'; t: 'theme'; theme: FrameTheme }
  | { la: 'plugin'; t: 'lang'; lang: string }
  | { la: 'plugin'; t: 'res'; id: number; ok: true; value: unknown }
  | { la: 'plugin'; t: 'res'; id: number; ok: false; error: FrameError }
  | { la: 'plugin'; t: 'stream'; sid: number; ev: 'line'; stream: 'stdout' | 'stderr'; line: string }
  | { la: 'plugin'; t: 'stream'; sid: number; ev: 'exit'; code: number }
  /** httpStream: the response started. */
  | { la: 'plugin'; t: 'stream'; sid: number; ev: 'start'; status: number; headers: Record<string, string> }
  /** httpStream body chunk or pty output. */
  | { la: 'plugin'; t: 'stream'; sid: number; ev: 'data'; chunk: Uint8Array }
  | { la: 'plugin'; t: 'stream'; sid: number; ev: 'end' }
  | { la: 'plugin'; t: 'stream'; sid: number; ev: 'error'; error: FrameError };

export const isFrameMessage = (d: unknown): d is FrameToHost =>
  !!d && typeof d === 'object' && (d as { la?: unknown }).la === 'plugin' && typeof (d as { t?: unknown }).t === 'string';
