import { useSyncExternalStore } from 'react';
import type { LineHandler } from '../../api/compose';

/** Output of the last command run on each stack. It lives in memory, so it survives navigating away and back. */
export interface LogLine {
  stream: 'stdout' | 'stderr';
  text: string;
}
export interface DeployState {
  title: string;
  lines: LogLine[];
  running: boolean;
  /** Exit code once finished; null while running; -1 when it could not run. */
  code: number | null;
  at: number;
}

const states = new Map<string, DeployState>();
const subs = new Set<() => void>();
const emit = () => subs.forEach((f) => f());
const MAX_LINES = 3000;

export function getDeploy(name: string): DeployState | undefined {
  return states.get(name);
}

export function useDeploy(name: string): DeployState | undefined {
  return useSyncExternalStore(
    (f) => {
      subs.add(f);
      return () => subs.delete(f);
    },
    () => states.get(name),
  );
}

/** Run something that streams lines and returns an exit code; collect the lines under `name`. */
export async function logged(name: string, title: string, run: (onLine: LineHandler) => Promise<number>): Promise<number> {
  let st: DeployState = { title, lines: [], running: true, code: null, at: Date.now() };
  states.set(name, st);
  emit();
  let pending: LogLine[] = [];
  let timer: ReturnType<typeof setTimeout> | undefined;
  const flush = () => {
    timer = undefined;
    if (!pending.length) return;
    const lines = st.lines.concat(pending);
    pending = [];
    st = { ...st, lines: lines.length > MAX_LINES ? lines.slice(-MAX_LINES) : lines };
    states.set(name, st);
    emit();
  };
  const onLine: LineHandler = (stream, text) => {
    pending.push({ stream, text: text.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '') });
    if (!timer) timer = setTimeout(flush, 80);
  };
  let code: number;
  try {
    code = await run(onLine);
  } catch (e) {
    onLine('stderr', (e as Error).message || String(e));
    code = -1;
  }
  if (timer) clearTimeout(timer);
  flush();
  st = { ...st, running: false, code, at: Date.now() };
  states.set(name, st);
  emit();
  return code;
}

/** Colour class of an output line. */
export function lineTone(l: LogLine): 'dim' | 'ok' | 'warn' | 'err' | '' {
  if (l.text.startsWith('$ ')) return 'dim';
  if (/\b(error|failed|denied|invalid|cannot|no such|not found)\b/i.test(l.text)) return 'err';
  if (/\b(Running|Healthy|Started|Recreated|Created|Pulled|Done|Stopped|Removed|up to date|Exited)\b\s*$/i.test(l.text.trim())) return 'ok';
  if (/\b(Recreate|Pulling|Creating|Starting|Stopping|Waiting|Restarting|Removing|Building)\b\s*$/i.test(l.text.trim())) return 'warn';
  return l.stream === 'stderr' ? '' : '';
}
