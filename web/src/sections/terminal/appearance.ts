/** Native terminal preferences share the existing per-user terminal object. */
export const TERMINAL_FONTS = ['JetBrains Mono', 'Cascadia Code', 'Consolas', 'monospace'] as const;
export interface NativeAppearance {
  fontSize: number;
  fontFamily: string;
  backgroundOpacity: number;
  backgroundBlur: number;
  theme: 'app' | 'dark' | 'light';
  cursorStyle: 'block' | 'bar' | 'underline';
  cursorBlink: boolean;
  lineHeight: number;
  scrollback: number;
}

const bounded = (value: unknown, min: number, max: number, fallback: number) =>
  typeof value === 'number' && Number.isFinite(value) ? Math.max(min, Math.min(max, value)) : fallback;

export function nativeAppearance(value: Record<string, unknown>): NativeAppearance {
  return {
    fontSize: bounded(value.fontSize, 9, 32, 14),
    fontFamily: TERMINAL_FONTS.includes(value.fontFamily as typeof TERMINAL_FONTS[number]) ? String(value.fontFamily) : 'JetBrains Mono',
    backgroundOpacity: bounded(value.backgroundOpacity, 0, 100, 88),
    backgroundBlur: bounded(value.backgroundBlur, 0, 32, 18),
    theme: value.theme === 'dark' || value.theme === 'light' ? value.theme : 'app',
    cursorStyle: value.cursorStyle === 'bar' || value.cursorStyle === 'underline' ? value.cursorStyle : 'block',
    cursorBlink: value.cursorBlink !== false,
    lineHeight: bounded(value.lineHeight, 1, 2, 1.15),
    scrollback: bounded(value.scrollback, 500, 50000, 5000),
  };
}

export function terminalFont(value: string): string {
  return value === 'monospace' ? 'ui-monospace, monospace' : `"${value}", ui-monospace, monospace`;
}
