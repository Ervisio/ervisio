import { useEffect, useSyncExternalStore } from 'react';
import { call, stream } from '../../api';
import type { StreamHandle } from '../../api';

/* ---------- types of system.* and overview.* results ---------- */
export interface Metrics {
  time: number;
  interval: number;
  cpu: { percent: number; cores: number[] };
  load: number[];
  memory: { total: number; used: number; available: number; percent: number };
  swap: { total: number; used: number; free: number; percent: number };
  disks: { mount: string; device: string; fstype: string; total: number; used: number; free: number; percent: number }[];
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

const BUCKET = 4000; // history resolution
const KEEP = 900; // one hour
let history: Point[] = [];
let bucket: { start: number; n: number; cpu: number; mem: number; net: number; disks: Record<string, number> } | null = null;

interface Snap {
  metrics: Metrics | null;
  history: Point[];
  error: string;
}
let snap: Snap = { metrics: null, history: [], error: '' };
const subs = new Set<() => void>();
let handle: StreamHandle | null = null;
let stopTimer: number | undefined;

export function physicalNet(m: Metrics) {
  const phys = m.net.filter((n) => !n.virtual);
  return phys.length ? phys : m.net;
}

function ingest(m: Metrics) {
  const t = m.time || Date.now();
  if (!bucket || t >= bucket.start + BUCKET) {
    if (bucket && bucket.n) history = [...history, avg(bucket)].slice(-KEEP);
    bucket = { start: t, n: 0, cpu: 0, mem: 0, net: 0, disks: {} };
  }
  bucket.n++;
  bucket.cpu += m.cpu.percent;
  bucket.mem += m.memory.percent;
  bucket.net += physicalNet(m).reduce((a, n) => a + n.rxRate + n.txRate, 0);
  for (const d of m.disks) bucket.disks[d.mount] = d.percent;
  snap = { metrics: m, history: [...history, avg(bucket)], error: '' };
  subs.forEach((s) => s());
}

function avg(b: NonNullable<typeof bucket>): Point {
  return { t: b.start + (BUCKET / 2), cpu: b.cpu / b.n, mem: b.mem / b.n, net: b.net / b.n, disks: { ...b.disks } };
}

function start() {
  if (handle) return;
  handle = stream<Metrics>('system.metricsStream', { interval: 2000 }, {
    reopen: true,
    onData: ingest,
    onError: (e) => {
      snap = { ...snap, error: e.message };
      subs.forEach((s) => s());
    },
  });
}

function subscribe(cb: () => void) {
  subs.add(cb);
  window.clearTimeout(stopTimer);
  start();
  return () => {
    subs.delete(cb);
    if (!subs.size) {
      // keep the stream for a moment so switching sections does not reconnect
      stopTimer = window.setTimeout(() => {
        if (!subs.size) {
          handle?.close();
          handle = null;
        }
      }, 5000);
    }
  };
}

/** Latest metrics and the last hour of history (kept in memory while the app is open). */
export function useMetrics(): Snap {
  return useSyncExternalStore(subscribe, () => snap);
}

/* ---------- small cached fetchers ---------- */
let hostCache: Host | null = null;
let hostPromise: Promise<void> | null = null;
const hostSubs = new Set<() => void>();

export function useHost(): Host | null {
  const h = useSyncExternalStore(
    (cb) => (hostSubs.add(cb), () => void hostSubs.delete(cb)),
    () => hostCache,
  );
  useEffect(() => {
    if (hostCache || hostPromise) return;
    hostPromise = call<Host>('system.host')
      .then((r) => {
        hostCache = r;
        hostSubs.forEach((s) => s());
      })
      .catch(() => undefined)
      .finally(() => {
        hostPromise = null;
      });
  }, []);
  return h;
}

interface AlertSnap {
  alerts: AlertItem[] | null;
  error: string;
  at: number;
}
let alertSnap: AlertSnap = { alerts: null, error: '', at: 0 };
let alertBusy = false;
const alertSubs = new Set<() => void>();

export async function refreshAlerts() {
  if (alertBusy) return;
  alertBusy = true;
  try {
    const r = await call<AlertItem[]>('overview.alerts');
    alertSnap = { alerts: Array.isArray(r) ? r : [], error: '', at: Date.now() };
  } catch (e) {
    alertSnap = { ...alertSnap, error: e instanceof Error ? e.message : String(e), at: Date.now() };
  } finally {
    alertBusy = false;
    alertSubs.forEach((s) => s());
  }
}

/** Alerts, refreshed every minute while something shows them (the daemon caches the slow checks). */
export function useAlerts() {
  const s = useSyncExternalStore(
    (cb) => (alertSubs.add(cb), () => void alertSubs.delete(cb)),
    () => alertSnap,
  );
  useEffect(() => {
    if (Date.now() - alertSnap.at > 20_000) void refreshAlerts();
    const id = window.setInterval(() => void refreshAlerts(), 60_000);
    return () => window.clearInterval(id);
  }, []);
  return { ...s, refresh: refreshAlerts };
}
