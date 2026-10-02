/**
 * Alert engine. It runs inside the page (or the Overview widget) while Ervisio is open in a browser; there is no
 * background service. Rules live in the `alerts` file, the history in `alerts-history` (200 entries at most).
 *
 *   useAlertEngine();                 // in the shell and the widget: starts once per frame, stops with the last user
 *   const items = useAlertHistory();  // recent alerts, newest first
 *   fireTestAlert();                  // sample toast
 *
 * Sources: the shared /events stream (die, kill, oom, health_status) and a slow sampling loop (stats of running
 * containers every 15 s when a cpu or memory rule is on, /system/df every 5 minutes when a disk rule is on).
 */
import { useEffect, useSyncExternalStore } from 'react';
import { t } from '../i18n';
import { toast } from '../kit';
import { ensureFile, loadFile, setFile, subscribeFile, type AlertHistoryItem, type AlertRule } from '../settings';
import { docker } from './engine';
import { subscribeEvents } from './events';
import { containerName, formatBytes } from './format';
import { cpuPercent, memoryUsed } from './stats';
import { summarizeDf } from './system';
import type { Container, DockerEvent, StatsRaw, SystemInfo } from './types';

const HISTORY_MAX = 200;
const SAMPLE_MS = 15000;
const DISK_MS = 5 * 60000;
const COMPOSE_PROJECT = 'com.docker.compose.project';
const GB = 1024 ** 3;

/* ---------- state shared with the UI ---------- */

let history: AlertHistoryItem[] = [];
let status = { running: false, lastSample: 0 };
const subs = new Set<() => void>();
const emit = () => subs.forEach((f) => f());
const subscribe = (f: () => void) => {
  subs.add(f);
  return () => subs.delete(f);
};

/** Recent alerts, newest first. */
export const useAlertHistory = (): AlertHistoryItem[] => useSyncExternalStore(subscribe, () => history);
/** Whether the engine runs in this frame, and when it last sampled. */
export const useAlertStatus = (): { running: boolean; lastSample: number } => useSyncExternalStore(subscribe, () => status);

function setStatus(patch: Partial<typeof status>): void {
  status = { ...status, ...patch };
  emit();
}

/* ---------- rules ---------- */

let rules: AlertRule[] = [];
const enabled = (kind: AlertRule['kind']) => rules.filter((r) => r.enabled && r.kind === kind);

function covers(r: AlertRule, name: string, stack: string): boolean {
  if (r.scope === 'stack') return !!r.target && r.target === stack;
  if (r.scope === 'container') return !!r.target && r.target === name;
  if (!r.scope && r.containers?.length) return r.containers.includes(name);
  return true;
}

/* ---------- firing ---------- */

const cooldown = new Map<string, number>();
function cooled(key: string, ms: number): boolean {
  const now = Date.now();
  if ((cooldown.get(key) ?? 0) > now) return false;
  cooldown.set(key, now + ms);
  return true;
}

let persistChain: Promise<void> = Promise.resolve();
function persist(): void {
  persistChain = persistChain
    .then(async () => {
      // Another frame may have written too: merge with what is on disk.
      const disk = (await loadFile('alerts-history')).items ?? [];
      const seen = new Set(history.map((h) => h.id));
      const merged = [...history, ...disk.filter((d) => !seen.has(d.id))].sort((a, b) => b.at - a.at).slice(0, HISTORY_MAX);
      history = merged;
      emit();
      await setFile('alerts-history', { items: merged });
    })
    .catch(() => undefined);
}

