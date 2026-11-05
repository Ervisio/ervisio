import { useEffect, useSyncExternalStore } from 'react';
import { ApiError, call, errorMessage } from '../../api';
import { useAccountScope } from '../../lib/accountScope';
import { ScopedReadCache } from '../../lib/scopedReadCache';
import { useWindowActivity } from '../../lib/windowActivity';
import type { App, Pkg, SearchResult, Summary, Update } from './types';

/** Shared, account/host-scoped state for the page, rail badge and palette. */
interface State {
  summary: Summary | null;
  updates: Update[] | null;
  installed: Pkg[] | null;
  apps: App[] | null;
  checking: boolean;
  error: ApiError | null;
}
type Resource = 'summary' | 'updates' | 'installed' | 'apps';
interface ScopeState {
  state: State;
  errors: Partial<Record<Resource | 'check', ApiError>>;
  suggestionsRevision: number;
  visited: Set<Resource>;
}
const empty = (): State => ({
  summary: null,
  updates: null,
  installed: null,
  apps: null,
  checking: false,
  error: null,
});
const anonymous = empty();
const scopes = new Map<string, ScopeState>();
const summaryCache = new ScopedReadCache<Summary>();
const updatesCache = new ScopedReadCache<Update[]>();
const installedCache = new ScopedReadCache<Pkg[]>();
const appsCache = new ScopedReadCache<App[]>();
const suggestionsCache = new ScopedReadCache<SearchResult[]>();
const SUMMARY_TTL = 60_000;
const CATALOG_TTL = 5 * 60_000;
let activeScope: string | null = null;
const subs = new Set<() => void>();
const subscribe = (cb: () => void) => (subs.add(cb), () => void subs.delete(cb));

function slot(scope: string): ScopeState {
  let value = scopes.get(scope);
  if (!value) {
    value = { state: empty(), errors: {}, suggestionsRevision: 0, visited: new Set() };
    scopes.set(scope, value);
    if (scopes.size > 8) scopes.delete(scopes.keys().next().value!);
  }
  return value;
}
function set(scope: string, patch: Partial<State>) {
  const value = slot(scope);
  value.state = { ...value.state, ...patch };
  subs.forEach((notify) => notify());
}
function result(scope: string, resource: Resource | 'check', error?: unknown) {
  const value = slot(scope);
  if (error !== undefined)
    value.errors[resource] =
      error instanceof ApiError ? error : new ApiError('network', errorMessage(error));
  else delete value.errors[resource];
  set(scope, { error: Object.values(value.errors)[0] ?? null });
}
export function useSoftware() {
  const scope = useAccountScope();
  activeScope = scope;
  return useSyncExternalStore(subscribe, () => (scope ? slot(scope).state : anonymous));
}
export const getSoftware = () => (activeScope ? slot(activeScope).state : anonymous);

export async function loadSummary(force = false, scope = activeScope): Promise<void> {
  if (!scope) return;
  try {
    await summaryCache.load(scope, () => call<Summary>('software.summary'), SUMMARY_TTL, force);
    set(scope, { summary: summaryCache.peek(scope) ?? null });
    result(scope, 'summary');
  } catch (error) {
    result(scope, 'summary', error);
  }
}

export async function loadUpdates(force = false, scope = activeScope): Promise<void> {
  if (!scope) return;
  await Promise.all([
    loadSummary(force, scope),
    (async () => {
      try {
        await updatesCache.load(
          scope,
          () => call<Update[]>('software.updates', { force }),
          SUMMARY_TTL,
          force,
        );
        set(scope, { updates: updatesCache.peek(scope) ?? null });
        result(scope, 'updates');
      } catch (error) {
        result(scope, 'updates', error);
      }
    })(),
  ]);
}

export async function loadInstalled(force = false, scope = activeScope): Promise<void> {
  if (!scope) return;
  slot(scope).visited.add('installed');
  try {
    await installedCache.load(
      scope,
      () => call<Pkg[]>('software.installed', { filter: 'all', force }),
      CATALOG_TTL,
      force,
    );
    set(scope, { installed: installedCache.peek(scope) ?? null });
    result(scope, 'installed');
  } catch (error) {
    result(scope, 'installed', error);
  }
}

export async function loadApps(force = false, scope = activeScope): Promise<void> {
  if (!scope) return;
  slot(scope).visited.add('apps');
  try {
    await appsCache.load(scope, () => call<App[]>('software.apps'), CATALOG_TTL, force);
    set(scope, { apps: appsCache.peek(scope) ?? null });
    result(scope, 'apps');
  } catch (error) {
    result(scope, 'apps', error);
  }
}

const suggestionKey = (scope: string, category: string) =>
  JSON.stringify([scope, category, slot(scope).suggestionsRevision]);
export const suggestionsRevision = (scope: string | null) =>
  scope ? slot(scope).suggestionsRevision : 0;
export const peekSuggestions = (category: string, scope = activeScope) =>
  scope ? suggestionsCache.peek(suggestionKey(scope, category)) : undefined;
export function loadSuggestions(category: string, scope = activeScope): Promise<SearchResult[]> {
  return scope
    ? suggestionsCache.load(
        suggestionKey(scope, category),
        () => call<SearchResult[]>('software.suggest', { category }),
        CATALOG_TTL,
      )
    : Promise.resolve([]);
}

/** A real package database refresh is always sent to the server; never cached. */
export async function checkNow(): Promise<string[]> {
  const scope = activeScope;
  if (!scope || slot(scope).state.checking) return [];
  set(scope, { checking: true });
  summaryCache.invalidate(scope);
  updatesCache.invalidate(scope);
  try {
    const summary = await call<Summary>('software.check');
    summaryCache.put(scope, summary);
    set(scope, { summary });
    // Do not start a follow-up request under a different account's new cookie.
    if (scope === activeScope) {
      const updates = await call<Update[]>('software.updates');
      updatesCache.put(scope, updates);
      set(scope, { updates });
      result(scope, 'updates');
    }
    result(scope, 'summary');
    result(scope, 'check');
    return summary.warnings;
  } catch (error) {
    result(scope, 'check', error);
    throw error;
  } finally {
    set(scope, { checking: false });
  }
}

/** Invalidate real reads after a transaction, retaining the last value during refresh.
 * Catalogs the user never opened remain lazy. Mutations themselves never enter a cache. */
export async function reloadAll(): Promise<void> {
  const scope = activeScope;
  if (!scope) return;
  const { visited } = slot(scope);
  summaryCache.invalidate(scope);
  updatesCache.invalidate(scope);
  installedCache.invalidate(scope);
  appsCache.invalidate(scope);
  slot(scope).suggestionsRevision++;
  await Promise.all([
    loadUpdates(true, scope),
    ...(visited.has('installed') ? [loadInstalled(true, scope)] : []),
    ...(visited.has('apps') ? [loadApps(true, scope)] : []),
  ]);
}

/** The badge performs inexpensive summary reads; reopening the page shares this cache. */
export function useBackgroundSummary() {
  const scope = useAccountScope();
  const active = useWindowActivity();
  activeScope = scope;
  useEffect(() => {
    if (!scope || !active) return;
    void loadSummary(false, scope);
    const id = window.setInterval(() => void loadSummary(false, scope), 5 * 60_000);
    return () => window.clearInterval(id);
  }, [scope, active]);
}
