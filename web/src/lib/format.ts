/** Small formatting helpers shared by sections. */
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

export function relativeTime(date: Date | number | string, lang = 'en'): string {
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
