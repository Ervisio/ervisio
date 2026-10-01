import { useEffect, useSyncExternalStore } from 'react';
import { call } from '../../api';
import type { Summary, Unit } from './types';

/** Tiny shared store so the always-on hooks (rail badge, palette) and the page agree. */
interface Shared {
  failed: number;
  running: string[];
}
let state: Shared = { failed: 0, running: [] };
const subs = new Set<() => void>();

function set(patch: Partial<Shared>) {
  const next = { ...state, ...patch };
  if (next.failed === state.failed && next.running.join('|') === state.running.join('|')) return;
  state = next;
  subs.forEach((s) => s());
}

export const publishFailed = (n: number) => set({ failed: n });
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
          if (live) publishFailed(s.failed.length);
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
