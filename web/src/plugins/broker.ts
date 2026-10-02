/**
 * Broker policy: decides what a request from a plugin frame may do, from the plugin's manifest alone.
 * Pure (no imports with side effects) so it can be unit-tested with `node --test` (web/tests/broker.test.ts).
 *
 * The daemon enforces the same rules again (plugins.exec / plugins.execStream / plugins.pty / plugins.http /
 * plugins.httpStream / plugins.readFile / plugins.writeFile / plugins.listDir / plugins.mkdir / plugins.remove
 * check the manifest, the signature policy and visibleTo), so this is the first of two gates, never the only
 * one. What a plugin can never reach through the broker: any other RPC method, raw streams, admin rights for
 * a command, HTTP API or folder not declared `admin`, files outside `capabilities.files`, sockets other than
 * through its declared commands and `capabilities.http` rules, other plugins' assets.
 */
import type { FrameOp } from './protocol';

/** Anything that may need the root bridge: a command, an HTTP API or a folder. */
export interface AdminLevel {
  admin?: boolean;
  adminUnlessGroup?: string;
}

export interface BrokerCommand extends AdminLevel {
  name: string;
  args?: unknown[];
  pty?: boolean;
}

export interface BrokerHTTP extends AdminLevel {
  name: string;
  socket?: string;
  headers?: string[];
  rules?: { methods: string[]; path: string }[];
}

/** A capabilities.files entry: a path (SDK v2) or {path, admin, adminUnlessGroup, create} (v3). */
export type BrokerFolder = string | ({ path: string; create?: boolean } & AdminLevel);

