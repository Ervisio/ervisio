import { useEffect, useSyncExternalStore } from 'react';
import { ApiError, call } from '../../api';
import type { App, Pkg, Summary, Update } from './types';

/** Shared state: the page, the rail badge and the palette all read it. */
interface State {
  summary: Summary | null;
  updates: Update[] | null;
  installed: Pkg[] | null;
  apps: App[] | null;
  checking: boolean;
  error: ApiError | null;
}

let state: State = { summary: null, updates: null, installed: null, apps: null, checking: false, error: null };
const subs = new Set<() => void>();
const set = (p: Partial<State>) => {
  state = { ...state, ...p };
  subs.forEach((s) => s());
};
const subscribe = (cb: () => void) => (subs.add(cb), () => void subs.delete(cb));
export const useSoftware = () => useSyncExternalStore(subscribe, () => state);
export const getSoftware = () => state;

const fail = (e: unknown) => {
  if (e instanceof ApiError) set({ error: e });
};

export async function loadSummary(force = false): Promise<void> {
  try {
    const s = await call<Summary>('software.summary');
    set({ summary: s, error: null });
    if (force) await loadUpdates(true);
  } catch (e) {
    fail(e);
  }
}

export async function loadUpdates(force = false): Promise<void> {
  try {
    const [u, s] = await Promise.all([call<Update[]>('software.updates', { force }), call<Summary>('software.summary')]);
    set({ updates: u, summary: s, error: null });
  } catch (e) {
    fail(e);
  }
}

export async function loadInstalled(force = false): Promise<void> {
  try {
    set({ installed: await call<Pkg[]>('software.installed', { filter: 'all', force }) });
  } catch (e) {
    fail(e);
  }
}

export async function loadApps(): Promise<void> {
  try {
    set({ apps: await call<App[]>('software.apps') });
  } catch (e) {
    fail(e);
  }
}

/** Check now: refresh the package databases, then recompute the updates. Returns the warnings. */
export async function checkNow(): Promise<string[]> {
  if (state.checking) return [];
  set({ checking: true });
  try {
    const s = await call<Summary>('software.check');
    const u = await call<Update[]>('software.updates');
    set({ summary: s, updates: u, error: null, checking: false });
    return s.warnings;
  } catch (e) {
    set({ checking: false });
    fail(e);
    throw e;
  }
}

/** After a transaction everything may have changed. */
export async function reloadAll(): Promise<void> {
  set({ installed: null, apps: null });
  await Promise.all([loadUpdates(true), loadInstalled(true), loadApps()]);
}

let started = false;
/** Called by the always-on rail badge hook: one summary now and every 5 minutes (the server caches for 10). */
export function useBackgroundSummary() {
  useEffect(() => {
    if (!started) {
      started = true;
      void loadSummary();
    }
    const id = window.setInterval(() => !document.hidden && void loadSummary(), 5 * 60_000);
    return () => window.clearInterval(id);
  }, []);
}
