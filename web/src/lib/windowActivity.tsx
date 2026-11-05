import {
  createContext,
  useCallback,
  useContext,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from 'react';

const Activity = createContext(true);
const visibilitySubscribers = new Set<() => void>();
const visible = () => typeof document === 'undefined' || !document.hidden;
const notifyVisibility = () => visibilitySubscribers.forEach((notify) => notify());
function subscribeVisibility(notify: () => void) {
  if (!visibilitySubscribers.size) document.addEventListener('visibilitychange', notifyVisibility);
  visibilitySubscribers.add(notify);
  return () => {
    visibilitySubscribers.delete(notify);
    if (!visibilitySubscribers.size)
      document.removeEventListener('visibilitychange', notifyVisibility);
  };
}

/** A hidden desktop window stays mounted, retaining its state while UI reads sleep. */
export function WindowActivityProvider({
  active,
  children,
}: {
  active: boolean;
  children: ReactNode;
}) {
  const parent = useContext(Activity);
  return <Activity.Provider value={parent && active}>{children}</Activity.Provider>;
}

/** Classic defaults to active. Browser visibility also pauses each window's UI work. */
export function useWindowActivity(): boolean {
  const active = useContext(Activity);
  const tabVisible = useSyncExternalStore(subscribeVisibility, visible, () => true);
  return active && tabVisible;
}

/** Unsubscribe hidden UI consumers without discarding their last rendered snapshot. */
export function useActivityExternalStore<T>(
  subscribe: (notify: () => void) => () => void,
  getSnapshot: () => T,
): T {
  const active = useWindowActivity();
  const last = useRef(getSnapshot());
  const listen = useCallback(
    (notify: () => void) => (active ? subscribe(notify) : () => undefined),
    [active, subscribe],
  );
  const read = useCallback(() => {
    if (active) last.current = getSnapshot();
    return last.current;
  }, [active, getSnapshot]);
  return useSyncExternalStore(listen, read, read);
}
