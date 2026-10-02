/**
 * The plugin's own router: state in memory, no URL. There is one page (`docker`); this decides what it shows.
 *
 *   navigate({ view: 'container', id })     // go forward; Back returns here
 *   navigate({ view: 'images' }, { root: true })   // sidebar jump: starts a fresh history
 *   back()                                   // previous view (or the home when there is none)
 *   const route = useRoute();                // current Route
 *
 * To add a route: add it to `Route`, give it a nav section in `sectionOf`, register its component in
 * views/registry.tsx.
 */
import { useSyncExternalStore } from 'react';

export type Route =
  | { view: 'containers' }
  | { view: 'container'; id: string; tab?: 'overview' | 'logs' | 'stats' | 'shell' | 'inspect' | 'settings' }
  | { view: 'stacks' }
  | { view: 'stack'; name: string }
  | { view: 'create'; /** container id to edit (recreate with the same settings) */ from?: string; /** image to start from */ image?: string }
  | { view: 'templates' }
  | { view: 'template'; id: string }
  | { view: 'images' }
  | { view: 'volumes' }
  | { view: 'networks' }
  | { view: 'registries' }
  | { view: 'cleanup' }
  | { view: 'autoupdate' }
  | { view: 'alerts' };

export type View = Route['view'];

/** Sidebar entries: each is a top-level route. */
export type NavId = 'containers' | 'stacks' | 'templates' | 'images' | 'volumes' | 'networks' | 'registries' | 'cleanup' | 'autoupdate' | 'alerts';

/** Which sidebar entry is lit for a route. */
export function sectionOf(r: Route): NavId {
  switch (r.view) {
    case 'container':
    case 'create':
      return 'containers';
    case 'stack':
      return 'stacks';
    case 'template':
      return 'templates';
    default:
      return r.view;
  }
}

const HOME: Route = { view: 'containers' };
let history: Route[] = [HOME];
const subs = new Set<() => void>();
const emit = () => subs.forEach((f) => f());
const same = (a: Route, b: Route) => JSON.stringify(a) === JSON.stringify(b);

export function navigate(route: Route, opts: { root?: boolean; replace?: boolean } = {}): void {
  const cur = history[history.length - 1];
  if (opts.root) history = [route];
  else if (opts.replace) history = [...history.slice(0, -1), route];
  else if (!same(cur, route)) history = [...history, route];
  else return;
  emit();
}

export function back(): void {
  history = history.length > 1 ? history.slice(0, -1) : [HOME];
  emit();
}

export const currentRoute = (): Route => history[history.length - 1];
export const canGoBack = (): boolean => history.length > 1;

const subscribe = (f: () => void) => {
  subs.add(f);
  return () => subs.delete(f);
};

export function useRoute(): Route {
  return useSyncExternalStore(subscribe, currentRoute);
}

export function useCanGoBack(): boolean {
  return useSyncExternalStore(subscribe, canGoBack);
}

/* ---------- global search (the shell's top box) ---------- */

let query = '';
const qsubs = new Set<() => void>();

export function setSearch(q: string): void {
  query = q;
  qsubs.forEach((f) => f());
}

/** The text in the shell's search box. Views that list things (containers, images, stacks) filter by it. */
export function useSearch(): string {
  return useSyncExternalStore(
    (f) => {
      qsubs.add(f);
      return () => qsubs.delete(f);
    },
    () => query,
  );
}

/** Props a view component receives: its route without the `view` field. `RouteProps<'container'>` = { id, tab? }. */
export type RouteProps<V extends View> = Omit<Extract<Route, { view: V }>, 'view'>;
