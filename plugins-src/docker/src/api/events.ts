/** One shared /events stream, opened while at least one listener exists. Reconnects with a growing delay. */
import { docker, type StreamHandle } from './engine';
import { JsonLines } from './streams';
import type { DockerEvent } from './types';

type Listener = (ev: DockerEvent) => void;
const listeners = new Set<Listener>();
let handle: StreamHandle | undefined;
let retry: ReturnType<typeof setTimeout> | undefined;
let failures = 0;
let lastTime = 0;

function connect(): void {
  if (handle || !listeners.size) return;
  const lines = new JsonLines<DockerEvent>((ev) => {
    failures = 0;
    lastTime = ev.time || lastTime;
    listeners.forEach((l) => l(ev));
  });
  const done = () => {
    if (!handle) return;
    handle = undefined;
    if (!listeners.size) return;
    failures++;
    retry = setTimeout(connect, Math.min(15000, 1000 * 2 ** Math.min(failures, 4)));
    // Anything may have changed while the stream was down.
    const gap: DockerEvent = { Type: 'system', Action: 'reconnect', time: Math.floor(Date.now() / 1000) };
    listeners.forEach((l) => l(gap));
  };
  handle = docker.stream('GET', '/events', lastTime ? { query: { since: String(lastTime + 1) } } : {}, {
    onData: (c) => lines.push(c),
    onEnd: done,
    onError: done,
  });
}

/** Calls `fn` for every Engine event (container, image, volume, network, daemon). Returns the unsubscribe function. */
export function subscribeEvents(fn: Listener): () => void {
  listeners.add(fn);
  connect();
  return () => {
    listeners.delete(fn);
    if (!listeners.size) {
      clearTimeout(retry);
      handle?.close();
      handle = undefined;
      failures = 0;
    }
  };
}
