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
export type FrameOp = 'exec' | 'readFile' | 'writeFile' | 'listDir' | 'asset' | 'toast' | 'open';

export type FrameToHost =
  | { la: 'plugin'; t: 'ready' }
  | { la: 'plugin'; t: 'loaded' }
  | { la: 'plugin'; t: 'failed'; message: string }
  | { la: 'plugin'; t: 'size'; height: number }
  | { la: 'plugin'; t: 'req'; id: number; op: FrameOp; args: Record<string, unknown> }
  | { la: 'plugin'; t: 'stream-open'; sid: number; command: string; args: unknown }
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
  | { la: 'plugin'; t: 'stream'; sid: number; ev: 'end' }
  | { la: 'plugin'; t: 'stream'; sid: number; ev: 'error'; error: FrameError };

export const isFrameMessage = (d: unknown): d is FrameToHost =>
  !!d && typeof d === 'object' && (d as { la?: unknown }).la === 'plugin' && typeof (d as { t?: unknown }).t === 'string';
