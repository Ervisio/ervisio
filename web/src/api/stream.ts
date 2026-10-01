import { ApiError, type StreamHandle, type StreamHandlers } from './types';
import { wsUrl } from './base';
import { authEvents, MOCK } from './http';
import { requestUnlock } from './unlock';

interface Chan {
  id: number;
  method: string;
  params: unknown;
  h: StreamHandlers;
  open: boolean;
  retried: boolean;
}

const chans = new Map<number, Chan>();
let ws: WebSocket | null = null;
let connecting = false;
let nextCh = 1;
let backoff = 500;
let reconnectTimer: number | undefined;
const outbox: string[] = [];

/** Helpers for modules that exchange base64 payloads. */
export const toBase64 = (u: Uint8Array): string => {
  let s = '';
  for (let i = 0; i < u.length; i += 0x8000) s += String.fromCharCode(...u.subarray(i, i + 0x8000));
  return btoa(s);
};
export const fromBase64 = (s: string): Uint8Array => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
const fromB64 = fromBase64;

function openFrame(c: Chan) {
  return JSON.stringify({ ch: c.id, op: 'open', method: c.method, params: c.params ?? {}, admin: !!c.h.admin });
}

function rawSend(frame: string) {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(frame);
  else outbox.push(frame);
}

function connect() {
  if (ws || connecting) return;
  connecting = true;
  const sock = new WebSocket(wsUrl('/api/ws'));
  ws = sock;
  sock.onopen = () => {
    connecting = false;
    backoff = 500;
    // channels that survived a reconnect and asked for reopen are re-opened; the rest were failed in onclose
    for (const c of chans.values()) {
      if (c.open && !c.h.reopen) continue;
      sock.send(openFrame(c));
      c.open = true;
    }
    while (outbox.length) sock.send(outbox.shift()!);
  };
  sock.onmessage = (ev) => {
    let f: any;
    try {
      f = JSON.parse(ev.data as string);
    } catch {
      return;
    }
    const c = chans.get(f.ch);
    if (!c) return;
    switch (f.op) {
      case 'data':
        c.h.onData?.(f.b64 && typeof f.data === 'string' ? fromB64(f.data) : f.data);
        break;
      case 'end':
        chans.delete(c.id);
        c.h.onEnd?.();
        break;
      case 'error': {
        const e = new ApiError(f.error?.code ?? 'internal', f.error?.message ?? 'Stream error', f.error?.data);
        if (e.code === 'needs_admin' && !c.retried) {
          c.retried = true;
          requestUnlock(c.method).then(
            () => rawSend(openFrame(c)),
            () => {
              chans.delete(c.id);
              c.h.onError?.(e);
            },
          );
          break;
        }
        chans.delete(c.id);
        c.h.onError?.(e);
        break;
      }
    }
  };
  sock.onclose = (ev) => {
    connecting = false;
    ws = null;
    if (ev.code === 1008) {
      // the session ended: fail everything and let the session context send the user to /login
      for (const c of [...chans.values()]) c.h.onError?.(new ApiError('unauthenticated', 'Session ended.'));
      chans.clear();
      authEvents.dispatchEvent(new Event('unauthenticated'));
      return;
    }
    for (const c of [...chans.values()]) {
      if (c.h.reopen) c.open = false;
      else {
        chans.delete(c.id);
        c.h.onError?.(new ApiError('unavailable', 'Connection lost.'));
      }
    }
    if (chans.size > 0) {
      window.clearTimeout(reconnectTimer);
      reconnectTimer = window.setTimeout(connect, backoff);
      backoff = Math.min(backoff * 2, 10_000);
    }
  };
  sock.onerror = () => sock.close();
}

/**
 * Open a multiplexed stream. Returns immediately; data arrives through the handlers.
 * Remember to call close() (e.g. in a useEffect cleanup).
 */
export function stream<T = any>(method: string, params: unknown, h: StreamHandlers<T> = {}): StreamHandle {
  if (MOCK) {
    queueMicrotask(() => h.onError?.(new ApiError('unavailable', 'Streams are not available in mock mode.')));
    return { send() {}, close() {} };
  }
  const c: Chan = { id: nextCh++, method, params, h: h as StreamHandlers, open: false, retried: false };
  chans.set(c.id, c);
  connect();
  if (ws && ws.readyState === WebSocket.OPEN) {
    c.open = true;
    ws.send(openFrame(c));
  }
  return {
    send(data: unknown) {
      if (!chans.has(c.id)) return;
      const frame = { ch: c.id, op: 'input', data };
      rawSend(JSON.stringify(frame));
    },
    close() {
      if (!chans.has(c.id)) return;
      chans.delete(c.id);
      rawSend(JSON.stringify({ ch: c.id, op: 'close' }));
      if (chans.size === 0 && ws) {
        // keep the socket for a moment: sections often close and reopen streams on navigation
        window.setTimeout(() => {
          if (chans.size === 0 && ws) {
            const s = ws;
            ws = null;
            s.onclose = null;
            s.close();
          }
        }, 15_000);
      }
    },
  };
}
