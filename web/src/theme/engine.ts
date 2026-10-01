import { HUE_KEYS, type ColourMode, type ThemeDef } from './themes';

const STATUS_DARK = { ok: '#3DDC97', warn: '#FFB547', err: '#FF6B81', info: '#5AB0FF' };
const STATUS_LIGHT = { ok: '#16805A', warn: '#A9620D', err: '#B5324A', info: '#2C73C9' };

function luminance(hex: string): number {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return 0;
  const n = parseInt(m[1], 16);
  const ch = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
}

/** Black on bright accents, white on dark accents. */
export const onColour = (hex: string) => (luminance(hex) > 0.28 ? '#000000' : '#FFFFFF');

export function effectiveHues(t: ThemeDef, mode: ColourMode, distro: string): Record<string, string> {
  if (t.own || mode === 'sections') return { ...t.hues };
  const c =
    mode === 'mono'
      ? t.kind === 'dark' ? '#D4D4D8' : '#3F3F46'
      : t.kind === 'dark' ? `color-mix(in srgb, ${distro} 85%, #fff)` : distro;
  return Object.fromEntries(HUE_KEYS.map((k) => [k, c]));
}

export function themeVars(t: ThemeDef, mode: ColourMode, distroColour?: string): Record<string, string> {
  const H = effectiveHues(t, mode, distroColour ?? t.distro);
  const dc = t.own ? t.distro : mode === 'mono' ? H.ov : distroColour ?? t.distro;
  const mix = t.kind === 'dark' ? t.bg : '#ffffff';
  const p = t.kind === 'dark' ? 16 : 14;
  const st = t.kind === 'dark' ? STATUS_DARK : STATUS_LIGHT;
  const v: Record<string, string> = {
    '--bg': t.bg,
    '--surface': t.surface,
    '--sunk': t.sunk,
    '--line': t.line,
    '--ink': t.ink,
    '--ink2': t.ink2,
    '--ink3': t.ink3,
    '--distro': dc,
    '--distro-s': `color-mix(in srgb, ${dc} ${p}%, ${mix})`,
  };
  for (const k of HUE_KEYS) {
    v[`--h-${k}`] = H[k];
    v[`--h-${k}-s`] = `color-mix(in srgb, ${H[k]} ${p}%, ${mix})`;
  }
  v['--acc'] = H.ov;
  v['--acc-s'] = `color-mix(in srgb, ${H.ov} ${p + 2}%, ${mix})`;
  // contrast colour needs a concrete hex; the distro mix is approximated by its source colour
  const accHex = H.ov.startsWith('#') ? H.ov : dc;
  v['--on-acc'] = onColour(accHex);
  for (const [k, c] of Object.entries(st)) {
    v[`--${k}`] = c;
    v[`--${k}-s`] = `color-mix(in srgb, ${c} ${p}%, ${mix})`;
  }
  v['--scrim'] = t.kind === 'dark' ? 'rgba(0,0,0,.62)' : 'rgba(20,24,40,.38)';
  v['--shadow'] = t.kind === 'dark' ? '0 24px 60px -20px rgba(0,0,0,.9)' : '0 24px 60px -24px rgba(29,32,48,.35)';
  return v;
}

export function applyVars(vars: Record<string, string>, scheme: 'dark' | 'light') {
  const r = document.documentElement;
  for (const k in vars) r.style.setProperty(k, vars[k]);
  r.style.colorScheme = scheme;
  r.dataset.scheme = scheme;
  let m = document.querySelector('meta[name="theme-color"]');
  if (!m) {
    m = document.createElement('meta');
    m.setAttribute('name', 'theme-color');
    document.head.appendChild(m);
  }
  m.setAttribute('content', vars['--bg']);
  try {
    localStorage.setItem('la.themeVars', JSON.stringify({ vars, scheme }));
  } catch {
    /* ignore */
  }
}

const HEX = /^#[0-9a-f]{6}$/i;
/** Validate and normalise an imported or edited theme. Returns null when unusable. */
export function parseTheme(raw: unknown): ThemeDef | null {
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, any>;
  const keys = ['bg', 'surface', 'sunk', 'line', 'ink', 'ink2', 'ink3'] as const;
  for (const k of keys) if (typeof o[k] !== 'string' || !HEX.test(o[k])) return null;
  if (!o.hues || typeof o.hues !== 'object') return null;
  for (const k of HUE_KEYS) if (typeof o.hues[k] !== 'string' || !HEX.test(o.hues[k])) return null;
  const kind = o.kind === 'light' || o.kind === 'dark' ? o.kind : luminance(o.bg) > 0.4 ? 'light' : 'dark';
  return {
    id: typeof o.id === 'string' && o.id ? o.id : `custom-${Date.now().toString(36)}`,
    name: typeof o.name === 'string' && o.name.trim() ? o.name.trim().slice(0, 40) : 'My theme',
    kind,
    own: !!o.own,
    custom: true,
    bg: o.bg, surface: o.surface, sunk: o.sunk, line: o.line, ink: o.ink, ink2: o.ink2, ink3: o.ink3,
    hues: Object.fromEntries(HUE_KEYS.map((k) => [k, o.hues[k]])) as ThemeDef['hues'],
    distro: typeof o.distro === 'string' && HEX.test(o.distro) ? o.distro : o.hues.ov,
  };
}
