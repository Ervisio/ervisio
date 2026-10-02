/**
 * Compose stacks. Managed stacks live in /opt/stacks/<name>/compose.yaml (+ .env, + .compose.deployed.yaml, a copy of
 * the file as it was at the last successful deploy). Projects started elsewhere are detected from container labels
 * and `docker compose ls`; they get read-only treatment plus actions by project name.
 *
 * Writing and running (the templates view uses these two):
 *   await writeStack('web', composeText, envText);
 *   const code = await deployStack('web', { pull: true }, (stream, line) => ...);   // 0 = success
 */
import { parse } from 'yaml';
import { getSdk, type PluginError } from '../sdk';
import { docker } from './engine';
import { COMPOSE_FILES, COMPOSE_PROJECT, COMPOSE_SERVICE, COMPOSE_WORKDIR, type Container } from './types';

export const STACKS_DIR = '/opt/stacks';
export const STACK_NAME_RE = /^[a-z0-9][a-z0-9_-]{0,62}$/;
export const COMPOSE_FILE = 'compose.yaml';
export const ENV_FILE = '.env';
export const DEPLOYED_FILE = '.compose.deployed.yaml';

export type LineHandler = (stream: 'stdout' | 'stderr', line: string) => void;
export type StackAction = 'down' | 'restart' | 'pull' | 'stop' | 'start';

export interface StackService {
  name: string;
  /** Containers of this service, newest state first. */
  containers: Container[];
  /** Running when at least one container is running; stopped otherwise (also when it has none). */
  state: 'running' | 'restarting' | 'paused' | 'stopped' | 'none';
}

export interface Stack {
  name: string;
  managed: boolean;
  /** Working directory of a detected project (label), or /opt/stacks/<name> for a managed one. */
  dir: string;
  /** Compose files of a detected project (label, comma separated in Docker, split here). */
  configFiles: string[];
  /** Status text of `docker compose ls`, such as "running(2)". */
  status: string;
  /** Service names: from the compose file when managed, else from the containers. */
  serviceNames: string[];
  services: StackService[];
  containers: Container[];
  running: number;
  total: number;
}

export const stackDir = (name: string): string => `${STACKS_DIR}/${name}`;
export const isValidStackName = (name: string): boolean => STACK_NAME_RE.test(name);

const isNotFound = (e: unknown): boolean => {
  const c = (e as PluginError | undefined)?.code;
  return c === 'not_found' || /no such file|not exist|not found|ENOENT/i.test((e as Error)?.message ?? '');
};


/** The app's HTTP connection can drop a request that follows a long idle stretch ("Cannot reach the server"). Retry those. */
async function retry<T>(fn: () => Promise<T>): Promise<T> {
  for (let i = 0; ; i++) {
    try {
      return await fn();
    } catch (e) {
      if (i >= 2 || !/Cannot reach the server/i.test((e as Error)?.message ?? '')) throw e;
      await new Promise((r) => setTimeout(r, 400 * (i + 1)));
    }
  }
}

const fsx = {
  read: (p: string) => retry(() => getSdk().files.read(p)),
  list: (p: string) => retry(() => getSdk().files.list(p)),
  write: (p: string, d: string) => retry(() => getSdk().files.write(p, d)),
  mkdir: (p: string) => retry(() => getSdk().files.mkdir(p)),
  remove: (p: string) => retry(() => getSdk().files.remove(p)),
};

/* ---------- the folder ---------- */

/** True when /opt/stacks exists and can be read. Rethrows any other error (for example needs_admin). */
export async function stacksFolderExists(): Promise<boolean> {
  try {
    await fsx.list(STACKS_DIR);
    return true;
  } catch (e) {
    if (isNotFound(e)) return false;
    throw e;
  }
}

/** Create /opt/stacks (root:docker 2775) with the admin command stacks-init when it is missing. */
export async function ensureStacksFolder(): Promise<void> {
  if (await stacksFolderExists()) return;
  const r = await getSdk().api.exec('stacks-init', []);
  if (r.exitCode !== 0) throw new Error(r.stderr.trim() || `stacks-init exited with ${r.exitCode}`);
}

/* ---------- reading ---------- */

export interface StackFiles {
  name: string;
  compose: string;
  env: string;
  /** The compose file as last deployed, or null when the stack was never deployed from here. */
  deployed: string | null;
  hasEnv: boolean;
}

async function readOptional(path: string): Promise<string | null> {
  try {
    return await fsx.read(path);
  } catch (e) {
    if (isNotFound(e)) return null;
    throw e;
  }
}

/** Throws not_found when the stack has no compose.yaml. */
export async function readStack(name: string): Promise<StackFiles> {
  if (!isValidStackName(name)) throw new Error('Invalid stack name');
  const dir = stackDir(name);
  const [compose, env, deployed] = await Promise.all([
    fsx.read(`${dir}/${COMPOSE_FILE}`),
    readOptional(`${dir}/${ENV_FILE}`),
    readOptional(`${dir}/${DEPLOYED_FILE}`),
  ]);
  return { name, compose, env: env ?? '', deployed, hasEnv: env !== null };
}