export interface BrokerManifest {
  id: string;
  capabilities?: {
    commands?: BrokerCommand[];
    http?: BrokerHTTP[];
    files?: { read?: BrokerFolder[]; write?: BrokerFolder[] };
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
  | { kind: 'openUrl'; url: string }
  | { kind: 'deny'; code: 'forbidden' | 'invalid'; message: string };

const MAX_ARGS = 16;
const MAX_ARG_LEN = 4096;
const MAX_PATH = 4096;
const MAX_WRITE = 4 << 20;
const MAX_HTTP_PATH = 2048;
const MAX_HTTP_QUERY = 8 << 10;
const MAX_HTTP_HEADERS = 32;
const MAX_HTTP_BODY = 64 << 20;
const HTTP_METHODS = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'];

const deny = (message: string, code: 'forbidden' | 'invalid' = 'forbidden'): Plan => ({ kind: 'deny', code, message });

function command(m: BrokerManifest, name: unknown): BrokerCommand | undefined {
  if (typeof name !== 'string') return undefined;
  return (m.capabilities?.commands ?? []).find((c) => c.name === name);
}

/** Root bridge needed: the entry is declared admin and the user is neither root nor in adminUnlessGroup. */
export function needsAdmin(c: AdminLevel, u: BrokerUser): boolean {
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

/** A files entry as an object. */
export const folderOf = (f: BrokerFolder): { path: string; create?: boolean } & AdminLevel => (typeof f === 'string' ? { path: f } : f && typeof f === 'object' ? f : { path: '' });

/**
 * The declared folder path falls in: the longest match, and on a tie the entry without admin (as the daemon).
 * Null when none.
 */
export function matchFolder(path: string, declared: BrokerFolder[], home?: string): ({ path: string } & AdminLevel) | null {
  const p = normPath(path, home);
  if (!p) return null;
  let best: ({ path: string } & AdminLevel) | null = null;
  let bestLen = -1;
  for (const d of declared) {
    const f = folderOf(d);
    const root = typeof f.path === 'string' ? normPath(f.path, home) : null;
    if (!root || !(root === '/' || p === root || p.startsWith(root + '/'))) continue;
    if (root.length > bestLen || (root.length === bestLen && best?.admin && !f.admin)) {
      best = f;
      bestLen = root.length;
    }
  }
  return best;
}

/** True when path (normalised) is one of the declared folders or inside one. */
export function underDeclared(path: string, declared: BrokerFolder[], home?: string): boolean {
  return matchFolder(path, declared, home) !== null;
}

/**
 * The decoded form of an HTTP request path, or null when the daemon would refuse it: origin-form only, no query,
 * fragment, control characters or backslashes, no encoded "/", "\" or NUL, no empty, "." or ".." segments.
 */
export function cleanHttpPath(raw: unknown): string | null {
  if (typeof raw !== 'string' || !raw.startsWith('/') || raw.startsWith('//') || raw.length > MAX_HTTP_PATH) return null;
  // eslint-disable-next-line no-control-regex
  if (/[\x00-\x20\x7f?#\\]/.test(raw) || /%(2f|5c|00|0a|0d)/i.test(raw)) return null;
  let dec: string;
  try {
    dec = decodeURIComponent(raw);
  } catch {
    return null;
  }
  // eslint-disable-next-line no-control-regex
  if (/[\x00-\x1f\x7f\\]/.test(dec)) return null;
  if (dec === '/') return dec;
  const segs = dec.slice(1).split('/');
  if (segs.some((s) => s === '' || s === '.' || s === '..')) return null;
  return dec;
}

/** A raw query string: printable ASCII without spaces or "#". */
export const validQuery = (q: unknown): q is string => typeof q === 'string' && q.length <= MAX_HTTP_QUERY && /^[\x21-\x22\x24-\x7e]*$/.test(q);

const ruleCache = new Map<string, RegExp | null>();
/** A manifest rule as a JS RegExp (anchored like the daemon). Null when Go syntax does not translate: the daemon decides. */
function ruleRe(p: string): RegExp | null {
  if (!ruleCache.has(p)) {
    let re: RegExp | null;
    try {
      re = new RegExp(`^(?:${p})$`, 'u');
    } catch {
      re = null;
    }
    ruleCache.set(p, re);
  }
  return ruleCache.get(p)!;
}

/** True when a rule of api allows method on the decoded path. */
export function httpAllowed(api: BrokerHTTP, method: string, decoded: string): boolean {
  return (api.rules ?? []).some((r) => {
    if (!Array.isArray(r.methods) || !r.methods.includes(method) || typeof r.path !== 'string') return false;
    const re = ruleRe(r.path);
    return re ? re.test(decoded) : true;
  });
}

function bytesToB64(u: Uint8Array): string {
  let s = '';
  for (let i = 0; i < u.length; i += 0x8000) s += String.fromCharCode(...u.subarray(i, i + 0x8000));
  return btoa(s);
}

/** Checks an http / httpStream request; returns the daemon params or a denial. */
function httpRequest(m: BrokerManifest, req: Record<string, unknown>, u: BrokerUser): { params: Record<string, unknown>; admin: boolean } | Plan {
  const api = typeof req.name === 'string' ? (m.capabilities?.http ?? []).find((h) => h.name === req.name) : undefined;
  if (!api) return deny(`${m.id} does not declare an HTTP API ${JSON.stringify(req.name)}.`);
  const method = req.method;
  if (typeof method !== 'string' || !HTTP_METHODS.includes(method)) return deny(`${JSON.stringify(method)} is not an allowed HTTP method.`, 'invalid');
  const dec = cleanHttpPath(req.path);
  if (dec === null) return deny(`${JSON.stringify(req.path)} is not a valid request path.`, 'invalid');
  if (!httpAllowed(api, method, dec)) return deny(`${m.id} does not declare ${method} ${dec} for ${api.name}.`, 'invalid');
  const query = req.query ?? '';
  if (!validQuery(query)) return deny('The query string is too long or holds characters that are not allowed.', 'invalid');
  const headers: Record<string, string> = {};
  const h = req.headers ?? {};
  if (typeof h !== 'object' || Array.isArray(h)) return deny('Headers must be an object of strings.', 'invalid');
  const entries = Object.entries(h as Record<string, unknown>);
  if (entries.length > MAX_HTTP_HEADERS) return deny('Too many request headers.', 'invalid');
  const allowed = (api.headers ?? []).map((x) => x.toLowerCase());
  for (const [k, v] of entries) {
    if (!allowed.includes(k.toLowerCase())) return deny(`${m.id} may not set the header ${JSON.stringify(k)} on ${api.name}.`, 'invalid');
    if (typeof v !== 'string' || v.length > 8192 || /[\r\n\0]/.test(v)) return deny(`The value of the header ${JSON.stringify(k)} is not allowed.`, 'invalid');
    headers[k] = v;
  }
  const params: Record<string, unknown> = { plugin: m.id, name: api.name, method, path: req.path, ...(query ? { query } : {}) };
  if (entries.length) params.headers = headers;
  const body = req.body;
  if (body instanceof Uint8Array) {
    if (body.length > MAX_HTTP_BODY) return deny('The request body is too large.', 'invalid');
    params.body = bytesToB64(body);
    params.b64 = true;
  } else if (typeof body === 'string') {
    if (body.length > MAX_HTTP_BODY) return deny('The request body is too large.', 'invalid');
    params.body = body;
  } else if (body !== undefined && body !== null) {
    return deny('The body must be a string or a Uint8Array.', 'invalid');
  }
  if (req.json === true) params.json = true;
  return { params, admin: needsAdmin(api, u) };
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
      if (c.pty) return deny(`${c.name} is a terminal command: open it with sdk.api.pty.`, 'invalid');
      const list = argList(a.args);
      if (!list) return deny('Command arguments must be a list of at most 16 strings.', 'invalid');
      return { kind: 'call', method: 'plugins.exec', params: { plugin: m.id, command: c.name, args: list }, admin: needsAdmin(c, u) };
    }
    case 'readFile':
    case 'listDir':
    case 'writeFile':
    case 'mkdir':
    case 'remove': {
      const path = str(a.path, MAX_PATH);
      if (!path) return deny('Give an absolute path.', 'invalid');
      const files = m.capabilities?.files ?? {};
      const write = op === 'writeFile' || op === 'mkdir' || op === 'remove';
      const declared = write ? files.write ?? [] : [...(files.read ?? []), ...(files.write ?? [])];
      if (!declared.length) return deny(`${m.id} declares no files it may ${write ? 'write' : 'read'}.`);
      // Paths with ~ are checked by the daemon when the home is unknown here (admin folders are never under ~).
      const tilde = path === '~' || path.startsWith('~/');
      const folder = tilde && !u.home ? { path } : matchFolder(path, declared, u.home);
      if (!folder) return deny(`${m.id} did not declare that it may ${write ? 'write' : 'read'} ${path}.`);
      const admin = needsAdmin(folder, u);
      if (op === 'writeFile') {
        const data = str(a.data, Math.ceil((MAX_WRITE * 4) / 3) + 4);
        if (data === null) return deny('The data to write is missing or too large.', 'invalid');
        return { kind: 'call', method: 'plugins.writeFile', params: { plugin: m.id, path, data, b64: a.b64 === true }, admin };
      }
      if (op === 'mkdir' || op === 'remove') return { kind: 'call', method: `plugins.${op}`, params: { plugin: m.id, path }, admin };
      const method = op === 'readFile' ? 'plugins.readFile' : 'plugins.listDir';
      return { kind: 'call', method, params: { plugin: m.id, path, ...(op === 'readFile' && a.b64 === true ? { b64: true } : {}) }, admin };
    }
    case 'http': {
      const r = httpRequest(m, a, u);
      if ('kind' in r) return r;
      return { kind: 'call', method: 'plugins.http', params: r.params, admin: r.admin };
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
    case 'openUrl': {
      const url = externalUrl(a.url);
      if (!url) return deny('Only http and https addresses without credentials can be opened.', 'invalid');
      return { kind: 'openUrl', url };
    }
  }
  return deny(`Plugins cannot use ${JSON.stringify(op)}.`);
}

/** An http(s) URL without user info, as a normalised string, or null. */
function externalUrl(v: unknown): string | null {
  if (typeof v !== 'string' || v.length > 2048) return null;
  let u: URL;
  try {
    u = new URL(v);
  } catch {
    return null;
  }
  if ((u.protocol !== 'http:' && u.protocol !== 'https:') || u.username || u.password) return null;
  return u.href;
}

/** Decision for a streamed command (plugins.execStream). */
export function authorizeStream(m: BrokerManifest, cmd: unknown, args: unknown, u: BrokerUser): Plan {
  const c = command(m, cmd);
  if (!c) return deny(`${m.id} does not declare a command ${JSON.stringify(cmd)}.`);
  if (c.pty) return deny(`${c.name} is a terminal command: open it with sdk.api.pty.`, 'invalid');
  const list = argList(args);
  if (!list) return deny('Command arguments must be a list of at most 16 strings.', 'invalid');
  return { kind: 'stream', method: 'plugins.execStream', params: { plugin: m.id, command: c.name, args: list }, admin: needsAdmin(c, u) };
}

/** Decision for a streamed HTTP request (plugins.httpStream). */
export function authorizeHttpStream(m: BrokerManifest, req: unknown, u: BrokerUser): Plan {
  if (!req || typeof req !== 'object') return deny('Give an HTTP request.', 'invalid');
  const r = httpRequest(m, req as Record<string, unknown>, u);
  if ('kind' in r) return r;
  return { kind: 'stream', method: 'plugins.httpStream', params: r.params, admin: r.admin };
}

/** Decision for a terminal command (plugins.pty): only commands declared `pty: true`. */
export function authorizePty(m: BrokerManifest, cmd: unknown, args: unknown, cols: unknown, rows: unknown, u: BrokerUser): Plan {
  const c = command(m, cmd);
  if (!c) return deny(`${m.id} does not declare a command ${JSON.stringify(cmd)}.`);
  if (!c.pty) return deny(`${c.name} is not declared as a terminal (pty) command.`, 'invalid');
  const list = argList(args);
  if (!list) return deny('Command arguments must be a list of at most 16 strings.', 'invalid');
  const size = (v: unknown, d: number) => (Number.isInteger(v) && (v as number) >= 1 && (v as number) <= 1000 ? (v as number) : d);
  return { kind: 'stream', method: 'plugins.pty', params: { plugin: m.id, command: c.name, args: list, cols: size(cols, 80), rows: size(rows, 24) }, admin: needsAdmin(c, u) };
}
