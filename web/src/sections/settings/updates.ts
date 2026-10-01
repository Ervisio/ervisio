import { useSyncExternalStore } from 'react';
import { apiUrl, call, stream, ApiError, MOCK } from '../../api';

/* Self-update API (docs/api/updates.md). */

export interface Latest {
  version: string;
  tag: string;
  name?: string;
  publishedAt: number;
  /** Markdown; render with <ReleaseNotes>, never as HTML. */
  notes: string;
  url?: string;
  prerelease: boolean;
  asset: string;
  /** Archive size for this machine; 0 = no build for this architecture. */
  size: number;
}
export interface CheckResult {
  current: string;
  channel: 'stable' | 'prerelease';
  autoCheck: boolean;
  checkedAt: number;
  latest: Latest | null;
  newer: boolean;
  arch: string;
}
export interface LastResult {
  state: 'running' | 'ok' | 'failed' | 'rolled-back';
  kind: 'update' | 'rollback';
  from?: string;
  to: string;
  startedAt: number;
  finishedAt?: number;
  error?: string;
  auto?: boolean;
}
export interface UpdateStatus {
  current: string;
  install: 'versioned' | 'flat' | 'none';
  canUpdate: boolean;
  reason?: string;
  previous: string;
  installed: string[];
  last: LastResult | null;
  running: boolean;
  packageBusy: boolean;
  settings?: { channel: string; autoCheck: boolean; autoInstall: boolean; autoInstallAt: string };
  arch: string;
}
export interface ApplyEvent {
  phase: 'check' | 'download' | 'verify' | 'extract' | 'test' | 'install' | 'restart';
  file?: string;
  done?: number;
  total?: number;
  percent?: number;
  version?: string;
  unit?: string;
}
export interface Health {
  status: string;
  version: string;
  startedAt: number;
}

export const checkUpdates = (force = false) => call<CheckResult>('updates.check', { force });
export const updateStatus = () => call<UpdateStatus>('updates.status', {});

export async function health(): Promise<Health | null> {
  if (MOCK) return { status: 'ok', version: '1.0.0', startedAt: 1 };
  try {
    const r = await fetch(apiUrl('/api/health'), { cache: 'no-store', credentials: 'omit' });
    if (!r.ok) return null;
    return (await r.json()) as Health;
  } catch {
    return null;
  }
}

/* ---------- the running update, kept outside React ----------
 * Once the daemon restarts every session ends (they live in its memory), so the app may be sent to the sign-in
 * page while we wait. The watcher therefore lives at module level: whatever is mounted, the page reloads as soon
 * as the expected version answers on /api/health, so the browser never keeps running the old bundle.
 */

export type RunPhase = ApplyEvent['phase'] | 'waiting' | 'done' | 'rolled-back' | 'timeout' | 'error';
export interface RunState {
  kind: 'update' | 'rollback';
  target: string;
  from: string;
  phase: RunPhase;
  percent?: number;
  done?: number;
  total?: number;
  error?: string;
  /** Phases already reached, in order. */
  reached: RunPhase[];
}

let run: RunState | null = null;
const subs = new Set<() => void>();
const set = (patch: Partial<RunState> | null) => {
  if (patch === null) run = null;
  else if (run) {
    const reached = patch.phase && !run.reached.includes(patch.phase) ? [...run.reached, patch.phase] : run.reached;
    run = { ...run, ...patch, reached };
  }
  subs.forEach((s) => s());
};

export function useRun(): RunState | null {
  return useSyncExternalStore(
    (cb) => {
      subs.add(cb);
      return () => void subs.delete(cb);
    },
    () => run,
  );
}
export const clearRun = () => {
  if (run && ['done', 'rolled-back', 'timeout', 'error'].includes(run.phase)) set(null);
};

/** How long the page waits for the restarted daemon (the server-side rollback triggers after 30 s). */
const WAIT_MS = 3 * 60_000;

async function waitForRestart(target: string, from: string, before: Health | null) {
  set({ phase: 'waiting' });
  const t0 = Date.now();
  for (;;) {
    await new Promise((r) => setTimeout(r, 1500));
    const h = await health();
    if (h && h.version === target && (!before || h.startedAt !== before.startedAt || before.version !== target)) {
      set({ phase: 'done' });
      window.setTimeout(() => window.location.replace(window.location.pathname + window.location.hash), 800);
      return;
    }
    // The old version came back with a new start time: the new one failed and was rolled back.
    if (h && before && h.version === from && h.startedAt !== before.startedAt && Date.now() - t0 > 5000) {
      set({ phase: 'rolled-back' });
      return;
    }
    if (Date.now() - t0 > WAIT_MS) {
      set({ phase: 'timeout' });
      return;
    }
  }
}

/** Starts updates.apply (admin stream: the unlock dialog appears if needed). */
export async function startUpdate(target: string, from: string): Promise<void> {
  if (run && !['done', 'rolled-back', 'timeout', 'error'].includes(run.phase)) return;
  run = { kind: 'update', target, from, phase: 'check', reached: ['check'] };
  set({});
  const before = await health();
  let restarting = false;
  stream<ApplyEvent>('updates.apply', { version: target }, {
    admin: true,
    onData: (e) => {
      if (e.phase === 'restart') restarting = true;
      set({ phase: e.phase, percent: e.percent, done: e.done, total: e.total });
    },
    onEnd: () => {
      if (restarting) void waitForRestart(target, from, before);
      else set({ phase: 'error', error: 'The update ended without restarting.' });
    },
    onError: (err: ApiError) => {
      // After the hand-off the daemon restarts and the connection drops ("unavailable"): wait for it. Any other
      // error (e.g. systemd-run failed) means the switch never started.
      if (restarting && (err.code === 'unavailable' || err.code === 'unauthenticated')) void waitForRestart(target, from, before);
      else set({ phase: 'error', error: err.message });
    },
  });
}

/** Starts updates.rollback (admin call) and waits for the previous version. */
export async function startRollback(target: string, from: string): Promise<void> {
  if (run && !['done', 'rolled-back', 'timeout', 'error'].includes(run.phase)) return;
  const before = await health();
  run = { kind: 'rollback', target, from, phase: 'restart', reached: ['restart'] };
  set({});
  try {
    await call('updates.rollback', { version: target }, { admin: true });
  } catch (e) {
    set({ phase: 'error', error: e instanceof Error ? e.message : String(e) });
    throw e;
  }
  void waitForRestart(target, from, before);
}
