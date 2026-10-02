import { useEffect, useState } from 'react';
import { CONFIG_DIR } from '../../settings';
import { getSdk } from '../../sdk';

/** Env keys that probably hold a secret: their values are masked until the user reveals them. */
export const looksSecret = (key: string): boolean => /pass|secret|token|key|pwd|credential|auth|private|cert/i.test(key);

/** Copies text. The plugin frame is sandboxed, so the async clipboard API can be refused: fall back to execCommand. */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    /* fall through */
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0;pointer-events:none';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    return ok;
  } catch {
    return false;
  }
}

/** Re-renders every `ms` so relative times and uptimes stay fresh. */
export function useNow(ms = 15000): number {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const i = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(i);
  }, [ms]);
  return now;
}

/* ---------- remembered choices (shell, log tail, wrap) ---------- */

export interface ContainerPrefs {
  shell: string;
  tail: string;
  wrap: boolean;
  timestamps: boolean;
}
const PREFS_PATH = `${CONFIG_DIR}/container-ui.json`;
const DEFAULT_PREFS: ContainerPrefs = { shell: '/bin/sh', tail: '500', wrap: true, timestamps: true };
let prefs: ContainerPrefs = { ...DEFAULT_PREFS };
let loaded: Promise<void> | undefined;

function loadPrefs(): Promise<void> {
  loaded ??= getSdk()
    .files.read(PREFS_PATH)
    .then((s) => {
      prefs = { ...DEFAULT_PREFS, ...JSON.parse(s) };
    })
    .catch(() => undefined);
  return loaded;
}

/** The remembered choices of this page, kept in a small JSON file in the plugin's config folder. */
export function usePrefs(): [ContainerPrefs, (p: Partial<ContainerPrefs>) => void, boolean] {
  const [cur, setCur] = useState<ContainerPrefs>(prefs);
  const [ready, setReady] = useState(false);
  useEffect(() => {
    let live = true;
    void loadPrefs().then(() => {
      if (!live) return;
      setCur(prefs);
      setReady(true);
    });
    return () => {
      live = false;
    };
  }, []);
  const update = (p: Partial<ContainerPrefs>) => {
    prefs = { ...prefs, ...p };
    setCur(prefs);
    void getSdk().files.write(PREFS_PATH, JSON.stringify(prefs)).catch(() => undefined);
  };
  return [cur, update, ready];
}

/** Resolves any CSS colour (var(), color-mix()) to #rrggbb, which xterm needs. */
export function resolveColour(css: string, fallback: string, within: Element): string {
  try {
    const probe = document.createElement('span');
    probe.style.color = css;
    within.appendChild(probe);
    const computed = getComputedStyle(probe).color;
    probe.remove();
    const c = document.createElement('canvas');
    c.width = c.height = 1;
    const ctx = c.getContext('2d', { willReadFrequently: true });
    if (!ctx) return fallback;
    ctx.fillStyle = '#000';
    ctx.fillStyle = computed || css;
    ctx.fillRect(0, 0, 1, 1);
    const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
    return '#' + [r, g, b].map((v) => v.toString(16).padStart(2, '0')).join('');
  } catch {
    return fallback;
  }
}

/** "name" without leading slash from an inspect Name. */
export const nameOf = (n: string): string => n.replace(/^\//, '');

/** Escapes text for use inside a RegExp. */
export const escapeRe = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
