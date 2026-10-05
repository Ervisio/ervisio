import type { HueId, IconName } from '../../ui';

export const SIZES = [3, 4, 5, 6, 7, 12] as const;
export type Cols = (typeof SIZES)[number];

export type WidgetType =
  | 'stat'
  | 'activity'
  | 'chart'
  | 'actions'
  | 'action'
  | 'alerts'
  | 'machine'
  | 'service'
  | 'log'
  | 'output'
  | 'folder'
  | 'plugin';

export interface Widget {
  id: string;
  type: WidgetType | string;
  cols: Cols;
  settings?: Record<string, any>;
}

export interface Layout {
  version: 1;
  widgets: Widget[];
}

export interface DashAction {
  id: string;
  label: string;
  icon: IconName;
  hue: HueId;
  argv: string[];
  admin?: boolean;
  confirm?: boolean;
}

export const HUES: HueId[] = ['ov', 'term', 'file', 'log', 'svc', 'sw', 'usr', 'plg'];
export const ACTION_ICONS: IconName[] = ['play', 'refresh', 'download', 'broom', 'power', 'terminal', 'server', 'shield', 'zap', 'files', 'trash', 'lock', 'star', 'heart', 'globe', 'cog', 'code', 'clock', 'key', 'upload', 'home', 'wifi', 'disk', 'cpu'];

export const uid = (p = 'w') => `${p}${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;

export function snapCols(n: number): Cols {
  let best: Cols = SIZES[0];
  for (const s of SIZES) if (Math.abs(s - n) < Math.abs(best - n)) best = s;
  return best;
}

/** Next/previous size in the SIZES list, clamped. */
export function stepCols(c: number, dir: 1 | -1): Cols {
  const i = SIZES.indexOf(snapCols(c));
  return SIZES[Math.max(0, Math.min(SIZES.length - 1, i + dir))];
}

const WIN_ACTIONS: DashAction[] = [
  { id: 'a-ports', label: 'Listening ports', icon: 'globe', hue: 'file', argv: ['powershell.exe', '-NoProfile', '-Command', 'Get-NetTCPConnection -State Listen'] },
  { id: 'a-disk', label: 'Disk usage', icon: 'disk', hue: 'term', argv: ['powershell.exe', '-NoProfile', '-Command', 'Get-PSDrive -PSProvider FileSystem'] },
];

let windowsHost = false;
/** Called by the page once the session is known: Linux-only default actions are swapped for PowerShell ones. */
export function setWindowsDefaults(win: boolean) {
  windowsHost = win;
}
const defaultActions = (): DashAction[] => (windowsHost ? WIN_ACTIONS : LINUX_ACTIONS);

const LINUX_ACTIONS: DashAction[] = [
  { id: 'a-ports', label: 'Listening ports', icon: 'globe', hue: 'file', argv: ['ss', '-tulpn'] },
  { id: 'a-disk', label: 'Disk usage', icon: 'disk', hue: 'term', argv: ['df', '-h'] },
  { id: 'a-journal', label: 'Trim old logs', icon: 'broom', hue: 'log', argv: ['journalctl', '--vacuum-time=7d'], admin: true },
  { id: 'a-reload', label: 'Reload systemd', icon: 'refresh', hue: 'svc', argv: ['systemctl', 'daemon-reload'], admin: true },
];

export function defaultLayout(): Layout {
  return {
    version: 1,
    widgets: [
      { id: 'w-cpu', type: 'stat', cols: 3, settings: { metric: 'cpu' } },
      { id: 'w-mem', type: 'stat', cols: 3, settings: { metric: 'memory' } },
      { id: 'w-disk', type: 'stat', cols: 3, settings: { metric: 'disk' } },
      { id: 'w-net', type: 'stat', cols: 3, settings: { metric: 'network' } },
      { id: 'w-act', type: 'activity', cols: 7 },
      { id: 'w-actions', type: 'actions', cols: 5, settings: { actions: defaultActions() } },
      { id: 'w-alerts', type: 'alerts', cols: 7 },
      { id: 'w-machine', type: 'machine', cols: 5 },
    ],
  };
}

/** Reads the `dashboard` pref defensively; falls back to the default layout. */
export function normalizeLayout(raw: unknown): Layout {
  const r = raw as Partial<Layout> | null | undefined;
  if (!r || typeof r !== 'object' || !Array.isArray(r.widgets)) return defaultLayout();
  const seen = new Set<string>();
  const widgets: Widget[] = [];
  for (const w of r.widgets as Partial<Widget>[]) {
    if (!w || typeof w !== 'object' || typeof w.type !== 'string') continue;
    let id = typeof w.id === 'string' && w.id ? w.id : uid();
    if (seen.has(id)) id = uid();
    seen.add(id);
    widgets.push({
      id,
      type: w.type,
      cols: snapCols(typeof w.cols === 'number' ? w.cols : 4),
      settings: w.settings && typeof w.settings === 'object' ? (w.settings as Record<string, any>) : undefined,
    });
  }
  return { version: 1, widgets };
}

/** Every action stored in a layout (single buttons and "My actions" tiles). */
export function layoutActions(l: Layout): DashAction[] {
  const out: DashAction[] = [];
  for (const w of l.widgets) {
    if (w.type === 'action' && isAction(w.settings)) out.push(w.settings as DashAction);
    if (w.type === 'actions' && Array.isArray(w.settings?.actions)) out.push(...(w.settings!.actions as unknown[]).filter(isAction));
  }
  return out;
}

export function isAction(a: unknown): a is DashAction {
  const x = a as DashAction | undefined;
  return !!x && Array.isArray(x.argv) && x.argv.length > 0 && x.argv.every((s) => typeof s === 'string') && typeof x.label === 'string';
}

/* ---------- command line <-> argv ---------- */

/** Splits a command line like a POSIX shell would for quoting, without expanding anything. */
export function splitArgv(line: string): string[] | null {
  const out: string[] = [];
  let cur = '';
  let has = false;
  let q: '"' | "'" | null = null;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (q === "'") {
      if (c === "'") q = null;
      else cur += c;
    } else if (q === '"') {
      if (c === '"') q = null;
      else if (c === '\\' && i + 1 < line.length && '"\\$`'.includes(line[i + 1])) cur += line[++i];
      else cur += c;
    } else if (c === '"' || c === "'") {
      q = c;
      has = true;
    } else if (c === '\\' && i + 1 < line.length) {
      cur += line[++i];
      has = true;
    } else if (/\s/.test(c)) {
      if (has || cur) out.push(cur);
      cur = '';
      has = false;
    } else {
      cur += c;
      has = true;
    }
  }
  if (q) return null; // unbalanced quote
  if (has || cur) out.push(cur);
  return out;
}

