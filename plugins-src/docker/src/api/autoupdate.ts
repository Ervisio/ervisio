/**
 * Auto-update through Watchtower. Applying the settings removes and creates one container, `linuxadmin-watchtower`,
 * that has the Docker socket mounted. "Check now" runs a second, short-lived one (`linuxadmin-watchtower-once`).
 *
 * Containers cannot get new labels once they exist, so "only the containers I choose" is done the other way round:
 * the chosen names are given to Watchtower as arguments, and Watchtower is recreated when the list changes.
 */
import type { AutoUpdateConfig } from '../settings';
import { docker, DockerError } from './engine';
import { LogLines } from './streams';
import type { Container, ContainerInspect } from './types';

export const WATCHTOWER = 'linuxadmin-watchtower';
export const WATCHTOWER_ONCE = 'linuxadmin-watchtower-once';
export const CONFIG_LABEL = 'la.autoupdate';
export const DEFAULT_IMAGE = 'nickfedor/watchtower:latest';

export const withTag = (image: string): string => {
  const i = image.trim() || DEFAULT_IMAGE;
  return /^[^/]*(\/.*)?:[^/:]+$/.test(i) || i.includes('@') ? i : `${i}:latest`;
};

/** Six fields, as Watchtower reads them: second minute hour day-of-month month day-of-week. */
export function cronOf(c: AutoUpdateConfig): string {
  const { type, hour, minute, day, cron } = c.schedule;
  if (type === 'custom') return cron.trim();
  return type === 'daily' ? `0 ${minute} ${hour} * * *` : `0 ${minute} ${hour} * * ${day}`;
}

/** A custom cron must have six fields made of cron characters. */
export const validCron = (s: string): boolean => /^\s*\S+(\s+\S+){5}\s*$/.test(s) && /^[\d*/,\-?A-Za-z\s]+$/.test(s);

export function envOf(c: AutoUpdateConfig, once: boolean): string[] {
  const env = ['WATCHTOWER_LOG_FORMAT=LogFmt', 'WATCHTOWER_NO_STARTUP_MESSAGE=true'];
  if (once) env.push('WATCHTOWER_RUN_ONCE=true');
  else env.push(`WATCHTOWER_SCHEDULE=${cronOf(c)}`);
  if (c.cleanup) env.push('WATCHTOWER_CLEANUP=true');
  if (c.includeStopped) env.push('WATCHTOWER_INCLUDE_STOPPED=true');
  if (c.monitorOnly) env.push('WATCHTOWER_MONITOR_ONLY=true');
  if (c.rolling) env.push('WATCHTOWER_ROLLING_RESTART=true');
  return env;
}

function body(c: AutoUpdateConfig, once: boolean) {
  return {
    Image: withTag(c.image),
    Cmd: c.mode === 'choose' ? c.containers : [],
    Env: envOf(c, once),
    Labels: once ? { 'la.autoupdate.once': 'true' } : { [CONFIG_LABEL]: JSON.stringify(c) },
    HostConfig: {
      Binds: ['/var/run/docker.sock:/var/run/docker.sock', '/etc/localtime:/etc/localtime:ro'],
      RestartPolicy: { Name: once ? 'no' : 'unless-stopped' },
    },
  };
}

/** Pulls the image when it is not on this machine yet. */
export async function ensureImage(image: string): Promise<void> {
  const ref = withTag(image);
  try {
    await docker.get(`/images/${ref}/json`);
    return;
  } catch (e) {
    if (!(e instanceof DockerError) || e.status !== 404) throw e;
  }
  await new Promise<void>((resolve, reject) => {
    let text = '';
    docker.stream('POST', '/images/create', { query: { fromImage: ref } }, {
      onData: (c) => (text += new TextDecoder().decode(c)),
      onEnd: () => {
        const bad = text.split('\n').map((l) => { try { return JSON.parse(l).error as string | undefined; } catch { return undefined; } }).find(Boolean);
        bad ? reject(new Error(bad)) : resolve();
      },
      onError: reject,
    });
  });
}

async function removeIfExists(name: string): Promise<void> {
  try {
    await docker.delete(`/containers/${name}`, { force: '1' });
  } catch (e) {
    if (!(e instanceof DockerError) || e.status !== 404) throw e;
  }
}

export type ApplyStep = 'pull' | 'remove' | 'create' | 'start';

/** Makes Watchtower match the settings: removed when off, recreated and started when on. */
export async function applyAutoUpdate(c: AutoUpdateConfig, onStep?: (s: ApplyStep) => void): Promise<void> {
  onStep?.('remove');
  if (!c.enabled) {
    await removeIfExists(WATCHTOWER);
    return;
  }
  onStep?.('pull');
  await ensureImage(c.image);
  onStep?.('remove');
  await removeIfExists(WATCHTOWER);
  onStep?.('create');
  await docker.post('/containers/create', { name: WATCHTOWER }, body(c, false));
  onStep?.('start');
  await docker.post(`/containers/${WATCHTOWER}/start`);
}

