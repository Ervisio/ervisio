/**
 * Stand-in for web/src/i18n inside the plugin frame runtime (aliased by vite.runtime.config.ts): the UI kit
 * only needs its own "ui" strings, bundled here, and the language the app sends over postMessage.
 */
import { useCallback, useSyncExternalStore } from 'react';
import enUi from '../../i18n/en/ui.json';
import itUi from '../../i18n/it/ui.json';

export type TFn = (key: string, vars?: Record<string, string | number>) => string;
type Dict = Record<string, string>;

function flatten(o: Record<string, unknown>, prefix = '', out: Dict = {}): Dict {
  for (const [k, v] of Object.entries(o)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object') flatten(v as Record<string, unknown>, key, out);
    else out[key] = String(v);
  }
  return out;
}

const DICTS: Record<string, Dict> = { en: flatten(enUi), it: flatten(itUi) };
let lang = 'en';
const listeners = new Set<() => void>();

export function setLang(l: string) {
  const next = l in DICTS ? l : 'en';
  if (next === lang) return;
  lang = next;
  listeners.forEach((f) => f());
}
export const getLang = () => lang;
export function subscribeLang(f: () => void): () => void {
  listeners.add(f);
  return () => void listeners.delete(f);
}

const interpolate = (s: string, vars?: Record<string, string | number>) =>
  vars ? s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m)) : s;

export function useT(_ns: string): TFn {
  const l = useSyncExternalStore(subscribeLang, getLang);
  return useCallback<TFn>(
    (key, vars) => {
      const keys = vars && typeof vars.count === 'number' ? [`${key}_${vars.count === 1 ? 'one' : 'other'}`, key] : [key];
      for (const d of [DICTS[l], DICTS.en]) for (const k of keys) if (d && k in d) return interpolate(d[k], vars);
      return key;
    },
    [l],
  );
}