export function joinArgv(argv: string[]): string {
  return argv.map((a) => (a !== '' && /^[\w@%+=:,./-]+$/.test(a) ? a : `'${a.replace(/'/g, `'\\''`)}'`)).join(' ');
}

/* ---------- widget catalogue ---------- */

export interface CatalogItem {
  type: WidgetType;
  category: 'basics' | 'system' | 'personal';
  icon: IconName;
  hue: HueId;
  cols: Cols;
  /** Open the settings dialog right after adding. */
  configure?: boolean;
  settings?(): Record<string, any> | undefined;
}

export const CATALOG: CatalogItem[] = [
  { type: 'stat', category: 'basics', icon: 'cpu', hue: 'ov', cols: 3, settings: () => ({ metric: 'cpu' }), configure: true },
  { type: 'activity', category: 'basics', icon: 'overview', hue: 'file', cols: 7 },
  { type: 'actions', category: 'basics', icon: 'play', hue: 'sw', cols: 5, settings: () => ({ actions: defaultActions().map((a) => ({ ...a, id: uid('a') })) }) },
  { type: 'alerts', category: 'basics', icon: 'alert', hue: 'svc', cols: 7 },
  { type: 'machine', category: 'basics', icon: 'server', hue: 'ov', cols: 5 },
  { type: 'chart', category: 'system', icon: 'overview', hue: 'ov', cols: 6, settings: () => ({ metric: 'cpu', range: '15m' }), configure: true },
  { type: 'service', category: 'system', icon: 'services', hue: 'svc', cols: 4, settings: () => ({ units: [] }), configure: true },
  { type: 'log', category: 'system', icon: 'logs', hue: 'log', cols: 6, settings: () => ({ source: 'journal', lines: 12 }), configure: true },
  { type: 'action', category: 'personal', icon: 'play', hue: 'sw', cols: 3, settings: () => ({ id: uid('a'), label: '', icon: 'play', hue: 'sw', argv: [] }), configure: true },
  { type: 'output', category: 'personal', icon: 'terminal', hue: 'term', cols: 6, settings: () => ({ argv: [], every: 10 }), configure: true },
  { type: 'folder', category: 'personal', icon: 'files', hue: 'file', cols: 3, settings: () => ({ path: '', label: '' }), configure: true },
];

export const METRICS = ['cpu', 'memory', 'disk', 'network'] as const;
export type Metric = (typeof METRICS)[number];
export const METRIC_META: Record<Metric, { icon: IconName; hue: HueId }> = {
  cpu: { icon: 'cpu', hue: 'ov' },
  memory: { icon: 'mem', hue: 'log' },
  disk: { icon: 'disk', hue: 'term' },
  network: { icon: 'net', hue: 'file' },
};
export const asMetric = (m: unknown): Metric => ((METRICS as readonly string[]).includes(m as string) ? (m as Metric) : 'cpu');

export const RANGES = { '5m': 5 * 60_000, '15m': 15 * 60_000, '1h': 60 * 60_000 } as const;
export type Range = keyof typeof RANGES;
export const asRange = (r: unknown, d: Range = '1h'): Range => (r === '5m' || r === '15m' || r === '1h' ? r : d);
