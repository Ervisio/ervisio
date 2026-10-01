/**
 * Broker policy: decides what a request from a plugin frame may do, from the plugin's manifest alone.
 * Pure (no imports with side effects) so it can be unit-tested with `node --test` (web/tests/broker.test.ts).
 *
 * The daemon enforces the same rules again (plugins.exec / plugins.execStream / plugins.readFile /
 * plugins.writeFile / plugins.listDir check the manifest, the signature policy and visibleTo), so this is
 * the first of two gates, never the only one. What a plugin can never reach through the broker: any other
 * RPC method, raw streams, admin rights for a command not declared `admin`, files outside
 * `capabilities.files`, sockets other than through its declared commands, other plugins' assets.
 */
import type { FrameOp } from './protocol';

export interface BrokerCommand {
  name: string;
  admin?: boolean;
  adminUnlessGroup?: string;
  args?: unknown[];
}

export interface BrokerManifest {
  id: string;
  capabilities?: {
    commands?: BrokerCommand[];
    files?: { read?: string[]; write?: string[] };
  };
  contributes?: { pages?: { id: string }[] };
}

export interface BrokerUser {
  groups?: string[];
  isRoot?: boolean;
  home?: string;
}

export type Plan =
  | { kind: 'call'; method: string; params: Record<string, unknown>; admin: boolean }
  | { kind: 'stream'; method: string; params: Record<string, unknown>; admin: boolean }
  | { kind: 'asset'; path: string }
  | { kind: 'toast'; tone: 'ok' | 'err' | 'info'; title: string; detail?: string }
  | { kind: 'open'; to: string }
  | { kind: 'deny'; code: 'forbidden' | 'invalid'; message: string };

const MAX_ARGS = 16;
const MAX_ARG_LEN = 4096;
const MAX_PATH = 4096;
const MAX_WRITE = 4 << 20;

const deny = (message: string, code: 'forbidden' | 'invalid' = 'forbidden'): Plan => ({ kind: 'deny', code, message });

function command(m: BrokerManifest, name: unknown): BrokerCommand | undefined {
  if (typeof name !== 'string') return undefined;
  return (m.capabilities?.commands ?? []).find((c) => c.name === name);
}

/** Root bridge needed: the command is declared admin and the user is neither root nor in adminUnlessGroup. */
export function needsAdmin(c: BrokerCommand, u: BrokerUser): boolean {
  if (!c.admin || u.isRoot) return false;
  return !(c.adminUnlessGroup && (u.groups ?? []).includes(c.adminUnlessGroup));
}

function argList(v: unknown): string[] | null {
  if (v === undefined || v === null) return [];
  if (!Array.isArray(v) || v.length > MAX_ARGS) return null;
  if (!v.every((a) => typeof a === 'string' && a.length <= MAX_ARG_LEN)) return null;
  return v as string[];
}

/** Normalises an absolute path lexically ("~/" expanded when the home is known). Null when not absolute. */
export function normPath(p: string, home?: string): string | null {
  if (p.includes('\0') || p.length > MAX_PATH) return null;
  if (p === '~' || p.startsWith('~/')) {
    if (!home) return null;
    p = home.replace(/\/+$/, '') + p.slice(1);
  }
  if (!p.startsWith('/')) return null;
  const out: string[] = [];
  for (const seg of p.split('/')) {
    if (seg === '' || seg === '.') continue;
    if (seg === '..') out.pop();
    else out.push(seg);
  }
  return '/' + out.join('/');
}

/** True when path (normalised) is one of the declared folders or inside one. */
export function underDeclared(path: string, declared: string[], home?: string): boolean {
  const p = normPath(path, home);
  if (!p) return false;
  return declared.some((d) => {
    const root = normPath(d, home);
    if (!root) return false;
    return root === '/' || p === root || p.startsWith(root + '/');
  });
}