/** The Watchtower container from the list, and the settings it was created with. */
export function findWatchtower(list: Container[] | undefined): { container: Container; config: AutoUpdateConfig | null } | undefined {
  const container = list?.find((c) => c.Names?.includes(`/${WATCHTOWER}`));
  if (!container) return undefined;
  let config: AutoUpdateConfig | null = null;
  try {
    config = JSON.parse(container.Labels?.[CONFIG_LABEL] ?? '') as AutoUpdateConfig;
  } catch {
    /* created by hand */
  }
  return { container, config };
}

/** Last log lines of a container (stdout and stderr, in order). */
export function readLogs(name: string, tail = 600): Promise<string[]> {
  return new Promise((resolve, reject) => {
    const out: string[] = [];
    const lines = new LogLines(false, false, (l) => out.push(l.text));
    docker.stream('GET', `/containers/${name}/logs`, { query: { stdout: '1', stderr: '1', tail: String(tail) } }, {
      onData: (c) => lines.push(c),
      onEnd: () => {
        lines.flush();
        resolve(out);
      },
      onError: reject,
    });
  });
}

export interface OnceOptions {
  /** true: look for updates only, change nothing. */
  lookOnly: boolean;
  onLine(text: string): void;
  onPhase?(p: 'pull' | 'run'): void;
}

/** Runs Watchtower once in a throwaway container and streams its log. Resolves with the exit code. */
export async function runOnce(c: AutoUpdateConfig, o: OnceOptions): Promise<number> {
  const cfg = { ...c, monitorOnly: o.lookOnly ? true : false };
  o.onPhase?.('pull');
  await ensureImage(cfg.image);
  await removeIfExists(WATCHTOWER_ONCE);
  await docker.post('/containers/create', { name: WATCHTOWER_ONCE }, body(cfg, true));
  try {
    o.onPhase?.('run');
    await docker.post(`/containers/${WATCHTOWER_ONCE}/start`);
    await new Promise<void>((resolve, reject) => {
      const lines = new LogLines(false, false, (l) => o.onLine(l.text));
      docker.stream('GET', `/containers/${WATCHTOWER_ONCE}/logs`, { query: { stdout: '1', stderr: '1', follow: '1' } }, {
        onData: (ch) => lines.push(ch),
        onEnd: () => {
          lines.flush();
          resolve();
        },
        onError: reject,
      });
    });
    const info = await docker.get<ContainerInspect>(`/containers/${WATCHTOWER_ONCE}/json`);
    return info.State.ExitCode;
  } finally {
    await removeIfExists(WATCHTOWER_ONCE).catch(() => undefined);
  }
}

/* ---------- log parsing ---------- */

export interface LogEntry {
  time: number;
  level: string;
  msg: string;
  attrs: Record<string, string>;
}

const PAIR = /([A-Za-z_][\w.-]*)=("(?:[^"\\]|\\.)*"|\S*)/g;

export function parseLine(line: string): LogEntry | null {
  if (!line.includes('msg=')) return null;
  const attrs: Record<string, string> = {};
  for (const m of line.matchAll(PAIR)) {
    let v = m[2];
    if (v.startsWith('"')) {
      try {
        v = JSON.parse(v);
      } catch {
        v = v.slice(1, -1);
      }
    }
    attrs[m[1]] = v;
  }
  const { time, level, msg, ...rest } = attrs;
  if (msg === undefined) return null;
  return { time: Date.parse(time) || 0, level: level ?? 'info', msg, attrs: rest };
}

export interface Session {
  /** Unix ms of the end of the check. */
  at: number;
  scanned: number;
  updated: number;
  failed: number;
  /** Containers with a newer image. */
  found: { container: string; image: string }[];
  /** Containers that were recreated. */
  updatedNames: string[];
  errors: string[];
}

export interface ParsedLog {
  sessions: Session[]; // newest first
  /** "Scheduling first run: ..." style text, when seen after the last session. */
  next: string;
}

const num = (v: string | undefined) => Number(v ?? 0) || 0;

/** Turns Watchtower's log into one entry per check. */
export function parseSessions(lines: string[]): ParsedLog {
  const sessions: Session[] = [];
  let cur: Session = { at: 0, scanned: 0, updated: 0, failed: 0, found: [], updatedNames: [], errors: [] };
  let next = '';
  for (const line of lines) {
    const e = parseLine(line);
    if (!e) continue;
    const m = e.msg.toLowerCase();
    if (m.startsWith('scheduling first run')) next = e.msg.replace(/^Scheduling first run:\s*/i, '');
    else if (m.startsWith('found new')) cur.found.push({ container: e.attrs.container ?? '', image: e.attrs.image ?? '' });
    else if (m.startsWith('started new container')) cur.updatedNames.push(e.attrs.container ?? '');
    else if (e.level === 'error' || e.level === 'fatal') cur.errors.push(e.msg + (e.attrs.error ? `: ${e.attrs.error}` : ''));
    else if (/(session (completed|done))/.test(m)) {
      cur.at = e.time;
      cur.scanned = num(e.attrs.scanned);
      cur.updated = num(e.attrs.updated);
      cur.failed = num(e.attrs.failed);
      sessions.push(cur);
      cur = { at: 0, scanned: 0, updated: 0, failed: 0, found: [], updatedNames: [], errors: [] };
    }
  }
  return { sessions: sessions.reverse(), next };
}
