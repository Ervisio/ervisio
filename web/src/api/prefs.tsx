import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { call } from './client';
import { useSession } from './session';

export type Prefs = Record<string, any>;

interface PrefsValue {
  prefs: Prefs;
  ready: boolean;
  /** Instant optimistic save; persists through prefs.set. Pass undefined to remove the key. */
  set(key: string, value: unknown): Promise<void>;
}
const Ctx = createContext<PrefsValue | null>(null);
const CACHE = 'ervisio.prefs';

export function PrefsProvider({ children }: { children: ReactNode }) {
  const { status } = useSession();
  const statusRef = useRef(status);
  statusRef.current = status;
  const [prefs, setPrefs] = useState<Prefs>(() => {
    try {
      return JSON.parse(localStorage.getItem(CACHE) || '{}');
    } catch {
      return {};
    }
  });
  const [ready, setReady] = useState(false);
  const ref = useRef(prefs);
  ref.current = prefs;

  useEffect(() => {
    if (status !== 'authed') {
      setReady(false);
      return;
    }
    let live = true;
    call<Prefs>('prefs.get', {})
      .then((p) => {
        if (!live) return;
        if (p && typeof p === 'object') {
          setPrefs(p);
          try {
            localStorage.setItem(CACHE, JSON.stringify(p));
          } catch {
            /* ignore */
          }
        }
      })
      .catch(() => undefined)
      .finally(() => live && setReady(true));
    return () => {
      live = false;
    };
  }, [status]);

  const set = useCallback(async (key: string, value: unknown) => {
    const next = { ...ref.current };
    if (value === undefined) delete next[key];
    else next[key] = value;
    ref.current = next;
    setPrefs(next);
    try {
      localStorage.setItem(CACHE, JSON.stringify(next));
    } catch {
      /* ignore */
    }
    if (statusRef.current === 'authed') await call('prefs.set', { key, value: value ?? null });
  }, []);

  const v = useMemo(() => ({ prefs, ready, set }), [prefs, ready, set]);
  return <Ctx.Provider value={v}>{children}</Ctx.Provider>;
}

export function usePrefs(): PrefsValue {
  const v = useContext(Ctx);
  if (!v) throw new Error('usePrefs must be used inside <PrefsProvider>');
  return v;
}
