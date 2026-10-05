export type Level = 'err' | 'warn' | 'info' | 'debug';
export const LEVELS: Level[] = ['err', 'warn', 'info', 'debug'];

export interface Entry {
  ts: number;
  tsUs: number;
  level: Level;
  source: string;
  srcId: string;
  message: string;
  raw?: string;
  unit?: string;
  pid?: number;
  file?: string;
  line?: number;
  cursor: string;
  /** Client-side unique key. */
  key: string;
}

export interface Source {
  id: string;
  kind: 'journal' | 'unit' | 'kernel' | 'file' | 'evt';
  group: 'system' | 'services' | 'files' | 'watchers';
  label: string;
  hint?: string;
  path?: string;
  unit?: string;
  count24h: number;
  errors24h: number;
  size?: number;
  needsAdmin?: boolean;
  format?: string;
  notify?: boolean;
}

export interface SourcesResult {
  groups: { id: Source['group']; sources: Source[] }[];
  total24h: number;
  errors24h: number;
  approx?: boolean;
  journalReadable: boolean;
}

export interface QueryResult {
  entries: Omit<Entry, 'key'>[];
  next?: string;
  hasMore: boolean;
  skipped?: { id: string; code: string; message: string }[];
}

export interface Bucket {
  t: number;
  err: number;
  warn: number;
  info: number;
  debug: number;
}

export interface HistogramResult {
  since: number;
  until: number;
  step: number;
  buckets: Bucket[];
  truncated?: boolean;
}

export interface ContextResult {
  entries: Omit<Entry, 'key'>[];
  index: number;
}

export type WatchFormat = 'plain' | 'auto' | 'json';

export interface Watcher {
  path: string;
  name: string;
  format: WatchFormat;
  notify: boolean;
  keepDays: number;
}
