import { useCallback, useEffect } from 'react';
import { call, stream, errorMessage } from '../../api';
import type { StreamHandle } from '../../api';
import { onRefreshNow, useRefreshInterval } from '../../lib/refresh';
import { useActivityExternalStore, useWindowActivity } from '../../lib/windowActivity';
import { useAccountScope } from '../../lib/accountScope';

/* ---------- types of system.* and overview.* results ---------- */
export interface Metrics {
  time: number;
  interval: number;
  cpu: { percent: number; cores: number[] };
  /** Absent or empty on Windows (no load average). */
  load?: number[] | null;
  memory: { total: number; used: number; available: number; percent: number };
  swap: { total: number; used: number; free: number; percent: number };
  disks: {
    mount: string;
    device: string;
    fstype: string;
    total: number;
    used: number;
    free: number;
    percent: number;
  }[];
  net: { iface: string; rxRate: number; txRate: number; virtual: boolean; up: boolean }[];
}

export interface Host {
  hostname: string;
  distro: { id: string; name: string; prettyName: string; version?: string; color?: string };
  kernel: string;
  arch: string;
  cpu: { model: string; cores: number; threads: number };
  memoryTotal: number;
  uptime: number;
  bootTime: number;
  ip?: string;
  machine?: { vendor?: string; product?: string };
}

export interface AlertItem {
  id: string;
  severity: 'ok' | 'warn' | 'err';
  titleKey?: string;
  title: string;
  vars?: Record<string, string | number>;
  detail?: string;
  action?: { section: string; params?: Record<string, string> };
}

/* ---------- live metrics + client-side history ---------- */
export interface Point {
  t: number;
  cpu: number;
  mem: number;
  net: number; // bytes/s, physical interfaces, rx + tx
  disks: Record<string, number>;
}

const MIN_BUCKET = 4000; // history resolution (ms); slower refresh rates get one point per sample
const KEEP_MS = 60 * 60_000; // history is buffered by time, so any refresh rate keeps one hour
let bucketMs = MIN_BUCKET;
let history: Point[] = [];
let bucket: {
  start: number;
  n: number;
  cpu: number;
  mem: number;
  net: number;
  disks: Record<string, number>;
} | null = null;

interface Snap {
  scope: string | null;
  metrics: Metrics | null;
  history: Point[];
  error: string;
}
const emptySnap: Snap = { scope: null, metrics: null, history: [], error: '' };
let snap: Snap = emptySnap;
let currentScope: string | null = null;
let generation = 0;
let streamGeneration = 0;
const subs = new Set<() => void>();
let handle: StreamHandle | null = null;
let streamMs = 2000; // interval wanted by the user
let handleMs = 0; // interval the open stream was started with

export function physicalNet(m: Metrics) {
  const phys = m.net.filter((n) => !n.virtual);
  return phys.length ? phys : m.net;
}

function ingest(m: Metrics) {
  const t = m.time || Date.now();
  if (!bucket || t >= bucket.start + bucketMs) {
    if (bucket && bucket.n) history = trim([...history, avg(bucket)], t);
    bucket = { start: t, n: 0, cpu: 0, mem: 0, net: 0, disks: {} };
  }
  bucket.n++;
  bucket.cpu += m.cpu.percent;
  bucket.mem += m.memory.percent;
  bucket.net += physicalNet(m).reduce((a, n) => a + n.rxRate + n.txRate, 0);
  for (const d of m.disks) bucket.disks[d.mount] = d.percent;
  snap = { scope: currentScope, metrics: m, history: [...history, avg(bucket)], error: '' };
  subs.forEach((s) => s());
}

function trim(h: Point[], now: number): Point[] {
  const from = now - KEEP_MS;
  let i = 0;
  while (i < h.length && h[i].t < from) i++;
  return i ? h.slice(i) : h;
}

function avg(b: NonNullable<typeof bucket>): Point {
  return {
    t: b.start + Math.min(bucketMs, streamMs) / 2,
    cpu: b.cpu / b.n,
    mem: b.mem / b.n,
    net: b.net / b.n,
    disks: { ...b.disks },
  };
}

function start() {
  if (handle || !currentScope) return;
  const owner = currentScope;
  const started = ++streamGeneration;
  handleMs = streamMs;
  bucketMs = Math.max(MIN_BUCKET, streamMs);
  handle = stream<Metrics>(
    'system.metricsStream',
    { interval: streamMs },
    {
      reopen: true,
      onData: (metrics) => {
        if (owner === currentScope && started === streamGeneration) ingest(metrics);
      },
      onError: (e) => {
        if (owner !== currentScope || started !== streamGeneration) return;
        snap = { ...snap, error: e.message };
        subs.forEach((s) => s());
      },
    },
  );
}

function restart() {
  if (!handle) return;
  handle.close();
  handle = null;
  streamGeneration++;
  start();
}

