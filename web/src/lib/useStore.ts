import { useSyncExternalStore } from 'react';

/** Subscribes a component to a module-level store: `subs` is its listener set, `read` returns the current snapshot. */
export function useStore<T>(subs: Set<() => void>, read: () => T): T {
  return useSyncExternalStore(
    (cb) => {
      subs.add(cb);
      return () => void subs.delete(cb);
    },
    read,
  );
}
