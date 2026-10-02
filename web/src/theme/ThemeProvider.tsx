import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from 'react';
import { usePrefs, useSession } from '../api';
import { applyVars, parseTheme, themeVars } from './engine';
import { BUILTIN_THEMES, DEFAULT_LIGHT_THEME_ID, DEFAULT_THEME_ID, type ColourMode, type ThemeDef } from './themes';

export interface FollowDevice {
  on: boolean;
  dark: string;
  light: string;
}

export interface ThemeValue {
  /** The theme currently painted (after follow-the-device and live preview). */
  theme: ThemeDef;
  isDark: boolean;
  /** Saved choice (ignores follow-the-device and preview). */
  themeId: string;
  themes: ThemeDef[];
  customThemes: ThemeDef[];
  colourMode: ColourMode;
  follow: FollowDevice;
  density: 'comfortable' | 'compact';
  reduceMotion: boolean;
  /** Distro colour in use (host colour or the preview chip). */
  distroColour: string;
  setTheme(id: string): void;
  setColourMode(m: ColourMode): void;
  setFollow(f: FollowDevice): void;
  setDensity(d: 'comfortable' | 'compact'): void;
  setReduceMotion(on: boolean): void;
  /** Light/dark quick toggle for the top bar: swaps to the paired theme. */
  toggleDark(): void;
  /** Paint a theme without saving it (editor, hover). Pass null to stop. */
  preview(t: ThemeDef | null): void;
  previewDistro(color: string | null): void;
  saveCustom(t: ThemeDef): ThemeDef;
  deleteCustom(id: string): void;
  importTheme(json: string): ThemeDef;
  exportTheme(t: ThemeDef): string;
  /** Re-applies the previous values after an Undo. */
  restore(snapshot: ThemeSnapshot): void;
  snapshot(): ThemeSnapshot;
}
export interface ThemeSnapshot {
  themeId: string;
  colourMode: ColourMode;
  follow: FollowDevice;
}

const Ctx = createContext<ThemeValue | null>(null);
const DEFAULT_FOLLOW: FollowDevice = { on: false, dark: DEFAULT_THEME_ID, light: DEFAULT_LIGHT_THEME_ID };

function systemDark(): boolean {
  return typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)').matches : true;
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const { prefs, set } = usePrefs();
  const { host } = useSession();
  const [sysDark, setSysDark] = useState(systemDark);
  const [prev, setPrev] = useState<ThemeDef | null>(null);
  const [prevDistro, setPrevDistro] = useState<string | null>(null);

  useEffect(() => {
    if (typeof matchMedia !== 'function') return;
    const mq = matchMedia('(prefers-color-scheme: dark)');
    const on = () => setSysDark(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);

  const customThemes = useMemo<ThemeDef[]>(
    () => ((prefs.customThemes as unknown[]) ?? []).map(parseTheme).filter((t): t is ThemeDef => !!t),
    [prefs.customThemes],
  );
  const themes = useMemo(() => [...BUILTIN_THEMES, ...customThemes], [customThemes]);
  const find = useCallback((id: string) => themes.find((t) => t.id === id) ?? BUILTIN_THEMES[0], [themes]);

  const themeId: string = typeof prefs.theme === 'string' ? prefs.theme : DEFAULT_THEME_ID;
  const colourMode: ColourMode = prefs.colourMode === 'distro' || prefs.colourMode === 'mono' ? prefs.colourMode : 'sections';
  const follow = useMemo<FollowDevice>(() => ({ ...DEFAULT_FOLLOW, ...(prefs.followDevice ?? {}) }), [prefs.followDevice]);
  const density = prefs.density === 'compact' ? 'compact' : 'comfortable';
  const reduceMotion = !!prefs.reduceMotion;

  const active = prev ?? find(follow.on ? (sysDark ? follow.dark : follow.light) : themeId);
  const distroColour = prevDistro ?? host?.distro.color ?? active.distro;
  const vars = useMemo(() => themeVars(active, colourMode, distroColour), [active, colourMode, distroColour]);

  useLayoutEffect(() => {
    applyVars(vars, active.kind);
  }, [vars, active.kind]);

  useEffect(() => {
    document.documentElement.dataset.density = density;
    document.documentElement.dataset.reduceMotion = reduceMotion ? '1' : '0';
  }, [density, reduceMotion]);

  const save = useCallback((k: string, v: unknown) => void set(k, v).catch(() => undefined), [set]);

  const value = useMemo<ThemeValue>(() => {
    const pair = (): [string, string] => (follow.on ? [follow.dark, follow.light] : [DEFAULT_THEME_ID, DEFAULT_LIGHT_THEME_ID]);
    return {
      theme: active,
      isDark: active.kind === 'dark',
      themeId,
      themes,
      customThemes,
      colourMode,
      follow,
      density,
      reduceMotion,
      distroColour,
      setTheme: (id) => {
        setPrev(null);
        if (follow.on) {
          const t = find(id);
          save('followDevice', { ...follow, [t.kind]: id });
        }
        save('theme', id);
      },
      setColourMode: (m) => save('colourMode', m),
      setFollow: (f) => save('followDevice', f),
      setDensity: (d) => save('density', d),
      setReduceMotion: (on) => save('reduceMotion', on),
      toggleDark: () => {
        setPrev(null);
        const [d, l] = pair();
        const target = active.kind === 'dark' ? l : d;
        if (follow.on) save('followDevice', { ...follow, on: false });
        save('theme', target);
      },
      preview: setPrev,
      previewDistro: setPrevDistro,
      saveCustom: (t) => {
        const clean: ThemeDef = { ...t, custom: true, id: t.id !== 'custom-new' && t.id.startsWith('custom-') ? t.id : `custom-${Date.now().toString(36)}` };
        const list = customThemes.filter((x) => x.id !== clean.id);
        save('customThemes', [...list, clean]);
        return clean;
      },
      deleteCustom: (id) => {
        save('customThemes', customThemes.filter((x) => x.id !== id));
        if (themeId === id) save('theme', DEFAULT_THEME_ID);
      },
      importTheme: (json) => {
        let raw: unknown;
        try {
          raw = JSON.parse(json);
        } catch {
          throw new Error('invalid_json');
        }
        const t = parseTheme(raw);
        if (!t) throw new Error('invalid_theme');
        const clean = { ...t, id: `custom-${Date.now().toString(36)}` };
        save('customThemes', [...customThemes, clean]);
        return clean;
      },
      exportTheme: (t) => {
        const { custom: _c, ...rest } = t;
        void _c;
        return JSON.stringify({ ervisioTheme: 1, ...rest }, null, 2);
      },
      snapshot: () => ({ themeId, colourMode, follow }),
      restore: (s) => {
        save('theme', s.themeId);
        save('colourMode', s.colourMode);
        save('followDevice', s.follow);
      },
    };
  }, [active, themeId, themes, customThemes, colourMode, follow, density, reduceMotion, distroColour, find, save]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTheme(): ThemeValue {
  const v = useContext(Ctx);
  if (!v) throw new Error('useTheme must be used inside <ThemeProvider>');
  return v;
}
