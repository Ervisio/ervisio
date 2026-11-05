import { useEffect, useRef, useState, type RefObject } from 'react';

export function useMediaQuery(query: string): boolean {
  const get = () => (typeof matchMedia === 'function' ? matchMedia(query).matches : false);
  const [m, setM] = useState(get);
  useEffect(() => {
    const mq = matchMedia(query);
    const on = () => setM(mq.matches);
    on();
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, [query]);
  return m;
}

/** Phones: below 860px, same breakpoint as the shell. */
export const useIsMobile = () => useMediaQuery('(max-width: 859px)');

const FOCUSABLE =
  'a[href],button:not([disabled]),input:not([disabled]):not([type=hidden]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

function focusableIn(el: HTMLElement): HTMLElement[] {
  return Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
    (x) => x.offsetParent !== null || x === document.activeElement,
  );
}

/** Wraps Tab / Shift+Tab around the first and last control of el. */
function wrapTab(e: KeyboardEvent, el: HTMLElement) {
  const list = focusableIn(el);
  if (list.length === 0) {
    e.preventDefault();
    return;
  }
  const first = list[0];
  const last = list[list.length - 1];
  const outside = !el.contains(document.activeElement);
  if (e.shiftKey && (document.activeElement === first || outside)) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && (document.activeElement === last || outside)) {
    e.preventDefault();
    first.focus();
  }
}

/** Traps Tab inside the element, focuses the first control on open and restores focus on close. */
export function useFocusTrap(ref: RefObject<HTMLElement>, active: boolean, onEscape?: () => void) {
  const esc = useRef(onEscape);
  esc.current = onEscape;
  useEffect(() => {
    if (!active || !ref.current) return;
    const el = ref.current;
    const prev = document.activeElement as HTMLElement | null;
    const auto = el.querySelector<HTMLElement>('[data-autofocus]');
    (auto ?? focusableIn(el)[0] ?? el).focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && esc.current) {
        e.stopPropagation();
        esc.current();
      } else if (e.key === 'Tab') {
        wrapTab(e, el);
      }
    };
    el.addEventListener('keydown', onKey);
    return () => {
      el.removeEventListener('keydown', onKey);
      prev?.focus?.();
    };
  }, [active, ref]);
}

let locks = 0;
export function useScrollLock(active: boolean) {
  useEffect(() => {
    if (!active) return;
    locks++;
    document.body.style.overflow = 'hidden';
    return () => {
      if (--locks === 0) document.body.style.overflow = '';
    };
  }, [active]);
}

export function useOutsideClick(ref: RefObject<HTMLElement>, on: () => void, active = true) {
  const cb = useRef(on);
  cb.current = on;
  useEffect(() => {
    if (!active) return;
    const h = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) cb.current();
    };
    document.addEventListener('mousedown', h);
    return () => document.removeEventListener('mousedown', h);
  }, [ref, active]);
}