function fire(rule: AlertRule, title: string, detail: string, c?: { name: string; id: string }): void {
  const item: AlertHistoryItem = {
    id: `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
    at: Date.now(),
    ruleId: rule.id,
    kind: rule.kind,
    title,
    detail,
    container: c?.name,
    containerId: c?.id,
  };
  history = [item, ...history].slice(0, HISTORY_MAX);
  emit();
  persist();
  try {
    toast.err(title, detail);
  } catch {
    /* the page may be closing */
  }
}

/** A sample toast, not stored in the history. */
export function fireTestAlert(): void {
  toast.info(t('alerts.test.title'), t('alerts.test.detail'));
}

export async function clearAlertHistory(): Promise<void> {
  history = [];
  emit();
  await setFile('alerts-history', { items: [] });
}

/* ---------- events ---------- */

const lastKill = new Map<string, number>();
const lastOom = new Map<string, number>();
const dies = new Map<string, number[]>();

function onEvent(ev: DockerEvent): void {
  if (ev.Type !== 'container' || !ev.Actor) return;
  const id = ev.Actor.ID;
  const a = ev.Actor.Attributes ?? {};
  const name = a.name ?? id.slice(0, 12);
  const stack = a[COMPOSE_PROJECT] ?? '';
  const now = Date.now();
  const action = ev.Action.split(':')[0];

  if (action === 'destroy') {
    [lastKill, lastOom, dies].forEach((m) => m.delete(id));
    for (const k of [...states.keys()]) if (k.endsWith(`:${id}`)) states.delete(k);
    return;
  }
  if (action === 'kill') {
    lastKill.set(id, now);
    return;
  }
  if (action === 'oom') {
    lastOom.set(id, now);
    return;
  }
  if (action === 'health_status') {
    if (!ev.Action.includes('unhealthy')) return;
    for (const r of enabled('unhealthy')) {
      if (covers(r, name, stack) && cooled(`${r.id}:${id}`, 10 * 60000)) fire(r, t('alerts.fire.unhealthy', { name }), t('alerts.fire.unhealthy.d'), { name, id });
    }
    return;
  }
  if (action !== 'die') return;

  // `docker stop` and `docker kill` send a signal first: those exits were asked for.
  const asked = now - (lastKill.get(id) ?? 0) < 5000;
  const code = Number(a.exitCode ?? 0);
  const oom = now - (lastOom.get(id) ?? 0) < 5000;
  if (asked) return;

  const list = (dies.get(id) ?? []).filter((x) => now - x < 24 * 3600000);
  list.push(now);
  dies.set(id, list);

  if (code !== 0 || oom) {
    for (const r of enabled('stopped')) {
      if (covers(r, name, stack) && cooled(`${r.id}:${id}`, 60000)) {
        fire(r, t('alerts.fire.stopped', { name }), oom ? t('alerts.fire.stopped.oom', { code }) : t('alerts.fire.stopped.d', { code }), { name, id });
      }
    }
  }
  for (const r of enabled('restart-loop')) {
    if (!covers(r, name, stack)) continue;
    const n = r.count ?? 3;
    const m = r.minutes ?? 5;
    const recent = list.filter((x) => now - x < m * 60000).length;
    if (recent >= n && cooled(`${r.id}:${id}`, m * 60000)) fire(r, t('alerts.fire.loop', { name }), t('alerts.fire.loop.d', { n: recent, m }), { name, id });
  }
}

/* ---------- sampling ---------- */

/** Per rule and container: when the value went above the limit, and whether this episode already alerted. */
const states = new Map<string, { since: number; fired: boolean }>();

function sustained(key: string, above: boolean, minutes: number): boolean {
  const now = Date.now();
  if (!above) {
    states.delete(key);
    return false;
  }
  let s = states.get(key);
  if (!s) {
    s = { since: now, fired: false };
    states.set(key, s);
  }
  if (s.fired || now - s.since < minutes * 60000) return false;
  s.fired = true;
  return true;
}

let hostMem = 0;
async function hostMemory(): Promise<number> {
  if (!hostMem) hostMem = (await docker.get<SystemInfo>('/info')).MemTotal || 0;
  return hostMem;
}

async function sampleContainers(): Promise<void> {
  const cpuRules = enabled('cpu');
  const memRules = enabled('memory');
  if (!cpuRules.length && !memRules.length) return;
  const list = (await docker.get<Container[]>('/containers/json')).filter((c) => c.State === 'running');
  const work = list.filter((c) => {
    const n = containerName(c);
    const s = c.Labels?.[COMPOSE_PROJECT] ?? '';
    return [...cpuRules, ...memRules].some((r) => covers(r, n, s));
  });
  const live = new Set(list.map((c) => c.Id));
  for (const k of [...states.keys()]) if (/^(cpu|mem)/.test(k) && !live.has(k.split(':').pop()!)) states.delete(k);

  let i = 0;
  const worker = async () => {
    while (i < work.length) {
      const c = work[i++];
      const name = containerName(c);
      const stack = c.Labels?.[COMPOSE_PROJECT] ?? '';
      let raw: StatsRaw;
      try {
        raw = await docker.get<StatsRaw>(`/containers/${c.Id}/stats`, { stream: 'false' });
      } catch {
        continue;
      }
      const cpu = cpuPercent(undefined, raw);
      for (const r of cpuRules) {
        if (!covers(r, name, stack) || cpu === null) continue;
        const limit = r.threshold ?? 90;
        if (sustained(`cpu${r.id}:${c.Id}`, cpu >= limit, r.minutes ?? 5)) {
          fire(r, t('alerts.fire.cpu', { name }), t('alerts.fire.cpu.d', { pct: Math.round(cpu), limit, m: r.minutes ?? 5 }), { name, id: c.Id });
        }
      }
      const used = memoryUsed(raw.memory_stats);
      for (const r of memRules) {
        if (!covers(r, name, stack)) continue;
        const base = r.memBasis === 'host' ? await hostMemory().catch(() => 0) : raw.memory_stats.limit ?? 0;
        if (!base) continue;
        const pct = (used / base) * 100;
        const limit = r.threshold ?? 90;
        if (sustained(`mem${r.id}:${c.Id}`, pct >= limit, r.minutes ?? 5)) {
          fire(r, t('alerts.fire.mem', { name }), t('alerts.fire.mem.d', { pct: Math.round(pct), limit, size: formatBytes(used) }), { name, id: c.Id });
        }
      }
    }
  };
  await Promise.all([worker(), worker(), worker(), worker()]);
}

let lastDisk = 0;
async function sampleDisk(): Promise<void> {
  const rs = enabled('disk');
  if (!rs.length || Date.now() - lastDisk < DISK_MS) return;
  lastDisk = Date.now();
  const total = summarizeDf(await docker.get('/system/df')).reduce((a, p) => a + p.total, 0);
  for (const r of rs) {
    const limit = r.threshold ?? 20;
    if (sustained(`disk${r.id}:-`, total >= limit * GB, 0)) fire(r, t('alerts.fire.disk'), t('alerts.fire.disk.d', { size: formatBytes(total), limit }));
  }
}

/* ---------- start and stop ---------- */

let users = 0;
let stop: (() => void) | undefined;

function begin(): () => void {
  let alive = true;
  let busy = false;
  const readRules = () => {
    rules = (loaded?.rules ?? []).slice();
  };
  let loaded: { rules: AlertRule[] } | undefined;
  const un1 = subscribeFile('alerts', () => {
    void ensureFile('alerts').then((f) => {
      loaded = f;
      readRules();
    });
  });
  void ensureFile('alerts').then((f) => {
    loaded = f;
    readRules();
  });
  void ensureFile('alerts-history').then((f) => {
    const seen = new Set(history.map((h) => h.id));
    history = [...history, ...f.items.filter((i) => !seen.has(i.id))].sort((a, b) => b.at - a.at).slice(0, HISTORY_MAX);
    emit();
  });
  const un2 = subscribeEvents(onEvent);
  const tick = async () => {
    if (!alive || busy) return;
    busy = true;
    try {
      await sampleContainers();
      await sampleDisk();
    } catch {
      /* Docker unreachable for now: try again at the next tick */
    } finally {
      busy = false;
      if (alive) setStatus({ lastSample: Date.now() });
    }
  };
  const timer = setInterval(() => void tick(), SAMPLE_MS);
  const first = setTimeout(() => void tick(), 3000);
  setStatus({ running: true });
  return () => {
    alive = false;
    clearInterval(timer);
    clearTimeout(first);
    un1();
    un2();
    setStatus({ running: false });
  };
}

/** Starts the engine (once per frame, however many callers) and returns the function that releases it. */
export function startAlerts(): () => void {
  if (users++ === 0) stop = begin();
  let done = false;
  return () => {
    if (done) return;
    done = true;
    if (--users === 0) {
      stop?.();
      stop = undefined;
    }
  };
}

/** Call from the shell and from the widget. */
export function useAlertEngine(): void {
  useEffect(() => startAlerts(), []);
}
