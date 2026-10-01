/** Small formatting helpers shared by sections. */

/* ---------- region (Settings > Language & region), set by the I18n provider from the user's prefs ---------- */
export interface Region {
  /** UI language, also used as the locale for dates and numbers. */
  lang: string;
  /** 12-hour clock (prefs region.timeFormat = "12"). */
  hour12: boolean;
  /** First day of the week: 0 Sunday, 1 Monday, 6 Saturday. */
  weekStart: 0 | 1 | 6;
}
let region: Region = { lang: 'en', hour12: false, weekStart: 1 };
export function setRegion(r: Partial<Region>) {
  region = { ...region, ...r };
}
export const getRegion = (): Readonly<Region> => region;
/** Intl options for hours/minutes that follow the 12/24 h setting. */
export const clockOptions = (seconds = false): Intl.DateTimeFormatOptions => ({
  hour: region.hour12 ? 'numeric' : '2-digit',
  minute: '2-digit',
  ...(seconds ? { second: '2-digit' } : {}),
  hourCycle: region.hour12 ? 'h12' : 'h23',
});

/** Time of day ("14:05", "2:05 PM"), optionally with seconds and milliseconds. */
export function formatTime(date: Date | number | string, o: { seconds?: boolean; ms?: boolean; lang?: string } = {}): string {
  const d = new Date(date);
  if (Number.isNaN(d.getTime())) return '';
  const opts = clockOptions(o.seconds || o.ms);
  if (!o.ms) return d.toLocaleTimeString(o.lang ?? region.lang, opts);
  const parts = new Intl.DateTimeFormat(o.lang ?? region.lang, { ...opts, fractionalSecondDigits: 3 } as Intl.DateTimeFormatOptions).format(d);
  return parts;
}

/** Calendar date in the user's language ("1 Oct", "1 Oct 2025" when not this year). */
export function formatDate(date: Date | number | string, o: Intl.DateTimeFormatOptions = {}): string {
  const d = new Date(date);
  if (Number.isNaN(d.getTime())) return '';
  const base: Intl.DateTimeFormatOptions = d.getFullYear() === new Date().getFullYear() ? { day: 'numeric', month: 'short' } : { day: 'numeric', month: 'short', year: 'numeric' };
  return d.toLocaleDateString(region.lang, Object.keys(o).length ? o : base);
}

/** Date and time ("1 Oct, 14:05"). */
export function formatDateTime(date: Date | number | string): string {
  return `${formatDate(date)}, ${formatTime(date)}`;
}
export function formatBytes(n: number, digits = 1): string {
  if (!Number.isFinite(n)) return '-';
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0;
  let v = Math.abs(n);
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${n < 0 ? '-' : ''}${i === 0 ? v.toFixed(0) : v.toFixed(digits)} ${u[i]}`;
}

export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m ${s % 60}s`;
  return `${s}s`;
}

/** mm:ss for countdowns. */
export function formatClock(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

export function formatPercent(v: number, digits = 0): string {
  return `${v.toFixed(digits)}%`;
}

export function relativeTime(date: Date | number | string, lang = region.lang): string {
  const t = new Date(date).getTime();
  const diff = (t - Date.now()) / 1000;
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'auto' });
  const steps: [number, Intl.RelativeTimeFormatUnit][] = [[60, 'second'], [3600, 'minute'], [86400, 'hour'], [2592000, 'day'], [31536000, 'month']];
  let div = 1;
  for (const [limit, unit] of steps) {
    if (Math.abs(diff) < limit) return rtf.format(Math.round(diff / div), unit);
    div = limit;
  }
  return rtf.format(Math.round(diff / 31536000), 'year');
}
