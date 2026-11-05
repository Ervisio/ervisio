import { useEffect, useRef, useState, type FormEvent, type RefObject } from 'react';
import { rememberRecentUser, useSession } from '../api';
import type { LoadedKey } from './KeyStep';
import { classify, classifyKey, type Problem } from './loginProblem';

type SignInContext = {
  user: string;
  stay: boolean;
  setBusy: (busy: boolean) => void;
  setProblem: (problem: Problem) => void;
};

export function usePasswordSignIn({
  user,
  stay,
  setBusy,
  setProblem,
  pwRef,
}: SignInContext & { pwRef: RefObject<HTMLInputElement> }) {
  const { signIn } = useSession();
  const [pw, setPw] = useState('');

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const u = user.trim();
    if (!u || !pw) {
      setProblem({ kind: 'wrong' });
      return;
    }
    setBusy(true);
    setProblem(null);
    try {
      const r = await signIn(u, pw, stay);
      rememberRecentUser({ user: u, isAdmin: r.isAdmin });
    } catch (x) {
      setProblem(classify(x));
      setPw('');
      pwRef.current?.focus();
    } finally {
      setBusy(false);
    }
  };

  return { pw, setPw, submit };
}

export function useKeySignIn({
  user,
  stay,
  busy,
  setBusy,
  setProblem,
  userRef,
}: SignInContext & { busy: boolean; userRef: RefObject<HTMLInputElement> }) {
  const { signInWithKey } = useSession();
  const [loadedKey, setLoadedKey] = useState<LoadedKey | null>(null);
  const [passphrase, setPassphrase] = useState('');

  useEffect(
    () => () => {
      // Nothing of the key is kept: the state goes with the component, and this drops the references early.
      setLoadedKey(null);
      setPassphrase('');
    },
    [],
  );

  const keyReady = !!loadedKey && (!loadedKey.needsPass || passphrase.length > 0);

  const submitKey = async () => {
    const u = user.trim();
    if (!loadedKey || busy) return;
    if (!u) {
      setProblem({ kind: 'key', code: 'key_refused' });
      userRef.current?.focus();
      return;
    }
    if (!keyReady) return;
    setBusy(true);
    setProblem(null);
    try {
      const r = await signInWithKey({
        user: u,
        keyText: loadedKey.text,
        passphrase: loadedKey.needsPass ? passphrase : undefined,
        stay,
      });
      rememberRecentUser({ user: u, isAdmin: r.isAdmin, method: 'key' });
      setLoadedKey(null);
      setPassphrase('');
    } catch (x) {
      const p = classifyKey(x);
      setProblem(p);
      if (p.code === 'bad_passphrase') setPassphrase('');
    } finally {
      setBusy(false);
    }
  };

  return { loadedKey, setLoadedKey, passphrase, setPassphrase, keyReady, submitKey };
}

export function useLoginFocus(title: string, other: boolean) {
  const pwRef = useRef<HTMLInputElement>(null);
  const userRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    document.title = title;
  }, [title]);
  useEffect(() => {
    (other ? userRef : pwRef).current?.focus();
  }, [other]);
  return { pwRef, userRef };
}
