import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { usePrefs } from '../api';
import { setRegion } from '../lib/format';

export const LANGUAGES = [
  { id: 'en', name: 'English' },
  { id: 'it', name: 'Italiano' },
] as const;
export type Lang = (typeof LANGUAGES)[number]['id'];
export const NAMESPACES = [
  'common', 'shell', 'auth', 'ui', 'settings',
  'overview', 'terminal', 'files', 'logs', 'services', 'software', 'users', 'plugins', 'envs',
] as const;

type Dict = Record<string, string>;
const loaders = import.meta.glob<{ default: Record<string, unknown> }>('./*/*.json');
const cache = new Map<string, Dict>();
const inflight = new Map<string, Promise<void>>();
const listeners = new Set<() => void>();

function flatten(o: Record<string, unknown>, prefix = '', out: Dict = {}): Dict {
  for (const [k, v] of Object.entries(o)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object') flatten(v as Record<string, unknown>, key, out);
    else out[key] = String(v);
  }
  return out;
}

function load(lang: string, ns: string): Promise<void> {
  const id = `${lang}/${ns}`;
  if (cache.has(id)) return Promise.resolve();
  let p = inflight.get(id);
  if (!p) {
    const loader = loaders[`./${id}.json`];
    p = (loader ? loader().then((m) => flatten(m.default)) : Promise.resolve({} as Dict))
      .catch(() => ({}) as Dict)
      .then((d) => {
        cache.set(id, d);
        inflight.delete(id);
        listeners.forEach((l) => l());
      });
    inflight.set(id, p);
  }
  return p;
}

function interpolate(s: string, vars?: Record<string, string | number>): string {
  if (!vars) return s;
  return s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m));
}

function lookup(lang: string, ns: string, key: string, vars?: Record<string, string | number>): string | undefined {
  const chain = lang === 'en' ? ['en'] : [lang, 'en'];
  const keys = vars && typeof vars.count === 'number' ? [`${key}_${vars.count === 1 ? 'one' : 'other'}`, key] : [key];
  for (const l of chain) {
    const d = cache.get(`${l}/${ns}`);
    if (!d) continue;
    for (const k of keys) if (k in d) return interpolate(d[k], vars);
  }
  return undefined;
}

export type TFn = (key: string, vars?: Record<string, string | number>) => string;

interface I18nValue {
  lang: Lang;
  /** Changes when the region settings change, so every useT() consumer re-renders its dates. */
  regionKey: string;
  setLang(l: Lang): void;
}
const Ctx = createContext<I18nValue | null>(null);
const initialLang = (): Lang => {
  try {
    const l = localStorage.getItem('ervisio.lang');
    return l === 'it' ? 'it' : 'en';
  } catch {
    return 'en';
  }
};

export function I18nProvider({ children }: { children: ReactNode }) {
  const { prefs, set } = usePrefs();
  const pref = prefs.language === 'it' || prefs.language === 'en' ? (prefs.language as Lang) : undefined;
  const lang: Lang = pref ?? initialLang();
  const [ready, setReady] = useState(false);
  // Region settings for lib/format (set during render so children format with the current values).
  const reg = (prefs.region ?? {}) as { timeFormat?: string; weekStart?: string };
  setRegion({ lang, hour12: reg.timeFormat === '12', weekStart: reg.weekStart === 'sun' ? 0 : reg.weekStart === 'sat' ? 6 : 1 });

  useEffect(() => {
    try {
      localStorage.setItem('ervisio.lang', lang);
    } catch {
      /* ignore */
    }
    document.documentElement.lang = lang;
    let live = true;
    const base = ['common', 'shell', 'auth', 'ui'];
    void Promise.all([...base.map((n) => load(lang, n)), ...(lang === 'en' ? [] : base.map((n) => load('en', n)))]).then(() => live && setReady(true));
    return () => {
      live = false;
    };
  }, [lang]);

  const regionKey = `${reg.timeFormat ?? ''}/${reg.weekStart ?? ''}`;
  const value = useMemo<I18nValue>(() => ({ lang, regionKey, setLang: (l) => void set('language', l).catch(() => undefined) }), [lang, regionKey, set]);
  return <Ctx.Provider value={value}>{ready ? children : null}</Ctx.Provider>;
}

export function useI18n(): I18nValue {
  const v = useContext(Ctx);
  if (!v) throw new Error('useI18n must be used inside <I18nProvider>');
  return v;
}

/**
 * `const t = useT('services'); t('title'); t('count', {count: 3})` (plural keys: count_one / count_other).
 * Keys in nested JSON are addressed with dots. Falls back to English, then to the key.
 */
export function useT(ns: string): TFn {
  const { lang } = useI18n();
  const [ver, bump] = useState(0);
  useEffect(() => {
    const l = () => bump((n) => n + 1);
    listeners.add(l);
    void load(lang, ns);
    if (lang !== 'en') void load('en', ns);
    return () => void listeners.delete(l);
  }, [lang, ns]);
  return useCallback<TFn>((key, vars) => lookup(lang, ns, key, vars) ?? (cache.has(`en/${ns}`) ? key : ''), [lang, ns, ver]); // eslint-disable-line react-hooks/exhaustive-deps
}
