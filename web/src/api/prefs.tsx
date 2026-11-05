import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { call } from './client';
import { API_BASE } from './base';
import { useSession } from './session';

export type Prefs = Record<string, any>;

interface PrefsValue {
  prefs: Prefs;
  ready: boolean;
  /** A failed initial read must not be treated as an empty desktop. */
  error: Error | null;
  retry(): void;
  /** Instant optimistic save; persists through prefs.set. Pass undefined to remove the key. */
  set(key: string, value: unknown): Promise<void>;
  /** Clears this account's preferences after every already-queued write. */
  reset(): Promise<void>;
}
const Ctx = createContext<PrefsValue | null>(null);
const CACHE = 'ervisio.prefs';
const EMPTY_PREFS: Prefs = {};
interface Snapshot {
  owner: string;
  values: Prefs;
}

function readCache(key: string): Prefs {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(key) || '{}');
    return value && typeof value === 'object' && !Array.isArray(value) ? (value as Prefs) : {};
  } catch {
    return {};
  }
}

function writeCache(key: string, values: Prefs) {
  try {
    localStorage.setItem(key, JSON.stringify(values));
  } catch {
    /* The server remains authoritative when browser storage is unavailable. */
  }
}

export function PrefsProvider({ children }: { children: ReactNode }) {
  const { status, session } = useSession();
  // Never hydrate one account's desktop or settings from another account's cache.
  // The API origin is part of the identity for wrappers using VITE_API_BASE.
  const owner =
    status === 'authed' && session
      ? JSON.stringify([API_BASE, session.uid, session.user])
      : 'anonymous';
  const cacheKey = `${CACHE}:${owner}`;
  const current = useRef({ status, owner });
  current.current = { status, owner };
  const [snapshot, setSnapshot] = useState<Snapshot>({ owner: '', values: {} });
  const [loadedOwner, setLoadedOwner] = useState('');
  const [loadError, setLoadError] = useState<Error | null>(null);
  const [attempt, setAttempt] = useState(0);
  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  const ref = useRef(snapshot);
  ref.current = snapshot;
  const writes = useRef<Promise<unknown>>(Promise.resolve());
  const resetting = useRef(false);
  const prefs = snapshot.owner === owner ? snapshot.values : EMPTY_PREFS;
  const ready = status === 'authed' && loadedOwner === owner;

  useEffect(() => {
    let live = true;
    // The old unscoped cache cannot be attributed to a user. Do not import it.
    try {
      localStorage.removeItem(CACHE);
    } catch {
      /* unavailable */
    }
    const cached = { owner, values: readCache(cacheKey) };
    ref.current = cached;
    setSnapshot(cached);
    setLoadedOwner('');
    setLoadError(null);
    if (status !== 'authed') return;
    call<Prefs>('prefs.get', {})
      .then((p) => {
        if (!live || current.current.owner !== owner) return;
        if (p && typeof p === 'object' && !Array.isArray(p)) {
          const next = { owner, values: p };
          ref.current = next;
          setSnapshot(next);
          writeCache(cacheKey, p);
          setLoadedOwner(owner);
        } else {
          throw new Error('The server returned invalid preferences.');
        }
      })
      .catch((error: unknown) => {
        if (live && current.current.owner === owner) {
          setLoadError(error instanceof Error ? error : new Error(String(error)));
        }
      });
    return () => {
      live = false;
    };
  }, [status, owner, cacheKey, attempt]);

  const set = useCallback(async (key: string, value: unknown) => {
    if (resetting.current) return;
    const identity = current.current;
    const identityKey = `${CACHE}:${identity.owner}`;
    const before = ref.current.owner === identity.owner ? ref.current.values : {};
    const next = { ...before };
    if (value === undefined) delete next[key];
    else next[key] = value;
    const updated = { owner: identity.owner, values: next };
    ref.current = updated;
    setSnapshot(updated);
    writeCache(identityKey, next);
    if (identity.status !== 'authed') return;
    // Preserve the order of quick desktop edits even when HTTP requests finish
    // at different times, and never send a queued write as a different user.
    const pending = writes.current
      .catch(() => undefined)
      .then(async () => {
        if (current.current.owner !== identity.owner || current.current.status !== 'authed') return;
        await call('prefs.set', { key, value: value ?? null });
      });
    writes.current = pending;
    try {
      await pending;
    } catch (error) {
      if (current.current.owner === identity.owner && ref.current.values[key] === next[key]) {
        const restored = { ...ref.current.values };
        if (key in before) restored[key] = before[key];
        else delete restored[key];
        const rollback = { owner: identity.owner, values: restored };
        ref.current = rollback;
        setSnapshot(rollback);
        writeCache(identityKey, restored);
      }
      throw error;
    }
  }, []);

  const reset = useCallback(async () => {
    if (resetting.current) return;
    const identity = current.current;
    if (identity.status !== 'authed') throw new Error('Sign in before resetting preferences.');
    resetting.current = true;
    const pending = writes.current
      .catch(() => undefined)
      .then(async () => {
        if (current.current.owner !== identity.owner || current.current.status !== 'authed')
          throw new Error('The signed-in account changed. Open Settings again.');
        await call('prefs.setAll', { values: {}, replace: true });
        if (current.current.owner === identity.owner) {
          const cleared = { owner: identity.owner, values: {} };
          ref.current = cleared;
          setSnapshot(cleared);
          writeCache(`${CACHE}:${identity.owner}`, {});
        }
      });
    writes.current = pending;
    try {
      await pending;
    } finally {
      resetting.current = false;
    }
  }, []);

  const v = useMemo(
    () => ({ prefs, ready, set, reset, error: loadError, retry }),
    [prefs, ready, set, reset, loadError, retry],
  );
  return <Ctx.Provider value={v}>{children}</Ctx.Provider>;
}

export function usePrefs(): PrefsValue {
  const v = useContext(Ctx);
  if (!v) throw new Error('usePrefs must be used inside <PrefsProvider>');
  return v;
}
