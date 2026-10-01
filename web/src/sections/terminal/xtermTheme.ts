import type { ITheme } from '@xterm/xterm';

let ctx: CanvasRenderingContext2D | null = null;

/** Resolve any CSS colour (var(), color-mix(), names) to #rrggbb, which xterm always understands. */
export function resolveColour(css: string, fallback = '#888888'): string {
  try {
    if (!ctx) {
      const c = document.createElement('canvas');
      c.width = c.height = 1;
      ctx = c.getContext('2d', { willReadFrequently: true });
    }
    if (!ctx) return fallback;
    const probe = document.createElement('span');
    probe.style.color = css;
    document.body.appendChild(probe);
    const computed = getComputedStyle(probe).color;
    probe.remove();
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = '#000';
    ctx.fillStyle = computed || css;
    ctx.fillRect(0, 0, 1, 1);
    const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
    return '#' + [r, g, b].map((v) => v.toString(16).padStart(2, '0')).join('');
  } catch {
    return fallback;
  }
}

const v = (name: string) => `var(${name})`;
const mix = (c: string, pct: number, other: string) => `color-mix(in srgb, ${c} ${pct}%, ${other})`;

const FIXED_DARK = { bg: '#0F1218', fg: '#D9DEE8', dim: '#6B7385', g: '#6FDDB0', b: '#7DB6FF', y: '#F5B963', r: '#FF8A9B', m: '#C6A2FF', c: '#6FD8DE', sel: '#24304A' };
const FIXED_LIGHT = { bg: '#FBFBFD', fg: '#1D2030', dim: '#8D92A5', g: '#16805A', b: '#2C73C9', y: '#A9620D', r: '#B5324A', m: '#7A43C2', c: '#1B7F86', sel: '#DDEBFB' };

function fromFixed(f: typeof FIXED_DARK, dark: boolean): ITheme {
  const lift = (c: string) => resolveColour(mix(c, 78, dark ? '#fff' : '#000'), c);
  return {
    background: f.bg, foreground: f.fg, cursor: f.g, cursorAccent: f.bg, selectionBackground: f.sel,
    black: dark ? '#2A3040' : '#1D2030', red: f.r, green: f.g, yellow: f.y, blue: f.b, magenta: f.m, cyan: f.c, white: dark ? '#A7AEBF' : '#6B7385',
    brightBlack: f.dim, brightRed: lift(f.r), brightGreen: lift(f.g), brightYellow: lift(f.y), brightBlue: lift(f.b), brightMagenta: lift(f.m), brightCyan: lift(f.c), brightWhite: f.fg,
  };
}

/** The scheme (dark or light) the terminal should use for a "terminal.theme" preference. */
export function terminalScheme(mode: string): 'dark' | 'light' {
  if (mode === 'dark' || mode === 'light') return mode;
  return document.documentElement.dataset.scheme === 'light' ? 'light' : 'dark';
}

/**
 * Terminal palette from the active theme tokens. "app" follows the theme; "dark" and "light" force the
 * fixed palettes from the design when the app is in the other scheme.
 */
export function buildTheme(mode: string): ITheme {
  const appScheme = document.documentElement.dataset.scheme === 'light' ? 'light' : 'dark';
  const scheme = terminalScheme(mode);
  if (mode !== 'app' && scheme !== appScheme) return fromFixed(scheme === 'dark' ? FIXED_DARK : FIXED_LIGHT, scheme === 'dark');
  const dark = scheme === 'dark';
  const R = (c: string, fb: string) => resolveColour(c, fb);
  const edge = dark ? '#fff' : '#000';
  const lift = (c: string) => R(mix(c, 78, edge), '#ffffff');
  return {
    background: R(dark ? v('--bg') : v('--sunk'), dark ? '#000000' : '#f3f4f8'),
    foreground: R(v('--ink'), '#d9dee8'),
    cursor: R(v('--h-term'), '#3ddc97'),
    cursorAccent: R(dark ? v('--bg') : v('--sunk'), '#000000'),
    selectionBackground: R(mix(v('--h-term'), 28, dark ? v('--bg') : '#fff'), '#24304a'),
    black: R(dark ? v('--line') : v('--ink'), '#26262b'),
    red: R(v('--err'), '#ff6b81'),
    green: R(v('--ok'), '#3ddc97'),
    yellow: R(v('--warn'), '#ffb547'),
    blue: R(v('--info'), '#5ab0ff'),
    magenta: R(v('--h-sw'), '#b98cff'),
    cyan: R(v('--h-usr'), '#3fd8de'),
    white: R(dark ? v('--ink2') : v('--ink3'), '#a3a3ad'),
    brightBlack: R(v('--ink3'), '#6c6c76'),
    brightRed: lift(v('--err')),
    brightGreen: lift(v('--ok')),
    brightYellow: lift(v('--warn')),
    brightBlue: lift(v('--info')),
    brightMagenta: lift(v('--h-sw')),
    brightCyan: lift(v('--h-usr')),
    brightWhite: R(v('--ink'), '#f4f4f6'),
  };
}
