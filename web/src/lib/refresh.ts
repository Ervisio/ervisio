import { useCallback, useEffect, useRef } from 'react';
import { usePrefs } from '../api';
import { useWindowActivity } from './windowActivity';

/** Per-user "how often live data refreshes" (pref `refreshInterval`, milliseconds). */
export const REFRESH_KEY = 'refreshInterval';
export const REFRESH_MIN = 1000;
export const REFRESH_MAX = 600_000;
export const REFRESH_DEFAULT = 2000;
/** Preset choices in ms; anything else in the range is a custom value. */
export const REFRESH_OPTIONS = [
  1000, 2000, 5000, 10_000, 30_000, 60_000, 300_000, 600_000,
] as const;

export function clampRefresh(v: unknown): number {
  const n = typeof v === 'number' ? v : Number(v);
  if (!Number.isFinite(n)) return REFRESH_DEFAULT;
  return Math.min(REFRESH_MAX, Math.max(REFRESH_MIN, Math.round(n)));
}

/** The user's refresh interval in ms (1000..600000, default 2000). */
export function useRefreshInterval(): number {
  const { prefs } = usePrefs();
  return clampRefresh(prefs[REFRESH_KEY] ?? REFRESH_DEFAULT);
}

/** Setter for the preference (clamped). */
export function useSetRefreshInterval(): (ms: number) => Promise<void> {
  const { set } = usePrefs();
  return useCallback((ms: number) => set(REFRESH_KEY, clampRefresh(ms)), [set]);
}

/** Short label such as "2 s", "30 s", "1 min", "90 s". */
export function formatInterval(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s >= 60 && s % 60 === 0) return `${s / 60} min`;
  return `${s} s`;
}

const NOW_EVENT = 'ervisio:refresh-now';
/** Ask every live view (metrics stream, alerts, pollers using usePolling) to refresh immediately. */
export function refreshNow() {
  window.dispatchEvent(new Event(NOW_EVENT));
}
export function onRefreshNow(fn: () => void): () => void {
  window.addEventListener(NOW_EVENT, fn);
  return () => window.removeEventListener(NOW_EVENT, fn);
}

/**
 * Runs `fn` every `ms` while this window and tab are visible, plus once on restore and
 * whenever "Refresh now" is used. Does not run `fn` at mount; the caller loads initial data itself.
 */
export function usePolling(fn: () => void, ms: number, enabled = true) {
  const active = useWindowActivity();
  const ref = useRef(fn);
  const paused = useRef(false);
  ref.current = fn;
  useEffect(() => {
    if (!enabled || !active) {
      paused.current = true;
      return;
    }
    if (paused.current) ref.current();
    paused.current = false;
    const tick = () => ref.current();
    const id = window.setInterval(tick, ms);
    const off = onRefreshNow(tick);
    return () => {
      window.clearInterval(id);
      off();
    };
  }, [ms, enabled, active]);
}
