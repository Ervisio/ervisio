/**
 * `react` inside this plugin is this file (see vite.config.ts). The real React is the SDK's: sdk.react exists only
 * once activate() runs, so activate calls setReact() before anything renders.
 *
 * Rule for all code in src/: never call a React API at module top level (no createContext, memo, forwardRef or
 * lazy at import time). Create those inside activate() or inside components. Hooks and createElement are fine
 * because they only run while rendering.
 */
import type * as ReactNS from 'react';

type R = typeof ReactNS;
let current: R | undefined;

export function setReact(r: R): void {
  current = r;
}

function react(): R {
  if (!current) throw new Error('React is not ready: setReact() must run in activate() before rendering.');
  return current;
}

/** A function that looks up the SDK's React export when called, not when imported. */
function fwd<K extends keyof R>(name: K): R[K] {
  return ((...args: unknown[]) => (react()[name] as unknown as (...a: unknown[]) => unknown)(...args)) as unknown as R[K];
}

export const Fragment = Symbol.for('react.fragment') as unknown as R['Fragment'];
export const createElement = fwd('createElement');
export const cloneElement = fwd('cloneElement');
export const isValidElement = fwd('isValidElement');
export const createContext = fwd('createContext');
export const createRef = fwd('createRef');
export const forwardRef = fwd('forwardRef');
export const memo = fwd('memo');
export const lazy = fwd('lazy');
export const startTransition = fwd('startTransition');
export const useState = fwd('useState');
export const useReducer = fwd('useReducer');
export const useEffect = fwd('useEffect');
export const useLayoutEffect = fwd('useLayoutEffect');
export const useInsertionEffect = fwd('useInsertionEffect');
export const useRef = fwd('useRef');
export const useMemo = fwd('useMemo');
export const useCallback = fwd('useCallback');
export const useContext = fwd('useContext');
export const useId = fwd('useId');
export const useImperativeHandle = fwd('useImperativeHandle');
export const useSyncExternalStore = fwd('useSyncExternalStore');
export const useTransition = fwd('useTransition');
export const useDeferredValue = fwd('useDeferredValue');
export const useDebugValue = fwd('useDebugValue');

const proxy = new Proxy({} as R, { get: (_t, key) => (react() as unknown as Record<string | symbol, unknown>)[key] });
export default proxy;