/** Service names of a compose text; empty when it does not parse. */
export function serviceNamesOf(text: string): string[] {
  try {
    const doc = parse(text) as { services?: Record<string, unknown> } | null;
    return doc && typeof doc.services === 'object' && doc.services ? Object.keys(doc.services) : [];
  } catch {
    return [];
  }
}

export interface ComposeLsEntry {
  Name: string;
  Status: string;
  ConfigFiles: string;
}

async function composeLs(): Promise<ComposeLsEntry[]> {
  try {
    const r = await getSdk().api.exec('compose-ls', []);
    if (r.exitCode !== 0) return [];
    const v = JSON.parse(r.stdout || '[]');
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

async function managedNames(): Promise<{ exists: boolean; names: Set<string> }> {
  const out = new Set<string>();
  let entries;
  try {
    entries = await fsx.list(STACKS_DIR);
  } catch (e) {
    if (isNotFound(e)) return { exists: false, names: out };
    throw e;
  }
  await Promise.all(
    entries
      .filter((e) => e.type === 'dir' && isValidStackName(e.name))
      .map(async (e) => {
        try {
          const inner = await fsx.list(stackDir(e.name));
          if (inner.some((f) => f.name === COMPOSE_FILE)) out.add(e.name);
        } catch (err) {
          if (!isNotFound(err)) throw err;
        }
      }),
  );
  return { exists: true, names: out };
}

const STATE_ORDER = ['running', 'restarting', 'paused'];

/** Fold the container list into per-service rows. */
export function servicesOf(list: Container[], names: string[] = []): StackService[] {
  const map = new Map<string, Container[]>();
  for (const n of names) map.set(n, []);
  for (const c of list) {
    const s = c.Labels?.[COMPOSE_SERVICE] ?? c.Names?.[0]?.replace(/^\//, '') ?? c.Id.slice(0, 12);
    if (!map.has(s)) map.set(s, []);
    map.get(s)!.push(c);
  }
  return [...map.entries()].map(([name, cs]) => {
    const states = cs.map((c) => c.State);
    const state = (STATE_ORDER.find((s) => states.includes(s)) as StackService['state'] | undefined) ?? (cs.length ? 'stopped' : 'none');
    return { name, containers: cs, state };
  });
}

export interface StackSources {
  /** False when /opt/stacks does not exist yet. */
  folder: boolean;
  managed: Set<string>;
  /** Service names per managed stack, read from compose.yaml. */
  names: Map<string, string[]>;
  ls: ComposeLsEntry[];
}

/** Everything about stacks that is not in the container list: the folder and `docker compose ls`. */
export async function loadStackSources(): Promise<StackSources> {
  const [{ exists: folder, names: managed }, ls] = await Promise.all([managedNames(), composeLs()]);
  const names = new Map<string, string[]>();
  await Promise.all(
    [...managed].map(async (n) => {
      const text = await readOptional(`${stackDir(n)}/${COMPOSE_FILE}`).catch(() => null);
      names.set(n, text ? serviceNamesOf(text) : []);
    }),
  );
  return { folder, managed, names, ls };
}

/** Pure merge of sources and containers into stacks (alphabetical). Managed first is a UI concern. */
export function buildStacks(src: StackSources, list: Container[]): Stack[] {
  const byProject = new Map<string, Container[]>();
  for (const c of list) {
    const p = c.Labels?.[COMPOSE_PROJECT];
    if (!p) continue;
    if (!byProject.has(p)) byProject.set(p, []);
    byProject.get(p)!.push(c);
  }
  const all = new Set<string>([...src.managed, ...byProject.keys(), ...src.ls.map((l) => l.Name)]);
  const out: Stack[] = [];
  for (const name of [...all].sort((a, b) => a.localeCompare(b))) {
    const cs = byProject.get(name) ?? [];
    const lsEntry = src.ls.find((l) => l.Name === name);
    const managed = src.managed.has(name);
    const files = (cs[0]?.Labels?.[COMPOSE_FILES] ?? lsEntry?.ConfigFiles ?? '').split(',').map((s) => s.trim()).filter(Boolean);
    const names = managed ? (src.names.get(name) ?? []) : [];
    const services = servicesOf(cs, names);
    out.push({
      name,
      managed,
      dir: managed ? stackDir(name) : (cs[0]?.Labels?.[COMPOSE_WORKDIR] ?? (files[0] ? files[0].replace(/\/[^/]*$/, '') : '')),
      configFiles: files,
      status: lsEntry?.Status ?? '',
      serviceNames: services.map((s) => s.name),
      services,
      containers: cs,
      running: cs.filter((c) => c.State === 'running').length,
      total: cs.length,
    });
  }
  return out;
}

/** All stacks: managed ones, plus projects detected from labels and `docker compose ls`. */
export async function listStacks(): Promise<Stack[]> {
  const [src, list] = await Promise.all([loadStackSources(), docker.get<Container[]>('/containers/json', { all: '1' })]);
  return buildStacks(src, list);
}

/* ---------- writing ---------- */

/** Create /opt/stacks/<name> and write compose.yaml, plus .env when given (an empty .env is written only if one exists or text is given). */
export async function writeStack(name: string, compose: string, env?: string): Promise<void> {
  if (!isValidStackName(name)) throw new Error('Invalid stack name');
  await ensureStacksFolder();
  try {
    await fsx.mkdir(stackDir(name));
  } catch (e) {
    // An existing folder is fine; anything else shows up when writing.
    if (!/exist/i.test((e as Error).message ?? '')) {
      try {
        await fsx.list(stackDir(name));
      } catch {
        throw e;
      }
    }
  }
  await fsx.write(`${stackDir(name)}/${COMPOSE_FILE}`, compose.endsWith('\n') ? compose : compose + '\n');
  if (env !== undefined) {
    const has = (await readOptional(`${stackDir(name)}/${ENV_FILE}`)) !== null;
    if (env.trim() !== '' || has) await fsx.write(`${stackDir(name)}/${ENV_FILE}`, env);
  }
}

/* ---------- running ---------- */

/** Run a manifest command and stream its lines. Resolves with the exit code; rejects when it could not run at all. */
export function runCompose(command: string, args: string[], onLine: LineHandler): Promise<number> {
  return new Promise((resolve, reject) => {
    getSdk().api.execStream(command, args, {
      onLine,
      onExit: resolve,
      onError: reject,
    });
  });
}

/** `docker compose up -d` for a managed stack; with `pull` it pulls first. On success the file is copied to .compose.deployed.yaml. */
export async function deployStack(name: string, opts: { pull?: boolean }, onLine: LineHandler): Promise<number> {
  if (!isValidStackName(name)) throw new Error('Invalid stack name');
  if (opts.pull) {
    onLine('stdout', '$ docker compose pull');
    const code = await runCompose('compose-pull', [name], onLine);
    if (code !== 0) return code;
  }
  onLine('stdout', '$ docker compose up -d --remove-orphans');
  const code = await runCompose('compose-up', [name], onLine);
  if (code === 0) {
    try {
      const text = await fsx.read(`${stackDir(name)}/${COMPOSE_FILE}`);
      await fsx.write(`${stackDir(name)}/${DEPLOYED_FILE}`, text);
    } catch (e) {
      onLine('stderr', `Could not save the deployed copy: ${(e as Error).message}`);
    }
  }
  return code;
}

/** Actions on a stack. Managed stacks use their compose file; detected ones (`detected: true`) act by project name. */
export function stackAction(name: string, action: StackAction, onLine: LineHandler, opts: { detected?: boolean } = {}): Promise<number> {
  if (!isValidStackName(name)) throw new Error('Invalid stack name');
  if (opts.detected) {
    if (action === 'pull') throw new Error('Pulling needs the compose file');
    return runCompose(`compose-p-${action}`, [name], onLine);
  }
  return runCompose(`compose-${action}`, [name], onLine);
}

/** Validate a managed stack's files with `docker compose config`. Resolves with the exit code; output goes to onLine. */
export const configStack = (name: string, onLine: LineHandler): Promise<number> => runCompose('compose-config', [name], onLine);

/** The resolved configuration of a detected project (read only), from the first compose file of its labels. */
export async function projectConfig(name: string, file: string): Promise<{ ok: boolean; text: string }> {
  const r = await getSdk().api.exec('compose-config-project', [name, file]);
  return r.exitCode === 0 ? { ok: true, text: r.stdout } : { ok: false, text: (r.stderr || r.stdout).trim() };
}

/** Copy a detected project into /opt/stacks/<name> as its resolved configuration. The original folder is left alone. */
export async function moveToManaged(name: string, file: string): Promise<void> {
  const cfg = await projectConfig(name, file);
  if (!cfg.ok) throw new Error(cfg.text || 'docker compose config failed');
  await writeStack(name, cfg.text, '');
}

async function removeTree(path: string, depth = 0): Promise<void> {
  const entries = await fsx.list(path);
  for (const e of entries) {
    const p = `${path}/${e.name}`;
    if (e.type === 'dir' && depth < 4) await removeTree(p, depth + 1);
    await fsx.remove(p);
  }
}

/**
 * Delete a stack: `down` (optionally with volumes), then its folder. Resolves with the exit code of down; the folder
 * is removed only when down succeeded.
 */
export async function deleteStack(name: string, opts: { volumes?: boolean; folder?: boolean }, onLine: LineHandler): Promise<number> {
  if (!isValidStackName(name)) throw new Error('Invalid stack name');
  const code = await runCompose(opts.volumes ? 'compose-down-volumes' : 'compose-down', [name], onLine);
  if (code !== 0) return code;
  if (opts.folder) {
    try {
      await removeTree(stackDir(name));
      await fsx.remove(stackDir(name));
    } catch (e) {
      onLine('stderr', `Could not remove the folder: ${(e as Error).message}`);
      return 1;
    }
  }
  return 0;
}
