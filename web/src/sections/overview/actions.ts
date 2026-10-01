import { useSyncExternalStore } from 'react';
import { ApiError, call } from '../../api';
import type { TFn } from '../../i18n';
import { toast } from '../../ui';
import type { DashAction } from './model';

export interface ActionResult {
  ok: boolean;
  exitCode: number;
  output: string;
  truncated?: boolean;
  timedOut?: boolean;
  durationMs: number;
}

export const took = (ms: number) => (ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`);

export interface OutputView {
  label: string;
  command: string;
  admin: boolean;
  result?: ActionResult;
  error?: string;
}

let current: OutputView | null = null;
const subs = new Set<() => void>();
const emit = () => subs.forEach((s) => s());

export function showOutput(v: OutputView | null) {
  current = v;
  emit();
}
export function useOutputView(): OutputView | null {
  return useSyncExternalStore(
    (cb) => (subs.add(cb), () => void subs.delete(cb)),
    () => current,
  );
}

const running = new Set<string>();
const runSubs = new Set<() => void>();
let runSnap: ReadonlySet<string> = new Set();
export function useRunning(): ReadonlySet<string> {
  return useSyncExternalStore(
    (cb) => (runSubs.add(cb), () => void runSubs.delete(cb)),
    () => runSnap,
  );
}
function setRunning(id: string, on: boolean) {
  if (on) running.add(id);
  else running.delete(id);
  runSnap = new Set(running);
  runSubs.forEach((s) => s());
}

/**
 * Runs a user action through overview.actionRun and tells the user how it went.
 * `onShow` opens the output; the default shows the dialog hosted by the Overview page.
 */
export async function runAction(a: DashAction, t: TFn, onShow: (v: OutputView) => void = showOutput): Promise<void> {
  if (running.has(a.id)) return;
  const base = { label: a.label, command: a.argv.join(' '), admin: !!a.admin };
  setRunning(a.id, true);
  try {
    const r = await call<ActionResult>('overview.actionRun', { argv: a.argv, timeout: 60 }, { admin: !!a.admin });
    const view: OutputView = { ...base, result: r };
    const show = { label: t('action.showOutput'), onClick: () => onShow(view) };
    if (r.ok) {
      toast.show({ title: t('action.done', { name: a.label }), detail: t('action.took', { time: took(r.durationMs) }), tone: 'ok', action: show });
    } else if (r.timedOut) {
      toast.show({ title: t('action.timedOut', { name: a.label }), tone: 'err', action: show });
    } else {
      toast.show({ title: t('action.failed', { name: a.label }), detail: t('action.exit', { code: r.exitCode }), tone: 'err', action: show });
    }
  } catch (e) {
    if (e instanceof ApiError && e.code === 'cancelled') return;
    const msg = e instanceof Error ? e.message : String(e);
    const view: OutputView = { ...base, error: msg };
    toast.show({ title: t('action.couldNotRun', { name: a.label }), detail: msg, tone: 'err', action: { label: t('action.showOutput'), onClick: () => onShow(view) } });
  } finally {
    setRunning(a.id, false);
  }
}

/* An action that wants confirmation, requested from outside the page (command palette). */
let pendingConfirm: DashAction | null = null;
const confirmSubs = new Set<() => void>();
export function requestConfirm(a: DashAction | null) {
  pendingConfirm = a;
  confirmSubs.forEach((s) => s());
}
export function useConfirmRequest(): DashAction | null {
  return useSyncExternalStore(
    (cb) => (confirmSubs.add(cb), () => void confirmSubs.delete(cb)),
    () => pendingConfirm,
  );
}
