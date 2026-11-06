import { useSyncExternalStore } from 'react';
import { ApiError, call, stream } from '../../api';
import { toast } from '../../ui';
import { reloadAll } from './store';
import type { ServerTx, TxEvent, TxParams } from './types';

/** One software job (one or more sequential transactions). Lives at module level so it survives navigation. */
export interface Job {
  /** i18n key suffix and vars for the title, e.g. {kind: 'updateAll'} */
  title: { key: string; vars?: Record<string, string | number> };
  steps: TxParams[];
}

export interface TxState {
  phase: 'idle' | 'running' | 'done' | 'failed';
  title: { key: string; vars?: Record<string, string | number> } | null;
  op: string;
  step: number;
  steps: number;
  done: number;
  total: number;
  current: string;
  log: string[];
  message: string;
  hint: string;
  rebootNeeded: boolean;
  startedAt: number;
  /** progress of the whole job, 0-100, undefined = unknown */
  percent: number | undefined;
  /** another session or a page reload started it: we only watch */
  attached: boolean;
  panelOpen: boolean;
}

const idle: TxState = {
  phase: 'idle',
  title: null,
  op: '',
  step: 0,
  steps: 0,
  done: 0,
  total: 0,
  current: '',
  log: [],
  message: '',
  hint: '',
  rebootNeeded: false,
  startedAt: 0,
  percent: undefined,
  attached: false,
  panelOpen: false,
};
let state: TxState = idle;
const subs = new Set<() => void>();
const set = (p: Partial<TxState>) => {
  state = { ...state, ...p };
  subs.forEach((s) => s());
};
const subscribe = (cb: () => void) => (subs.add(cb), () => void subs.delete(cb));
export const useTx = () => useSyncExternalStore(subscribe, () => state);
export const getTx = () => state;
export const openPanel = () => set({ panelOpen: true });
export const closePanel = () => set({ panelOpen: false });
export const dismissTx = () => {
  if (state.phase === 'running') set({ panelOpen: false });
  else state = { ...idle };
  subs.forEach((s) => s());
};

/* i18n for toasts: the owner (page or badge hook) registers a translator. */
type T = (key: string, vars?: Record<string, string | number>) => string;
let translate: T = (k) => k;
let goto: (path: string) => void = (p) => window.location.assign(p);
export function registerRuntime(t: T, navigate: (path: string) => void) {
  translate = t;
  goto = navigate;
}

let toastId = 0;
let handle: { close(): void } | null = null;

const MAX_LOG = 1500;
const pct = (step: number, steps: number, done: number, total: number): number | undefined => {
  if (!steps) return undefined;
  const within = total > 0 ? Math.min(1, done / total) : 0;
  return Math.round(((step - 1 + within) / steps) * 100);
};

export const isRunning = () => state.phase === 'running';

export function startJob(job: Job): void {
  if (state.phase === 'running') {
    toast.info(translate('tx.busy'));
    return;
  }
  state = {
    ...idle,
    phase: 'running',
    title: job.title,
    steps: job.steps.length,
    step: 1,
    startedAt: Date.now(),
    panelOpen: true,
    op: job.steps[0]?.op ?? '',
    percent: 0,
  };
  subs.forEach((s) => s());
  toastId = toast.show({
    tone: 'run',
    title: translate(`tx.title.${job.title.key}`, job.title.vars),
    progress: 0,
    duration: 0,
    action: { label: translate('tx.show'), onClick: showPanel },
  });
  void runSteps(job);
}

function showPanel() {
  set({ panelOpen: true });
  goto('/software?tab=installed');
}

type StepResult = { ok: boolean; message: string; hint: string; reboot: boolean };

function pushLog(line: string) {
  const log =
    state.log.length >= MAX_LOG ? [...state.log.slice(-MAX_LOG + 1), line] : [...state.log, line];
  set({ log });
}

const TOAST_INTERVAL_MS = 400;

