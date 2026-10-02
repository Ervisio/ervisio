/**
 * Strings. Each area owns one file in ./areas/ that default-exports { en, it }. Every file there is picked up at
 * build time and merged at activation, so adding an area never touches this file. Keys are namespaced by area
 * ("containers.title"); `common.*` is shared.
 */
import { getSdk } from '../sdk';

export type Dict = Record<string, string>;
export interface AreaStrings {
  en: Dict;
  it: Dict;
}

const modules = import.meta.glob<{ default: AreaStrings }>('./areas/*.ts', { eager: true });

export function mergedStrings(): AreaStrings {
  const out: AreaStrings = { en: {}, it: {} };
  for (const path of Object.keys(modules).sort()) {
    const m = modules[path].default;
    Object.assign(out.en, m.en);
    Object.assign(out.it, m.it);
  }
  return out;
}

export function registerAllStrings(): void {
  const s = mergedStrings();
  getSdk().registerStrings({ en: s.en, it: s.it });
}

/** Translate. `t('containers.title')`, `t('common.count', { n: 3 })`. Falls back to English, then to the key. */
export const t = (key: string, vars?: Record<string, string | number>): string => getSdk().t(key, vars);

/** Like `t`, but uses `<key>.one` when `vars.n` is 1 and that key exists ("1 volume" instead of "1 volumes"). */
export const tn = (key: string, vars: Record<string, string | number> & { n: number }): string => {
  if (vars.n === 1) {
    const one = t(`${key}.one`, vars);
    if (one !== `${key}.one`) return one;
  }
  return t(key, vars);
};
