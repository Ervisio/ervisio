/**
 * A small shared store for data that is fetched from the Engine, polled, and refreshed by events.
 * One store = one fetcher. While at least one component uses it, it polls and listens to /events; with no users it
 * is idle. Several components using the same store share one request.
 */
import { useEffect, useSyncExternalStore } from 'react';
import { classify, type ErrorInfo } from './engine';
import { subscribeEvents } from './events';
import type { DockerEvent } from './types';

export interface ResourceState<T> {
  data: T | undefined;
  error: ErrorInfo | null;
  /** True until the first answer (or error) arrives. */
  loading: boolean;
  updatedAt: number;
}

export interface Resource<T> {
  /** Hook: the current state. Subscribes this component to the store. */
  use(): ResourceState<T>;
  /** Fetch now (joins a request already in flight). */
  refresh(): Promise<void>;
  /** The state without subscribing (for event handlers). */
  peek(): ResourceState<T>;
}

export interface ResourceOptions {
  /** Poll every this many ms while used. */
  intervalMs: number;
  /** Engine events that should trigger a refresh. Omit for none. */
  refreshOn?: (ev: DockerEvent) => boolean;
}

export function createResource<T>(fetcher: () => Promise<T>, opts: ResourceOptions): Resource<T> {
  let state: ResourceState<T> = { data: undefined, error: null, loading: true, updatedAt: 0 };
  const subs = new Set<() => void>();
  let timer: ReturnType<typeof setInterval> | undefined;
  let unEvents: (() => void) | undefined;
  let debounce: ReturnType<typeof setTimeout> | undefined;
  let inflight: Promise<void> | undefined;

  const set = (s: ResourceState<T>) => {
    state = s;
    subs.forEach((f) => f());
  };

  const refresh = (): Promise<void> => {
    if (inflight) return inflight;
    // A request that hangs (a busy daemon) must not block every later refresh: give up after 20 s.
    const timeout = new Promise<never>((_, reject) => setTimeout(() => reject(new Error('Docker did not answer in time')), 20000));
    inflight = Promise.race([fetcher(), timeout])
      .then((data) => set({ data, error: null, loading: false, updatedAt: Date.now() }))
      .catch((e) => set({ data: state.data, error: classify(e), loading: false, updatedAt: state.updatedAt }))
      .finally(() => {
        inflight = undefined;
      });
    return inflight;
  };

  const start = () => {
    void refresh();
    timer = setInterval(() => {
      if (!document.hidden) void refresh();
    }, opts.intervalMs);
    if (opts.refreshOn) {
      const match = opts.refreshOn;
      unEvents = subscribeEvents((ev) => {
        if (!match(ev)) return;
        clearTimeout(debounce);
        debounce = setTimeout(() => void refresh(), 350);
      });
    }
  };

  const stop = () => {
    clearInterval(timer);
    clearTimeout(debounce);
    unEvents?.();
    timer = undefined;
    unEvents = undefined;
  };

  const subscribe = (f: () => void) => {
    subs.add(f);
    if (subs.size === 1) start();
    return () => {
      subs.delete(f);
      if (!subs.size) stop();
    };
  };

  return {
    use: () => useSyncExternalStore(subscribe, () => state),
    refresh,
    peek: () => state,
  };
}

/** Runs `fn` once when a component mounts and returns its cleanup on unmount (thin wrapper, keeps views terse). */
export function useMount(fn: () => void | (() => void)): void {
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(fn, []);
}
