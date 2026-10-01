import { LEVELS, type Level, type Watcher, type WatchFormat } from './types';

export type RangeId = '15m' | '1h' | '24h' | '7d' | 'custom';
export const RANGES: RangeId[] = ['15m', '1h', '24h', '7d', 'custom'];
export const RANGE_MS: Record<Exclude<RangeId, 'custom'>, number> = {
  '15m': 15 * 60_000,
  '1h': 3_600_000,
  '24h': 86_400_000,
  '7d': 7 * 86_400_000,
};

export interface Filter {
  /** Source ids; ['all'] means everything. */
  sources: string[];
  levels: Level[];
  q: string;
  range: RangeId;
  from?: number;
  to?: number;
  live: boolean;
}

export const DEFAULT_LEVELS: Level[] = ['err', 'warn', 'info'];

const LEVEL_ALIAS: Record<string, Level> = {
  err: 'err', error: 'err', errors: 'err', fatal: 'err', crit: 'err', critical: 'err',
  warn: 'warn', warning: 'warn', warnings: 'warn',
  info: 'info', notice: 'info',
  debug: 'debug', trace: 'debug',
};

const num = (s: string | null) => {
  const n = s ? Number(s) : NaN;
  return Number.isFinite(n) && n > 0 ? n : undefined;
};

/** The URL is the single source of truth for the current filter, so links from other sections work. */
export function parseFilter(p: URLSearchParams): Filter {
  let sources = p.getAll('src').filter(Boolean);
  const unit = p.get('unit');
  const file = p.get('file');
  if (!sources.length && unit) sources = [`unit:${/\.[a-z]+$/.test(unit) ? unit : unit + '.service'}`];
  if (!sources.length && file && file.startsWith('/')) sources = [`file:${file}`];
  if (!sources.length) sources = ['all'];

  const lv = (p.get('level') ?? '')
    .split(',')
    .map((s) => LEVEL_ALIAS[s.trim().toLowerCase()])
    .filter((x): x is Level => !!x);
  const levels = lv.length ? LEVELS.filter((l) => lv.includes(l)) : DEFAULT_LEVELS;

  const r = p.get('range') as RangeId | null;
  const from = num(p.get('from'));
  const to = num(p.get('to'));
  const range: RangeId = r && RANGES.includes(r) ? r : from ? 'custom' : '1h';
  return { sources, levels, q: p.get('q') ?? '', range, from, to, live: p.get('live') === '1' };
}

export function writeFilter(prev: URLSearchParams, patch: Partial<Filter>): URLSearchParams {
  const cur = parseFilter(prev);
  const f: Filter = { ...cur, ...patch };
  const next = new URLSearchParams();
  const keepSrc = patch.sources !== undefined ? patch.sources : cur.sources;
  if (!(keepSrc.length === 1 && keepSrc[0] === 'all')) keepSrc.forEach((s) => next.append('src', s));
  if (f.levels.join() !== DEFAULT_LEVELS.join()) next.set('level', f.levels.join(','));
  if (f.q) next.set('q', f.q);
  if (f.range !== '1h') next.set('range', f.range);
  if (f.range === 'custom') {
    if (f.from) next.set('from', String(f.from));
    if (f.to) next.set('to', String(f.to));
  }
  if (f.live) next.set('live', '1');
  return next;
}

export function bounds(f: Filter, anchor: number): { since: number; until?: number } {
  if (f.range === 'custom') return { since: f.from ?? anchor - RANGE_MS['24h'], until: f.to };
  return { since: anchor - RANGE_MS[f.range] };
}

export function spanMs(f: Filter, anchor: number): number {
  const b = bounds(f, anchor);
  return (b.until ?? anchor) - b.since;
}

// ---------- time ----------
const p2 = (n: number) => String(n).padStart(2, '0');

export function fmtClock(ts: number, ms = true): string {
  const d = new Date(ts);
  const base = `${p2(d.getHours())}:${p2(d.getMinutes())}:${p2(d.getSeconds())}`;
  return ms ? `${base}.${String(d.getMilliseconds()).padStart(3, '0')}` : base;
}

export function fmtDay(ts: number, lang: string): string {
  return new Date(ts).toLocaleDateString(lang, { month: 'short', day: 'numeric' });
}

export function fmtFull(ts: number, lang: string): string {
  const d = new Date(ts);
  const date = d.toLocaleDateString(lang, { weekday: 'short', day: 'numeric', month: 'short' });
  return `${date}, ${fmtClock(ts)}`;
}

export function fmtAxis(ts: number, span: number, lang: string): string {
  if (span > 36 * 3_600_000) return `${fmtDay(ts, lang)} ${p2(new Date(ts).getHours())}:00`;
  return fmtClock(ts, false).slice(0, 5);
}

export function toLocalInput(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}T${p2(d.getHours())}:${p2(d.getMinutes())}`;
}

// ---------- misc ----------
const HUES = ['file', 'plg', 'term', 'ov', 'sw', 'svc', 'usr', 'log'] as const;
export function hueFor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return HUES[h % HUES.length];
}

export function normalizeWatchers(v: unknown): Watcher[] {
  if (!Array.isArray(v)) return [];
  const out: Watcher[] = [];
  for (const w of v) {
    if (!w || typeof w.path !== 'string' || !w.path.startsWith('/')) continue;
    const format: WatchFormat = w.format === 'plain' || w.format === 'json' ? w.format : 'auto';
    out.push({
      path: w.path,
      name: typeof w.name === 'string' && w.name ? w.name : w.path.slice(w.path.lastIndexOf('/') + 1),
      format,
      notify: !!w.notify,
      keepDays: typeof w.keepDays === 'number' ? w.keepDays : 7,
    });
  }
  return out;
}

export function dirOf(path: string): string {
  const i = path.lastIndexOf('/');
  return i <= 0 ? '/' : path.slice(0, i);
}

export function entryText(e: { ts: number; level: string; source: string; message: string; raw?: string }): string {
  return e.raw ?? `${new Date(e.ts).toISOString()} ${e.level} ${e.source}: ${e.message}`;
}
