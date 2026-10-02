import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import * as client from './client';
import { signInWithKey as keySignIn, type SignInWithKeyArgs } from '../auth/sshkey';
import { authEvents } from './http';
import { ApiError, type PublicHost, type Session } from './types';

export type SessionStatus = 'loading' | 'authed' | 'anon';

export interface SessionValue {
  status: SessionStatus;
  session: Session | null;
  /** Distro, hostname, ip: available before and after sign-in. */
  host: PublicHost | null;
  /** Seconds of admin rights left (0 when locked). */
  unlockLeft: number;
  isUnlocked: boolean;
  /** Unlocked until sign-out: show no countdown. */
  unlockForever: boolean;
  /** Root users (or sessions with no sudo) can always run admin calls. */
  signIn(user: string, password: string, stay?: boolean): Promise<{ user: string; isAdmin: boolean; isRoot: boolean }>;
  /** Sign in with an SSH key (see auth/sshkey). Throws SshKeyError. */
  signInWithKey(args: SignInWithKeyArgs): Promise<{ user: string; isAdmin: boolean; isRoot: boolean }>;
  signOut(): Promise<void>;
  /** password "" tries sudo NOPASSWD (useful after an SSH-key sign-in); see client.unlock. */
  unlock(password: string): Promise<void>;
  lock(): Promise<void>;
  refresh(): Promise<void>;
}

const Ctx = createContext<SessionValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<SessionStatus>('loading');
  const [session, setSession] = useState<Session | null>(null);
  const [host, setHost] = useState<PublicHost | null>(() => {
    try {
      return JSON.parse(localStorage.getItem('la.host') || 'null');
    } catch {
      return null;
    }
  });
  const [now, setNow] = useState(() => Date.now());

  const refresh = useCallback(async () => {
    try {
      const s = await client.session();
      setSession(s);
      setStatus('authed');
    } catch (e) {
      setSession(null);
      setStatus('anon');
      if (!(e instanceof ApiError) || e.code === 'network') console.warn('session check failed', e);
    }
  }, []);

  useEffect(() => {
    void refresh();
    client
      .publicHost()
      .then((h) => {
        setHost(h);
        try {
          localStorage.setItem('la.host', JSON.stringify(h));
        } catch {
          /* ignore */
        }
      })
      .catch(() => undefined);
  }, [refresh]);

  useEffect(() => {
    const on = () => {
      setSession(null);
      setStatus('anon');
    };
    authEvents.addEventListener('unauthenticated', on);
    return () => authEvents.removeEventListener('unauthenticated', on);
  }, []);

  const until = session?.unlockedUntil ?? 0;
  useEffect(() => {
    if (until <= Date.now()) return;
    setNow(Date.now());
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [until]);

  const signIn = useCallback(async (user: string, password: string, stay = true) => {
    const res = await client.login(user, password, stay);
    const s = await client.session();
    setSession(s);
    setStatus('authed');
    return res;
  }, []);

  const signInWithKey = useCallback(async (args: SignInWithKeyArgs) => {
    const res = await keySignIn(args);
    const s = await client.session();
    setSession(s);
    setStatus('authed');
    return res;
  }, []);

  const signOut = useCallback(async () => {
    await client.logout().catch(() => undefined);
    setSession(null);
    setStatus('anon');
  }, []);

  const unlock = useCallback(async (password: string) => {
    const u = await client.unlock(password);
    setSession((s) => (s ? { ...s, unlockedUntil: u.until ?? Date.now() + 5 * 60_000, unlockedForever: u.forever } : s));
  }, []);

  const lock = useCallback(async () => {
    await client.lock().catch(() => undefined);
    setSession((s) => (s ? { ...s, unlockedUntil: undefined, unlockedForever: false } : s));
  }, []);

  const unlockLeft = until > now ? Math.ceil((until - now) / 1000) : 0;
  const unlockForever = unlockLeft > 0 && !!session?.unlockedForever;
  const value = useMemo<SessionValue>(
    () => ({ status, session, host, unlockLeft, isUnlocked: unlockLeft > 0, unlockForever, signIn, signInWithKey, signOut, unlock, lock, refresh }),
    [status, session, host, unlockLeft, unlockForever, signIn, signInWithKey, signOut, unlock, lock, refresh],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useSession(): SessionValue {
  const v = useContext(Ctx);
  if (!v) throw new Error('useSession must be used inside <SessionProvider>');
  return v;
}
