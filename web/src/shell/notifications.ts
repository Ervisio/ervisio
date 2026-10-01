import { useSyncExternalStore } from 'react';
import type { IconName } from '../ui';

/*
 * Notification center: the bell in the top bar lists recent notices (in memory, this tab, newest first, 50 max)
 * and shows an unread dot. Sections push with notify() (re-exported from 'sections'); it does not show a toast,
 * call toast.* yourself when the user should see it right away.
 */
export interface Notice {
  id: number;
  /** Already translated. */
  title: string;
  detail?: string;
  tone?: 'ok' | 'warn' | 'err' | 'info';
  icon?: IconName;
  /** In-app link opened when the notice is clicked, e.g. /logs?file=/var/log/app.log&level=err */
  to?: string;
  /** Notices with the same key replace each other (e.g. one per watched file) instead of piling up. */
  key?: string;
  at: number;
  read: boolean;
}
export type NoticeInput = Omit<Notice, 'id' | 'at' | 'read'>;

let list: Notice[] = [];
let seq = 1;
const subs = new Set<() => void>();
const emit = () => {
  list = [...list];
  subs.forEach((s) => s());
};

/** Push an entry to the bell menu in the top bar. Returns its id. */
export function notify(n: NoticeInput): number {
  const id = seq++;
  const rest = n.key ? list.filter((x) => x.key !== n.key) : list;
  list = [{ ...n, id, at: Date.now(), read: false }, ...rest].slice(0, 50);
  emit();
  return id;
}
export const dismissNotice = (id: number) => {
  list = list.filter((n) => n.id !== id);
  emit();
};
export const markAllRead = () => {
  if (!list.some((n) => !n.read)) return;
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
