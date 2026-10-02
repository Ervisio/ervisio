import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, requestUnlock, useSession } from '../../api';
import { useT } from '../../i18n';
import { Button, Icon } from '../../ui';

export type LoadState = 'loading' | 'ready' | 'locked' | 'failed';

/**
 * Loads data from an admin-level daemon method without opening the unlock dialog on its own: while the
 * administrator rights are locked the state is 'locked' and the block shows a panel with an Unlock button
 * (see LockedPanel). It loads again when the rights are unlocked, and every `poll` ms while mounted.
 */
export function useAdminLoad<T>(fn: () => Promise<T>, poll = 0) {
  const { isUnlocked } = useSession();
  const [data, setData] = useState<T | null>(null);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState('');
  const fnRef = useRef(fn);
  fnRef.current = fn;
  const load = useCallback(async (quiet = false) => {
    if (!quiet) setState('loading');
    try {
      setData(await fnRef.current());
      setState('ready');
      setError('');
    } catch (e) {
      if (e instanceof ApiError && e.code === 'needs_admin') setState('locked');
      else {
        setError(e instanceof Error ? e.message : String(e));
        if (!quiet) setState('failed');
      }
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load, isUnlocked]);
  useEffect(() => {
    if (!poll) return;
    const id = window.setInterval(() => {
      if (!document.hidden) void load(true);
    }, poll);
    return () => window.clearInterval(id);
  }, [poll, load]);
  return { data, state, error, reload: () => load(true), setData };
}

/** Shown instead of a block's content while administrator rights are locked. */
export function LockedPanel({ title, text, onUnlocked }: { title: string; text: string; onUnlocked(): void }) {
  const t = useT('settings');
  return (
    <div className="st-lock" role="status">
      <Icon name="lock" size={22} />
      <div className="grow"><b>{title}</b><small>{text}</small></div>
      <Button icon="unlock" onClick={() => requestUnlock().then(onUnlocked, () => undefined)}>{t('channels.unlock')}</Button>
    </div>
  );
}
