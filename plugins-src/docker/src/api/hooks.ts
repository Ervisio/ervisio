import { useEffect, useRef, useState } from 'react';
import { subscribeEvents } from './events';
import { statsHistory, statsLast, subscribeStats } from './stats';
import type { DockerEvent, StatPoint } from './types';

export { useMount } from './store';

/** Re-render with live stats of one container. `enabled` should be false when the container is not running. */
export function useStats(id: string | undefined, enabled = true, everyMs = 2000): { last: StatPoint | undefined; history: StatPoint[] } {
  const [, bump] = useState(0);
  const lastRender = useRef(0);
  useEffect(() => {
    if (!id || !enabled) return;
    return subscribeStats(id, () => {
      const now = Date.now();
      if (now - lastRender.current >= everyMs) {
        lastRender.current = now;
        bump((n) => n + 1);
      }
    });
  }, [id, enabled, everyMs]);
  if (!id || !enabled) return { last: undefined, history: [] };
  return { last: statsLast(id), history: statsHistory(id) };
}

/** Calls `fn` for each Engine event while mounted. `fn` may change between renders. */
export function useDockerEvents(fn: (ev: DockerEvent) => void): void {
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => subscribeEvents((ev) => ref.current(ev)), []);
}

/** Loads something once per `deps` change and tracks loading/error. For one-off reads (inspect, history). */
export function useAsync<T>(load: () => Promise<T>, deps: unknown[]): { data: T | undefined; error: Error | null; loading: boolean; reload(): void } {
  const [state, setState] = useState<{ data: T | undefined; error: Error | null; loading: boolean }>({ data: undefined, error: null, loading: true });
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    setState((s) => ({ ...s, loading: true }));
    load().then(
      (data) => live && setState({ data, error: null, loading: false }),
      (error) => live && setState((s) => ({ data: s.data, error, loading: false })),
    );
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);
  return { ...state, reload: () => setTick((n) => n + 1) };
}
