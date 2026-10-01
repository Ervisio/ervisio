import { useEffect, useMemo, useRef, useSyncExternalStore } from 'react';
import type { IconName } from '../ui';

/* ---------------- Palette actions ---------------- */
export interface PaletteAction {
  /** Unique, e.g. "services.restart-all". */
  id: string;
  /** Already translated title shown in the palette. */
  title: string;
  /** Small grey text, e.g. the section name. */
  hint?: string;
  icon?: IconName;
  /** Extra words that should match, already translated. */
  keywords?: string[];
  /** Section id used to tint the row (ov, term, ...). */
  hue?: string;
  run(): void;
}

let actions = new Map<string, PaletteAction[]>();
const aSubs = new Set<() => void>();

/** Register actions for an owner key. Returns the unregister function. Prefer the usePaletteActions hook. */
export function registerPaletteActions(owner: string, list: PaletteAction[]): () => void {
  actions = new Map(actions).set(owner, list);
  aSubs.forEach((s) => s());
  return () => {
    actions = new Map(actions);
    actions.delete(owner);
    aSubs.forEach((s) => s());
  };
}

/**
 * Register palette actions while the calling component is mounted.
 * Pass a memoised or stable-enough list; the latest `run` callbacks are always used.
 */
export function usePaletteActions(owner: string, list: PaletteAction[]) {
  const ref = useRef(list);
  ref.current = list;
  const sig = list.map((a) => a.id + a.title).join('|');
  useEffect(() => {
    const proxy: PaletteAction[] = ref.current.map((a, i) => ({ ...a, run: () => ref.current[i]?.run() }));
    return registerPaletteActions(owner, proxy);
  }, [owner, sig]);
}

export function useAllPaletteActions(): PaletteAction[] {
  const map = useSyncExternalStore(
    (cb) => {
      aSubs.add(cb);
      return () => void aSubs.delete(cb);
    },
    () => actions,
  );
  const fromHooks = useSectionPaletteHooks();
  return useMemo(() => [...map.values()].flat().concat(fromHooks), [map, fromHooks]);
}

/* ---------------- Rail badges ---------------- */
export interface RailBadge {
  count: number;
  /** err (red, default) or info (accent). */
  tone?: 'err' | 'info';
}
let badges: Record<string, RailBadge | undefined> = {};
const bSubs = new Set<() => void>();

/** Set (or clear with null) the badge on a section's rail item. Prefer the useRailBadge hook. */
export function setRailBadge(sectionId: string, badge: RailBadge | null) {
  badges = { ...badges, [sectionId]: badge ?? undefined };
  bSubs.forEach((s) => s());
}

/** Show a count on the rail item while the calling component is mounted. 0 / undefined hides it. */
export function useRailBadge(sectionId: string, count: number | undefined, tone: RailBadge['tone'] = 'err') {
  useEffect(() => {
    setRailBadge(sectionId, count ? { count, tone } : null);
  }, [sectionId, count, tone]);
}

export function useRailBadges(): Record<string, RailBadge | undefined> {
  return useSyncExternalStore(
    (cb) => {
      bSubs.add(cb);
      return () => void bSubs.delete(cb);
    },
    () => badges,
  );
}

/* ---------------- Optional always-on hooks provided by section folders ----------------
 * A section may add `src/sections/<id>/badge.ts` with a default-exported hook returning a count
 * (number | RailBadge | undefined) and/or `src/sections/<id>/palette.ts` with a default-exported hook
 * returning PaletteAction[]. The shell calls them even when the section page is not open, so keep
 * them cheap (no heavy polling). The list is static at build time, so hook order is stable.
 */
type BadgeHook = () => number | RailBadge | undefined;
type PaletteHook = () => PaletteAction[];
const badgeHooks = Object.entries(import.meta.glob<{ default: BadgeHook }>('./*/badge.ts', { eager: true })).map(
  ([p, m]) => [p.split('/')[1], m.default] as const,
);
const paletteHooks = Object.values(import.meta.glob<{ default: PaletteHook }>('./*/palette.ts', { eager: true })).map((m) => m.default);

/** Called once by the shell. */
export function useSectionBadgeHooks(): Record<string, RailBadge | undefined> {
  const out: Record<string, RailBadge | undefined> = {};
  for (const [id, hook] of badgeHooks) {
    const v = hook();
    out[id] = typeof v === 'number' ? (v > 0 ? { count: v } : undefined) : v && v.count > 0 ? v : undefined;
  }
  return out;
}
function useSectionPaletteHooks(): PaletteAction[] {
  const out: PaletteAction[] = [];
  for (const h of paletteHooks) out.push(...h());
  return out;
}