// "Refresh now": a fresh sample at once (reopening the stream sends the first one immediately) and fresh alerts.
onRefreshNow(() => {
  if (handle) restart();
  if (alertSubs.size) void refreshAlerts();
});

function subscribe(scope: string | null, cb: () => void) {
  selectScope(scope);
  subs.add(cb);
  start();
  return () => {
    subs.delete(cb);
    if (!subs.size) {
      handle?.close();
      handle = null;
      streamGeneration++;
    }
  };
}

/** Latest metrics and the last hour of history (kept in memory while the app is open). */
export function useMetrics(): Snap {
  const scope = useAccountScope();
  const active = useWindowActivity();
  const ms = useRefreshInterval();
  streamMs = ms;
  useEffect(() => {
    if (active && scope === currentScope && handle && handleMs !== ms) restart();
  }, [ms, active, scope]);
  const listen = useCallback((cb: () => void) => subscribe(scope, cb), [scope]);
  const result = useActivityExternalStore(listen, () => snap);
  // A minimized consumer retains its rendered snapshot. Never expose that
  // frozen account's data after SessionProvider changes the authenticated user.
  return result.scope === scope ? result : emptySnap;
}

/* ---------- small cached fetchers ---------- */
let hostSnap: { scope: string | null; host: Host | null } = { scope: null, host: null };
let hostPromise: Promise<void> | null = null;
const hostSubs = new Set<() => void>();

export function useHost(): Host | null {
  const scope = useAccountScope();
  const active = useWindowActivity();
  const listen = useCallback(
    (cb: () => void) => {
      selectScope(scope);
      hostSubs.add(cb);
      return () => {
        hostSubs.delete(cb);
      };
    },
    [scope],
  );
  const h = useActivityExternalStore(listen, () => hostSnap);
  useEffect(() => {
    if (!scope || !active) return;
    selectScope(scope);
    if (hostSnap.host || hostPromise) return;
    const started = generation;
    hostPromise = call<Host>('system.host')
      .then((r) => {
        if (scope !== currentScope || started !== generation) return;
        hostSnap = { scope, host: r };
        hostSubs.forEach((s) => s());
      })
      .catch(() => undefined)
      .finally(() => {
        if (scope === currentScope && started === generation) hostPromise = null;
      });
  }, [active, scope]);
  return h.scope === scope ? h.host : null;
}

interface AlertSnap {
  scope: string | null;
  alerts: AlertItem[] | null;
  error: string;
  at: number;
}
const emptyAlerts: AlertSnap = { scope: null, alerts: null, error: '', at: 0 };
let alertSnap: AlertSnap = emptyAlerts;
let alertBusy = false;
const alertSubs = new Set<() => void>();

export async function refreshAlerts() {
  if (alertBusy || !currentScope) return;
  const owner = currentScope;
  const started = generation;
  alertBusy = true;
  try {
    const r = await call<AlertItem[]>('overview.alerts');
    if (owner !== currentScope || started !== generation) return;
    alertSnap = { scope: owner, alerts: Array.isArray(r) ? r : [], error: '', at: Date.now() };
  } catch (e) {
    if (owner !== currentScope || started !== generation) return;
    alertSnap = { ...alertSnap, error: errorMessage(e), at: Date.now() };
  } finally {
    if (owner === currentScope && started === generation) {
      alertBusy = false;
      alertSubs.forEach((s) => s());
    }
  }
}

/** Alerts, refreshed at the user's rate (at least every 30 s) while something shows them (the daemon caches the slow checks). */
export function useAlerts() {
  const scope = useAccountScope();
  const active = useWindowActivity();
  const listen = useCallback(
    (cb: () => void) => {
      selectScope(scope);
      alertSubs.add(cb);
      return () => {
        alertSubs.delete(cb);
      };
    },
    [scope],
  );
  const s = useActivityExternalStore(listen, () => alertSnap);
  const every = Math.max(useRefreshInterval(), 30_000);
  useEffect(() => {
    if (!scope || !active) return;
    selectScope(scope);
    if (Date.now() - alertSnap.at > 20_000) void refreshAlerts();
    const id = window.setInterval(() => !document.hidden && void refreshAlerts(), every);
    return () => window.clearInterval(id);
  }, [every, active, scope]);
  return { ...(s.scope === scope ? s : emptyAlerts), refresh: refreshAlerts };
}

/** One shared connection is enough for the current account. Switching identity
 * drops all telemetry/history and invalidates late RPC/stream callbacks. */
function selectScope(scope: string | null) {
  if (scope === currentScope) return;
  currentScope = scope;
  generation++;
  streamGeneration++;
  handle?.close();
  handle = null;
  handleMs = 0;
  history = [];
  bucket = null;
  snap = { ...emptySnap, scope };
  hostSnap = { scope, host: null };
  hostPromise = null;
  alertSnap = { ...emptyAlerts, scope };
  alertBusy = false;
  for (const notify of [...subs, ...hostSubs, ...alertSubs]) notify();
}
