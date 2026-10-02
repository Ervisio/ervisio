/** Number and time formatters. Pure, no SDK needed except for the language in relativeTime. */
import { getSdk } from '../sdk';

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

/** 1536 -> "1.5 KB". Uses 1024 steps, shows no decimals for bytes and one for 10 or more. */
export function formatBytes(n: number | null | undefined, digits?: number): string {
  if (n === null || n === undefined || !isFinite(n) || n < 0) return '–';
  let v = n;
  let i = 0;
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  const d = digits ?? (i === 0 ? 0 : v >= 100 ? 0 : v >= 10 ? 1 : 2);
  return `${v.toFixed(d).replace(/\.0+$/, '')} ${UNITS[i]}`;
}

export const formatRate = (bytesPerSec: number): string => `${formatBytes(bytesPerSec)}/s`;

/** 12.434 -> "12.4%". */
export function formatPercent(v: number | null | undefined, digits = 1): string {
  if (v === null || v === undefined || !isFinite(v)) return '–';
  return `${v.toFixed(v >= 100 ? 0 : digits)}%`;
}

/** Seconds -> "3d 4h", "2h 5m", "45s". */
export function formatDuration(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m ${s % 60}s`;
  return `${s}s`;
}

/** Unix seconds or an ISO string -> "3 days ago" in the app's language. */
export function relativeTime(when: number | string): string {
  const ms = typeof when === 'number' ? when * 1000 : Date.parse(when);
  if (!isFinite(ms)) return '–';
  const diff = (ms - Date.now()) / 1000;
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(getSdk().lang(), { numeric: 'auto' });
  if (abs < 60) return rtf.format(Math.round(diff), 'second');
  if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour');
  if (abs < 86400 * 60) return rtf.format(Math.round(diff / 86400), 'day');
  if (abs < 86400 * 365) return rtf.format(Math.round(diff / (86400 * 30)), 'month');
  return rtf.format(Math.round(diff / (86400 * 365)), 'year');
}

const IT_UNITS: Record<string, [string, string]> = {
  second: ['secondo', 'secondi'], minute: ['minuto', 'minuti'], hour: ['ora', 'ore'], day: ['giorno', 'giorni'],
  week: ['settimana', 'settimane'], month: ['mese', 'mesi'], year: ['anno', 'anni'],
};

/** Italian for a span like "10 minutes", "About an hour", "Less than a second". */
function spanIt(span: string): string {
  const x = span.trim();
  if (/^less than a second$/i.test(x)) return 'meno di un secondo';
  const about = /^about an? (\w+?)s?$/i.exec(x);
  if (about) return about[1].toLowerCase() === 'hour' ? "circa un'ora" : `circa un ${IT_UNITS[about[1].toLowerCase()]?.[0] ?? about[1]}`;
  const m = /^(\d+) (\w+?)s?$/.exec(x);
  const u = m && IT_UNITS[m[2].toLowerCase()];
  if (m && u) return `${m[1]} ${m[1] === '1' ? u[0] : u[1]}`;
  return x;
}

/** The Engine's status text ("Up 3 minutes (unhealthy)", "Exited (0) 2 hours ago") in the app's language. */
export function localStatus(status: string): string {
  if (getSdk().lang() !== 'it') return status;
  let m = /^Up (.+?)( \((?:health: )?(unhealthy|healthy|health: starting|starting)\))?$/.exec(status);
  if (m) return `Attivo da ${spanIt(m[1])}${m[3] ? ` (${m[3].replace(/^health: /, '') === 'unhealthy' ? 'non integro' : m[3].replace(/^health: /, '') === 'healthy' ? 'integro' : 'in avvio'})` : ''}`;
  m = /^(Exited|Restarting|Dead) (\(\d+\)) (.+) ago$/.exec(status);
  if (m) return `${m[1] === 'Exited' ? 'Fermo' : m[1] === 'Restarting' ? 'Si riavvia' : 'Morto'} ${m[2]} ${spanIt(m[3])} fa`;
  const simple: Record<string, string> = { Created: 'Creato', Paused: 'In pausa', Dead: 'Morto', Removing: 'In rimozione', Restarting: 'Si riavvia' };
  return simple[status] ?? status;
}

/** Container name without the leading slash. */
export const containerName = (c: { Names?: string[] }): string => (c.Names?.[0] ?? '').replace(/^\//, '') || '?';

/** "ghcr.io/org/app:1.2" -> "app:1.2" */
export const shortImage = (image: string): string => image.replace(/^.*\//, '');

/** First 12 characters of an id, without a "sha256:" prefix. */
export const shortId = (id: string): string => id.replace(/^sha256:/, '').slice(0, 12);
