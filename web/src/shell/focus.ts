import { useEffect, useSyncExternalStore } from 'react';

/*
 * Focus mode: the shell hides the rail, the top bar and the phone dock, and the content panel takes the
 * whole window. A section asks for it with useFocusMode(on) while it wants it; the shell turns it off by
 * itself when that section unmounts (navigation). Keyboard exits stay with the section (it knows which
 * keys are free: the terminal uses F11 / Ctrl+Shift+F, never Escape).
 */
let owner: string | null = null;
const subs = new Set<() => void>();
const emit = () => subs.forEach((s) => s());

/** Imperative form. Prefer the useFocusMode hook. */
export function setFocusMode(id: string, on: boolean) {
  if (on) owner = id;
  else if (owner === id) owner = null;
  else return;
  emit();
}

/** Keep the shell in focus mode while `on` is true and the calling component is mounted. */
export function useFocusMode(id: string, on: boolean) {
  useEffect(() => {
    setFocusMode(id, on);
    return () => setFocusMode(id, false);
  }, [id, on]);
}

/** For the shell: is focus mode on? */
export function useFocusActive(): boolean {
  return useSyncExternalStore(
    (cb) => {
      subs.add(cb);
      return () => void subs.delete(cb);
    },
    () => owner !== null,
  );
}
