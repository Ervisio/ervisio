/**
 * Frame side of the plugin protocol (web/src/plugins/protocol.ts): requests to the host broker and streams.
 * Runs inside the sandboxed plugin frame, never in the app.
 */
import type { FrameError, FrameOp, FrameToHost, HostToFrame } from '../protocol';

export class PluginError extends Error {
  code: string;
  constructor(e: FrameError) {
    super(e.message);
    this.name = 'PluginError';
    this.code = e.code;
  }
}

export const send = (m: FrameToHost) => window.parent.postMessage(m, '*');

let nextId = 1;
const pending = new Map<number, { resolve(v: unknown): void; reject(e: unknown): void }>();

export function request<T = unknown>(op: FrameOp, args: Record<string, unknown> = {}): Promise<T> {
  const id = nextId++;
  return new Promise<T>((resolve, reject) => {
    pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
    send({ la: 'plugin', t: 'req', id, op, args });
  });
}

export interface StreamCallbacks {
  onLine?(stream: 'stdout' | 'stderr', line: string): void;
  onExit?(code: number): void;
  onError?(e: PluginError): void;
}
let nextSid = 1;
const streams = new Map<number, StreamCallbacks>();

export function openStream(command: string, args: string[], h: StreamCallbacks): { close(): void } {
  const sid = nextSid++;
  streams.set(sid, h);
  send({ la: 'plugin', t: 'stream-open', sid, command, args });
  return {
    close() {
      if (streams.delete(sid)) send({ la: 'plugin', t: 'stream-close', sid });
    },
  };
}

/** Handles responses and stream events; returns false for other messages. */
export function handleReply(m: HostToFrame): boolean {
  if (m.t === 'res') {
    const p = pending.get(m.id);
    pending.delete(m.id);
    if (p) {
      if (m.ok) p.resolve(m.value);
      else p.reject(new PluginError(m.error));
    }
    return true;
  }
  if (m.t === 'stream') {
    const h = streams.get(m.sid);
    if (!h) return true;
    switch (m.ev) {
      case 'line':
        h.onLine?.(m.stream, m.line);
        break;
      case 'exit':
        h.onExit?.(m.code);
        break;
      case 'end':
        streams.delete(m.sid);
        break;
      case 'error':
        streams.delete(m.sid);
        h.onError?.(new PluginError(m.error));
        break;
    }
    return true;
  }
  return false;
}

/** Toasts appear in the app (prefixed with the plugin name), not inside the frame. */
export const toast = {
  show(t: { title: string; detail?: string; tone?: 'ok' | 'err' | 'info' | string }): number {
    void request('toast', { tone: t.tone, title: t.title, detail: t.detail }).catch(() => undefined);
    return 0;
  },
  ok: (title: string, detail?: string) => toast.show({ title, detail, tone: 'ok' }),
  err: (title: string, detail?: string) => toast.show({ title, detail, tone: 'err' }),
  info: (title: string, detail?: string) => toast.show({ title, detail, tone: 'info' }),
  undo: (title: string) => toast.show({ title, tone: 'ok' }),
  update: () => undefined,
  dismiss: () => undefined,
};
