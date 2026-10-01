import { useSyncExternalStore } from 'react';
import type { IconName } from '../ui';

export interface Notice {
  id: number;
  title: string;
  detail?: string;
  tone?: 'ok' | 'warn' | 'err' | 'info';
  icon?: IconName;
  at: number;
  read: boolean;
}
let list: Notice[] = [];
let seq = 1;
const subs = new Set<() => void>();
const emit = () => {
  list = [...list];
  subs.forEach((s) => s());
};

/** Push an entry to the bell menu in the top bar (in memory, for this tab). */
export function notify(n: Omit<Notice, 'id' | 'at' | 'read'>) {
  list = [{ ...n, id: seq++, at: Date.now(), read: false }, ...list].slice(0, 50);
  emit();
}
export const markAllRead = () => {
  list = list.map((n) => ({ ...n, read: true }));
  emit();
};
export const clearNotices = () => {
  list = [];
  emit();
};
export function useNotices(): Notice[] {
  return useSyncExternalStore(
    (cb) => {
      subs.add(cb);
      return () => void subs.delete(cb);
    },
    () => list,
  );
}
