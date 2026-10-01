/** Locale aware number helpers for the dashboard. */
export const num = (v: number, lang: string, digits = 1) => v.toLocaleString(lang, { maximumFractionDigits: digits, minimumFractionDigits: 0 });

const BYTE_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
export function bytes(n: number, lang: string): { value: string; unit: string } {
  let i = 0;
  let v = Math.max(0, n);
  while (v >= 1024 && i < BYTE_UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  return { value: num(v, lang, v >= 100 || i === 0 ? 0 : 1), unit: BYTE_UNITS[i] };
}
export const bytesStr = (n: number, lang: string) => {
  const b = bytes(n, lang);
  return `${b.value} ${b.unit}`;
};

export function rate(n: number, lang: string): { value: string; unit: string } {
  const b = bytes(n, lang);
  return { value: b.value, unit: `${b.unit}/s` };
}

export function duration(sec: number, t: (k: string, v?: Record<string, string | number>) => string): string {
  const s = Math.max(0, Math.floor(sec));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d) return t('machine.dur.dh', { d, h });
  if (h) return t('machine.dur.hm', { h, m });
  return t('machine.dur.m', { m });
}
