/**
 * Frame side of the plugin protocol (web/src/plugins/protocol.ts): requests to the host broker and streams.
 * Runs inside the sandboxed plugin frame, never in the app.
 */
import type { FrameError, FrameHttpRequest, FrameOp, FrameToHost, FrameUploadResult, HostToFrame } from '../protocol';

export class PluginError extends Error {
  code: string;
  constructor(e: FrameError) {
    super(e.message);
    this.name = 'PluginError';
    this.code = e.code;
  }
}

export const send = (m: FrameToHost, transfer?: Transferable[]) => window.parent.postMessage(m, '*', transfer ?? []);

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
/** Every stream kind goes through one handler table. */
interface AnyCallbacks extends StreamCallbacks {
  onStart?(status: number, headers: Record<string, string>): void;
  onData?(chunk: Uint8Array): void;
  onEnd?(): void;
}
let nextSid = 1;
const streams = new Map<number, AnyCallbacks>();

type OpenSpec<T = Extract<FrameToHost, { t: 'stream-open' }>> = T extends unknown ? Omit<T, 'la' | 't' | 'sid'> : never;

function open(m: OpenSpec, h: AnyCallbacks) {
  const sid = nextSid++;
  streams.set(sid, h);
  send({ la: 'plugin', t: 'stream-open', sid, ...m } as FrameToHost);
  return {
    sid,
    close() {
      if (streams.delete(sid)) send({ la: 'plugin', t: 'stream-close', sid });
    },
  };
}

export function openStream(command: string, args: string[], h: StreamCallbacks, env?: string): { close(): void } {
  const { close } = open({ command, args, ...(env ? { env } : {}) }, h);
  return { close };
}

export interface HttpStreamCallbacks {
  onStart?(status: number, headers: Record<string, string>): void;
  onData?(chunk: Uint8Array): void;
  onEnd?(): void;
  onError?(e: PluginError): void;
}

export function openHttpStream(req: FrameHttpRequest, h: HttpStreamCallbacks): { close(): void } {
  const { close } = open({ kind: 'http', req }, h);
  return { close };
}

export interface PtyCallbacks {
  onData?(chunk: Uint8Array): void;
  onExit?(code: number): void;
  onError?(e: PluginError): void;
}

export function openPty(command: string, args: string[], cols: number, rows: number, h: PtyCallbacks, env?: string) {
  const s = open({ kind: 'pty', command, args, cols, rows, ...(env ? { env } : {}) }, h);
  const enc = new TextEncoder();
  return {
    write(data: string | Uint8Array) {
      if (!streams.has(s.sid)) return;
      const bytes = typeof data === 'string' ? enc.encode(data) : data instanceof Uint8Array ? data : null;
      if (!bytes) return;
      // Copies in 64 KiB pieces (a paste can be large); each copy is handed over, the caller's buffer stays usable.
      for (let i = 0; i < bytes.length; i += 64 << 10) {
        const part = bytes.slice(i, i + (64 << 10));
        send({ la: 'plugin', t: 'stream-input', sid: s.sid, data: part }, [part.buffer]);
      }
    },
    resize(cols: number, rows: number) {
      if (streams.has(s.sid)) send({ la: 'plugin', t: 'stream-input', sid: s.sid, resize: { cols: Math.floor(cols), rows: Math.floor(rows) } });
    },
    close: s.close,
  };
}

export interface UploadHandle {
  /** Resolves with the service's answer; rejects with code "cancelled" after cancel(). */
  result: Promise<FrameUploadResult>;
  cancel(): void;
}

let nextUid = 1;
export interface UploadCallbacks {
  onProgress?(loaded: number, total: number): void;
  /** Given, the response is streamed: called when the service answers... */
  onResponseStart?(status: number, headers: Record<string, string>): void;
  /** ...and with each piece of its body as it arrives. */
  onResponseData?(chunk: Uint8Array): void;
}
const uploads = new Map<number, UploadCallbacks & { resolve(r: FrameUploadResult): void; reject(e: unknown): void }>();

/** plugins.upload: hands the file to the host, which streams it; progress and the result come back as messages. */
export function openUpload(req: FrameHttpRequest, file: Blob, o: UploadCallbacks = {}): UploadHandle {
  const uid = nextUid++;
  const result = new Promise<FrameUploadResult>((resolve, reject) => {
    uploads.set(uid, { resolve, reject, ...o });
  });
  send({ la: 'plugin', t: 'upload-open', uid, req, file, ...(o.onResponseData || o.onResponseStart ? { stream: true } : {}) });
  return {
    result,
    cancel() {
      if (uploads.has(uid)) send({ la: 'plugin', t: 'upload-cancel', uid });
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
  if (m.t === 'upload') {
    const u = uploads.get(m.uid);
    if (!u) return true;
    if (m.ev === 'progress' || m.ev === 'start' || m.ev === 'data') {
      try {
        if (m.ev === 'progress') u.onProgress?.(m.loaded, m.total);
        else if (m.ev === 'start') u.onResponseStart?.(m.status, m.headers);
        else u.onResponseData?.(m.chunk);
      } catch (e) {
        console.warn(e);
      }
    } else {
      uploads.delete(m.uid);
      if (m.ev === 'done') u.resolve(m.result);
      else u.reject(new PluginError(m.error));
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
      case 'start':
        h.onStart?.(m.status, m.headers);
        break;
      case 'data':
        h.onData?.(m.chunk);
        break;
      case 'end':
        streams.delete(m.sid);
        h.onEnd?.();
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
