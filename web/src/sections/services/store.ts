import { useEffect, useSyncExternalStore } from 'react';
import { call } from '../../api';
import type { FailedUnit, Summary, Unit } from './types';

/** Tiny shared store so the always-on hooks (rail badge, palette) and the page agree. */
interface Shared {
  failed: number;
  running: string[];
  /** Names of failed units; null until the first summary arrived. */
  failedUnits: FailedUnit[] | null;
}
let state: Shared = { failed: 0, running: [], failedUnits: null };
const subs = new Set<() => void>();

function set(patch: Partial<Shared>) {
  const next = { ...state, ...patch };
  const names = (l: FailedUnit[] | null) => (l ? l.map((u) => u.name).join('|') : '-');
  if (next.failed === state.failed && next.running.join('|') === state.running.join('|') && names(next.failedUnits) === names(state.failedUnits)) return;
  state = next;
  subs.forEach((s) => s());
}

export const publishFailed = (n: number) => set({ failed: n });
/** Publish the failed units of a fresh services.summary (drives the badge and "unit failed" notifications). */
export const publishSummary = (s: Summary) => set({ failed: s.failed.length, failedUnits: s.failed });
export const publishRunning = (units: Unit[]) =>
  set({ running: units.filter((u) => u.state === 'running' && u.name.endsWith('.service')).map((u) => u.name) });

function subscribe(cb: () => void) {
  subs.add(cb);
  return () => void subs.delete(cb);
}
export const useShared = () => useSyncExternalStore(subscribe, () => state);

/** Polls (slowly) for the always-on hooks; the page itself publishes fresher data. */
export function useBackgroundPoll(withUnits: boolean) {
  useEffect(() => {
    let live = true;
    const tick = async () => {
      try {
        if (withUnits) {
          const r = await call<{ units: Unit[] }>('services.list', { type: 'service' });
          if (live) publishRunning(r.units);
        } else {
          const s = await call<Summary>('services.summary');
          if (live) publishSummary(s);
        }
      } catch {
        /* the page shows errors; background polling stays quiet */
      }
    };
    void tick();
    const id = window.setInterval(() => !document.hidden && void tick(), 60_000);
    return () => {
      live = false;
      window.clearInterval(id);
    };
  }, [withUnits]);
}