function runStep(p: TxParams, onToast: () => boolean): Promise<StepResult> {
  return new Promise<StepResult>((resolve) => {
    let finished = false;
    const end = (r: StepResult) => {
      if (finished) return;
      finished = true;
      handle = null;
      resolve(r);
    };
    const onProgress = (e: { done: number; total: number; current: string }) => {
      const percent = pct(state.step, state.steps, e.done, e.total);
      set({ done: e.done, total: e.total, current: e.current, percent });
      if (onToast()) toast.update(toastId, { progress: percent, detail: e.current });
    };
    const user = p.source === 'cli' || (p.source === 'flatpak' && p.scope === 'user');
    handle = stream<TxEvent>(user ? 'software.transactionUser' : 'software.transaction', p, {
      admin: !user,
      onData: (e) => {
        if (e.type === 'log') pushLog(e.line);
        else if (e.type === 'progress') onProgress(e);
        else if (e.type === 'done')
          end({ ok: e.ok, message: e.message, hint: e.hint, reboot: e.rebootNeeded });
      },
      onEnd: () => end({ ok: false, message: translate('tx.lost'), hint: '', reboot: false }),
      onError: (err: ApiError) =>
        end({
          ok: false,
          message: err.message,
          hint: err.code === 'conflict' ? 'locked' : '',
          reboot: false,
        }),
    });
  });
}

function failJob(result: StepResult) {
  set({ phase: 'failed', message: result.message, hint: result.hint, percent: undefined });
  toast.update(toastId, {
    tone: 'err',
    title: translate('tx.failed'),
    detail: result.message,
    progress: undefined,
    duration: 9000,
    action: { label: translate('tx.show'), onClick: showPanel },
  });
  void reloadAll();
}

function finishJob(job: Job) {
  set({ phase: 'done', percent: 100, done: state.total, message: '' });
  toast.update(toastId, {
    tone: 'ok',
    title: translate(`tx.done.${job.title.key}`, job.title.vars),
    detail: state.rebootNeeded ? translate('tx.rebootNeeded') : undefined,
    progress: undefined,
    duration: state.rebootNeeded ? 10000 : 5000,
    action: undefined,
  });
  void reloadAll();
}

async function runSteps(job: Job) {
  let lastToast = 0;
  const toastDue = () => {
    if (Date.now() - lastToast <= TOAST_INTERVAL_MS) return false;
    lastToast = Date.now();
    return true;
  };
  for (let i = 0; i < job.steps.length; i++) {
    const p = job.steps[i];
    set({ step: i + 1, done: 0, total: 0, current: '', op: p.op });
    const result = await runStep(p, toastDue);
    if (!result.ok) return failJob(result);
    if (result.reboot) set({ rebootNeeded: true });
  }
  finishJob(job);
}

/* ---- attach: a transaction is already running (page reloaded, other session) ---- */
let attachTimer = 0;
export async function attachIfBusy(): Promise<void> {
  if (state.phase === 'running' || attachTimer) return;
  try {
    const s = await call<{ busy: boolean; transaction: ServerTx | null }>('software.status');
    if (!s.busy || !s.transaction) return;
    mirror(s.transaction);
    attachTimer = window.setInterval(async () => {
      try {
        const r = await call<{ busy: boolean; transaction: ServerTx | null }>('software.status');
        if (r.transaction) mirror(r.transaction);
        if (!r.busy) {
          window.clearInterval(attachTimer);
          attachTimer = 0;
          void reloadAll();
        }
      } catch {
        /* keep trying */
      }
    }, 1500);
  } catch {
    /* status is optional */
  }
}

function mirror(t: ServerTx) {
  if (t.running) {
    set({
      phase: 'running',
      attached: true,
      op: t.op,
      step: t.step,
      steps: t.steps,
      done: t.done,
      total: t.total,
      current: t.current,
      log: t.log,
      startedAt: t.startedAt,
      percent: pct(t.step, t.steps, t.done, t.total),
      rebootNeeded: false,
      title: {
        key: t.op === 'upgrade' && t.packages.length === 0 ? 'updateAll' : t.op,
        vars: { name: t.packages.join(', ') },
      },
      panelOpen: state.panelOpen || state.phase !== 'running',
    });
  } else {
    set({
      phase: t.ok ? 'done' : 'failed',
      attached: true,
      log: t.log,
      message: t.message ?? '',
      hint: t.hint ?? '',
      percent: t.ok ? 100 : undefined,
      rebootNeeded: t.rebootNeeded,
      op: t.op,
      title: state.title ?? { key: t.op, vars: { name: t.packages.join(', ') } },
    });
  }
}

export const cancelWatch = () => handle?.close();