/** A relative asset path inside the plugin folder. */
export function validAsset(p: unknown): p is string {
  if (typeof p !== 'string' || !p || p.length > 256) return false;
  if (p.startsWith('/') || p.includes('\\') || p.includes('\0') || p.includes('?') || p.includes('#') || p.includes('%')) return false;
  return p.split('/').every((s) => s !== '' && s !== '.' && s !== '..');
}

const str = (v: unknown, max: number): string | null => (typeof v === 'string' && v.length <= max ? v : null);

/** The broker's decision for one request of plugin `m`. */
export function authorize(m: BrokerManifest, op: FrameOp | string, args: Record<string, unknown>, u: BrokerUser): Plan {
  const a = args && typeof args === 'object' ? args : {};
  switch (op) {
    case 'exec': {
      const c = command(m, a.command);
      if (!c) return deny(`${m.id} does not declare a command ${JSON.stringify(a.command)}.`);
      const list = argList(a.args);
      if (!list) return deny('Command arguments must be a list of at most 16 strings.', 'invalid');
      return { kind: 'call', method: 'plugins.exec', params: { plugin: m.id, command: c.name, args: list }, admin: needsAdmin(c, u) };
    }
    case 'readFile':
    case 'listDir':
    case 'writeFile': {
      const path = str(a.path, MAX_PATH);
      if (!path) return deny('Give an absolute path.', 'invalid');
      const files = m.capabilities?.files ?? {};
      const write = op === 'writeFile';
      const declared = write ? files.write ?? [] : [...(files.read ?? []), ...(files.write ?? [])];
      // Paths with ~ are checked by the daemon when the home is unknown here.
      const tilde = path === '~' || path.startsWith('~/');
      if (!(tilde && !u.home) && !underDeclared(path, declared, u.home)) {
        return deny(`${m.id} did not declare that it may ${write ? 'write' : 'read'} ${path}.`);
      }
      if (!declared.length) return deny(`${m.id} declares no files it may ${write ? 'write' : 'read'}.`);
      if (write) {
        const data = str(a.data, Math.ceil((MAX_WRITE * 4) / 3) + 4);
        if (data === null) return deny('The data to write is missing or too large.', 'invalid');
        return { kind: 'call', method: 'plugins.writeFile', params: { plugin: m.id, path, data, b64: a.b64 === true }, admin: false };
      }
      const method = op === 'readFile' ? 'plugins.readFile' : 'plugins.listDir';
      return { kind: 'call', method, params: { plugin: m.id, path, ...(op === 'readFile' && a.b64 === true ? { b64: true } : {}) }, admin: false };
    }
    case 'asset':
      if (!validAsset(a.path)) return deny('Give a relative path inside the plugin folder.', 'invalid');
      return { kind: 'asset', path: a.path };
    case 'toast': {
      const tone = a.tone === 'err' || a.tone === 'info' ? a.tone : 'ok';
      const title = str(a.title, 200);
      if (!title) return deny('A toast needs a short title.', 'invalid');
      const detail = a.detail === undefined ? undefined : str(a.detail, 1000) ?? undefined;
      return { kind: 'toast', tone, title, detail };
    }
    case 'open': {
      const page = (m.contributes?.pages ?? []).find((p) => p.id === a.page);
      if (!page) return deny(`${m.id} has no page ${JSON.stringify(a.page)}.`);
      return { kind: 'open', to: `/p/${m.id}/${page.id}` };
    }
  }
  return deny(`Plugins cannot use ${JSON.stringify(op)}.`);
}

/** Decision for a streamed command (plugins.execStream). */
export function authorizeStream(m: BrokerManifest, cmd: unknown, args: unknown, u: BrokerUser): Plan {
  const c = command(m, cmd);
  if (!c) return deny(`${m.id} does not declare a command ${JSON.stringify(cmd)}.`);
  const list = argList(args);
  if (!list) return deny('Command arguments must be a list of at most 16 strings.', 'invalid');
  return { kind: 'stream', method: 'plugins.execStream', params: { plugin: m.id, command: c.name, args: list }, admin: needsAdmin(c, u) };
}
