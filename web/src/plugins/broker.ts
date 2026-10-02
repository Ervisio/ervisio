/**
 * Broker policy: decides what a request from a plugin frame may do, from the plugin's manifest alone.
 * Pure (no imports with side effects) so it can be unit-tested with `node --test` (web/tests/broker.test.ts).
 *
 * The daemon enforces the same rules again (plugins.exec / plugins.execStream / plugins.pty / plugins.http /
 * plugins.httpStream / plugins.download / plugins.upload / plugins.audit.list / plugins.readFile / plugins.writeFile / plugins.listDir / plugins.mkdir / plugins.remove
 * check the manifest, the signature policy and visibleTo), so this is the first of two gates, never the only
 * one. What a plugin can never reach through the broker: any other RPC method, raw streams, admin rights for
 * a command, HTTP API or folder not declared `admin`, files outside `capabilities.files`, sockets other than
 * through its declared commands and `capabilities.http` rules, other plugins' assets.
 */
import { authorizeJobsOp } from './brokerJobs.ts';
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
  /** Kind family ("docker") the command may run against an environment for. */
  remote?: string;
}

export interface BrokerHTTP extends AdminLevel {
  name: string;
  socket?: string;
  headers?: string[];
  rules?: { methods: string[]; path: string }[];
  /** Largest file plugins.upload may send (default 20 GiB). */
  maxUpload?: number;
  /** Kind family ("docker") the API may be sent to an environment for. */
  remote?: string;
}

/** A capabilities.files entry: a path (SDK v2) or {path, admin, adminUnlessGroup, create} (v3). */
export type BrokerFolder = string | ({ path: string; create?: boolean } & AdminLevel);

export interface BrokerManifest {
  id: string;
  capabilities?: {
    commands?: BrokerCommand[];
    http?: BrokerHTTP[];
    /** Background jobs (capabilities.jobs) and the notify capability: see brokerJobs.ts. */
    jobs?: { name: string }[];
    notify?: boolean;
    files?: { read?: BrokerFolder[]; write?: BrokerFolder[] };
    /** capabilities.network.userHosts: the plugin may ask an administrator to approve more hosts. */
    userHosts?: boolean;
  };
  contributes?: { pages?: { id: string }[] };
}

export interface BrokerUser {
  groups?: string[];
  isRoot?: boolean;
  home?: string;
  /** The app's own origin: openUrl never opens it (a tab of the app, e.g. /terminal?cmd=…, is outside the manifest). */
  appOrigin?: string;
}

export type Plan =
  | { kind: 'call'; method: string; params: Record<string, unknown>; admin: boolean }
  /** A large transfer: the host asks the daemon for a one-time URL (POST /api/plugins/transfer) with `body`. */
  | { kind: 'transfer'; transfer: 'download' | 'upload'; body: Record<string, unknown>; admin: boolean; size?: number }
  /** sdk.saveFile: the app saves data the plugin holds in memory as a browser download. */
  | { kind: 'save'; filename: string; mime: string; data: Blob | Uint8Array | string; size: number }
  | { kind: 'stream'; method: string; params: Record<string, unknown>; admin: boolean }
  | { kind: 'asset'; path: string }
  | { kind: 'toast'; tone: 'ok' | 'err' | 'info'; title: string; detail?: string }
  | { kind: 'open'; to: string }
  | { kind: 'openUrl'; url: string }
  /** sdk.network.request: the app asks an administrator (a dialog) and reloads the plugin's frames when approved. */
  | { kind: 'network'; host: string; scheme: 'https' | 'http' }
  | { kind: 'deny'; code: 'forbidden' | 'invalid'; message: string };

const MAX_ARGS = 16;
const MAX_ARG_LEN = 4096;
const MAX_PATH = 4096;
const MAX_WRITE = 4 << 20;
const MAX_HTTP_PATH = 2048;
const MAX_HTTP_QUERY = 8 << 10;
const MAX_HTTP_HEADERS = 32;
// A request body travels inside one JSON message (/api/rpc, or the WebSocket frame that opens a stream), which the
// daemon accepts up to 12 MiB: 8 MiB of bytes is 11.2 MiB as base64. Larger bodies are sent with plugins.upload.
const MAX_HTTP_BODY = 8 << 20;
const TEXT_BODY_INLINE = 256 << 10;
const TOO_BIG = 'The request body is larger than 8 MiB. Send large bodies with sdk.api.upload.';
const DEFAULT_MAX_UPLOAD = 20 * 2 ** 30;
const ENV_RE = /^env-[0-9a-f]{8}$/;
const HOST_RE = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*(:[0-9]{1,5})?$/;
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

/** An optional environment id: undefined when absent, null when it is not an id. */
function envId(v: unknown): string | null | undefined {
  if (v === undefined || v === null || v === '') return undefined;
  return typeof v === 'string' && ENV_RE.test(v) ? v : null;
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
  const env = envId(req.env);
  if (env === null) return deny('env must be the id of an environment.', 'invalid');
  if (env && !api.remote) return deny(`${m.id} does not allow ${api.name} to target an environment.`);
  const params: Record<string, unknown> = { plugin: m.id, name: api.name, method, path: req.path, ...(query ? { query } : {}), ...(env ? { env } : {}) };
  if (entries.length) params.headers = headers;
  const body = req.body;
  if (body instanceof Uint8Array) {
    if (body.length > MAX_HTTP_BODY) return deny(TOO_BIG, 'invalid');
    params.body = bytesToB64(body);
    params.b64 = true;
  } else if (typeof body === 'string') {
    if (body.length > MAX_HTTP_BODY) return deny(TOO_BIG, 'invalid');
    if (body.length > TEXT_BODY_INLINE) {
      // JSON escaping can multiply a long text: send it as base64 so the message size is known.
      const bytes = new TextEncoder().encode(body);
      if (bytes.length > MAX_HTTP_BODY) return deny(TOO_BIG, 'invalid');
      params.body = bytesToB64(bytes);
      params.b64 = true;
    } else params.body = body;
  } else if (body !== undefined && body !== null) {
    return deny('The body must be a string or a Uint8Array.', 'invalid');
  }
  if (req.json === true) params.json = true;
  // Against an environment the daemon's tunnel is the user's own: no administrator rights apply.
  return { params, admin: env ? false : needsAdmin(api, u) };
}

const MAX_FILENAME = 255;
const MAX_AUDIT_LIMIT = 1000;

/** A request for plugins.download / plugins.upload: an HTTP request without a body. */
function transferHttp(m: BrokerManifest, req: unknown, u: BrokerUser, methods: string[], what: string): { params: Record<string, unknown>; admin: boolean; api: BrokerHTTP } | Plan {
  if (!req || typeof req !== 'object') return deny('Give an HTTP request.', 'invalid');
  const r = req as Record<string, unknown>;
  if (typeof r.method !== 'string' || !methods.includes(r.method)) return deny(`A ${what} uses ${methods.join(' or ')}, not ${JSON.stringify(r.method)}.`, 'invalid');
  if (r.body !== undefined && r.body !== null) return deny(`A ${what} takes no body.`, 'invalid');
  const x = httpRequest(m, { ...r, body: undefined, json: undefined }, u);
  if ('kind' in x) return x;
  const api = (m.capabilities?.http ?? []).find((h) => h.name === r.name)!;
  return { ...x, api };
}

/** Decision for plugins.download(name, req, filename): GET of an HTTP API, or the output of a declared command. */
export function authorizeDownload(m: BrokerManifest, a: Record<string, unknown>, u: BrokerUser): Plan {
  const filename = a.filename === undefined ? 'download' : str(a.filename, MAX_FILENAME);
  if (!filename) return deny('The file name is not valid.', 'invalid');
  if (typeof a.command === 'string') {
    const c = command(m, a.command);
    if (!c) return deny(`${m.id} does not declare a command ${JSON.stringify(a.command)}.`);
    if (c.pty) return deny(`${c.name} is a terminal command: it cannot be downloaded.`, 'invalid');
    const list = argList(a.args);
    if (!list) return deny('Command arguments must be a list of at most 16 strings.', 'invalid');
    const env = remoteEnv(m, c, a.env);
    if (typeof env === 'object' && env) return env;
    const admin = env ? false : needsAdmin(c, u);
    return { kind: 'transfer', transfer: 'download', body: { kind: 'download', plugin: m.id, command: c.name, args: list, filename, ...(env ? { env } : {}), admin }, admin };
  }
  const x = transferHttp(m, a.req, u, ['GET'], 'download');
  if ('kind' in x) return x;
  const { name, method, path, query, headers, env } = x.params as { name: string; method: string; path: string; query?: string; headers?: Record<string, string>; env?: string };
  return { kind: 'transfer', transfer: 'download', body: { kind: 'download', plugin: m.id, name, method, path, query, headers, filename, ...(env ? { env } : {}), admin: x.admin }, admin: x.admin };
}

/** Decision for plugins.upload(name, req, file): POST or PUT of `size` bytes, at most the API's maxUpload. */
export function authorizeUpload(m: BrokerManifest, req: unknown, size: unknown, u: BrokerUser, stream = false): Plan {
  if (typeof size !== 'number' || !Number.isSafeInteger(size) || size < 0) return deny('Give a file (a File or Blob).', 'invalid');
  const x = transferHttp(m, req, u, ['POST', 'PUT'], 'upload');
  if ('kind' in x) return x;
  const max = x.api.maxUpload ? x.api.maxUpload : DEFAULT_MAX_UPLOAD;
  if (size > max) return deny(`The file is larger than the ${max} bytes ${m.id} may upload to ${x.api.name}.`, 'invalid');
  const { name, method, path, query, headers, env } = x.params as { name: string; method: string; path: string; query?: string; headers?: Record<string, string>; env?: string };
  return { kind: 'transfer', transfer: 'upload', body: { kind: 'upload', plugin: m.id, name, method, path, query, headers, size, ...(env ? { env } : {}), ...(stream ? { stream: true } : {}), admin: x.admin }, admin: x.admin, size };
}

/** Decision for plugins.audit.list: the plugin's own entries only. */
function authorizeAuditList(m: BrokerManifest, a: Record<string, unknown>): Plan {
  const params: Record<string, unknown> = { plugin: m.id };
  for (const k of ['user', 'action', 'text', 'cursor'] as const) {
    if (a[k] === undefined || a[k] === null || a[k] === '') continue;
    const v = str(a[k], 256);
    if (v === null) return deny(`${k} must be a short string.`, 'invalid');
    params[k] = v;
  }
  for (const k of ['since', 'until'] as const) {
    if (a[k] === undefined || a[k] === null || a[k] === '') continue;
    const v = a[k];
    if (typeof v === 'number' && Number.isFinite(v)) params[k] = v;
    else if (typeof v === 'string' && v.length <= 40) params[k] = v;
    else return deny(`${k} must be a time (milliseconds or ISO 8601).`, 'invalid');
  }
  if (a.limit !== undefined) {
    if (!Number.isInteger(a.limit) || (a.limit as number) < 1 || (a.limit as number) > MAX_AUDIT_LIMIT) return deny(`limit must be between 1 and ${MAX_AUDIT_LIMIT}.`, 'invalid');
    params.limit = a.limit;
  }
  return { kind: 'call', method: 'plugins.audit.list', params, admin: false };
}

/** Largest file sdk.saveFile may save: it is held in memory twice (the frame's copy and the app's Blob). */
export const MAX_SAVE = 64 << 20;

/**
 * A file name safe to offer to the browser, as the daemon cleans download names (server/internal/server/transfer.go):
 * no path, control or bidi characters, no characters that Windows or shells treat specially, no leading dots, at most
 * 200 bytes; "download" when nothing is left.
 */
export function sanitizeFilename(name: string): string {
  let n = name.replace(/\p{Cc}|[\u2028\u2029\u202a-\u202e\u2066-\u2069]/gu, '_');
  n = n.slice(Math.max(n.lastIndexOf('/'), n.lastIndexOf('\\')) + 1);
  n = n.replace(/[<>:"/\\|?*;%`$]/g, '_').trim().replace(/^\.+/, '').replace(/[. ]+$/, '');
  const enc = new TextEncoder();
  while (enc.encode(n).length > 200) n = Array.from(n).slice(0, -1).join('');
  return n || 'download';
}

/** A MIME type, or the generic one. */
const MIME_RE = /^[a-zA-Z0-9][a-zA-Z0-9!#$&^_.+-]{0,60}\/[a-zA-Z0-9][a-zA-Z0-9!#$&^_.+-]{0,60}$/;

/** Decision for sdk.saveFile(filename, data, mime?). */
export function authorizeSave(a: Record<string, unknown>): Plan {
  if (typeof a.filename !== 'string' || !a.filename || a.filename.length > 1024) return deny('Give a file name.', 'invalid');
  const d = a.data;
  let size: number;
  if (typeof d === 'string') size = new TextEncoder().encode(d).length;
  else if (d instanceof Uint8Array) size = d.length;
  else if (typeof Blob !== 'undefined' && d instanceof Blob) size = d.size;
  else return deny('The data must be a string, a Uint8Array or a Blob.', 'invalid');
  if (size > MAX_SAVE) return deny(`The file is larger than ${MAX_SAVE >> 20} MiB. Use sdk.api.download to stream a large file from a service.`, 'invalid');
  let mime = 'application/octet-stream';
  if (a.mime !== undefined && a.mime !== null && a.mime !== '') {
    if (typeof a.mime !== 'string' || !MIME_RE.test(a.mime)) return deny('The MIME type is not valid.', 'invalid');
    mime = a.mime;
  }
  return { kind: 'save', filename: sanitizeFilename(a.filename), mime, data: d, size };
}

/**
 * Rate limit of sdk.saveFile for one frame: at most `count` files and `bytes` bytes in any `windowMs`. A plugin
 * does not need a user gesture to start a download, so this keeps it from flooding the browser with files.
 */
export class SaveLimiter {
  private log: { at: number; size: number }[] = [];
  private count: number;
  private windowMs: number;
  private bytes: number;
  constructor(count = 10, windowMs = 30_000, bytes = 256 << 20) {
    this.count = count;
    this.windowMs = windowMs;
    this.bytes = bytes;
  }
  /** Records the file and returns true when it may be saved now. */
  allow(size: number, now = Date.now()): boolean {
    this.log = this.log.filter((e) => now - e.at < this.windowMs);
    if (this.log.length >= this.count || this.log.reduce((n, e) => n + e.size, 0) + size > this.bytes) return false;
    this.log.push({ at: now, size });
    return true;
  }
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
      const env = remoteEnv(m, c, a.env);
      if (typeof env === 'object' && env) return env;
      return { kind: 'call', method: 'plugins.exec', params: { plugin: m.id, command: c.name, args: list, ...(env ? { env } : {}) }, admin: env ? false : needsAdmin(c, u) };
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
      // An environment (a paired server): the folder lives there, so ~ means nothing here, and the call runs as the
      // mapped user on that server, under its manifest: no administrator rights apply on this side.
      const env = envId(a.env);
      if (env === null) return deny('env must be the id of an environment.', 'invalid');
      if (env) {
        if (tilde) return deny('A path on an environment must be absolute (~ is not expanded there).', 'invalid');
        const remote = [...(m.capabilities?.http ?? []), ...(m.capabilities?.commands ?? [])].some((x) => x.remote);
        if (!remote) return deny(`${m.id} does not allow targeting an environment.`);
      }
      const admin = env ? false : needsAdmin(folder, u);
      const e = env ? { env } : {};
      if (op === 'writeFile') {
        const data = str(a.data, Math.ceil((MAX_WRITE * 4) / 3) + 4);
        if (data === null) return deny('The data to write is missing or too large.', 'invalid');
        return { kind: 'call', method: 'plugins.writeFile', params: { plugin: m.id, path, data, b64: a.b64 === true, ...e }, admin };
      }
      if (op === 'mkdir' || op === 'remove') return { kind: 'call', method: `plugins.${op}`, params: { plugin: m.id, path, ...e }, admin };
      const method = op === 'readFile' ? 'plugins.readFile' : 'plugins.listDir';
      return { kind: 'call', method, params: { plugin: m.id, path, ...(op === 'readFile' && a.b64 === true ? { b64: true } : {}), ...e }, admin };
    }
    case 'http': {
      const r = httpRequest(m, a, u);
      if ('kind' in r) return r;
      return { kind: 'call', method: 'plugins.http', params: r.params, admin: r.admin };
    }
    case 'download':
      return authorizeDownload(m, a, u);
    case 'saveFile':
      return authorizeSave(a);
    case 'auditList':
      return authorizeAuditList(m, a);
    case 'envs':
      return { kind: 'call', method: 'plugins.envs.list', params: {}, admin: false };
    case 'network': {
      if (!m.capabilities?.userHosts) return deny(`${m.id} does not declare capabilities.network.userHosts.`);
      const host = typeof a.host === 'string' ? a.host.trim().toLowerCase() : '';
      const scheme = a.scheme === 'http' ? 'http' : 'https';
      if (!host || host.length > 253 || !HOST_RE.test(host)) return deny('Give a host name with an optional port, like registry.example.org:5000.', 'invalid');
      return { kind: 'network', host, scheme };
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
      if (u.appOrigin && new URL(url).origin === u.appOrigin) return deny('Plugins cannot open pages of this app in a new tab.', 'invalid');
      return { kind: 'openUrl', url };
    }
  }
  if (op === 'jobs' || op === 'notify') return authorizeJobsOp(m, op, a);
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

/** The environment a command may run on: its id, undefined for none, or a denial. */
function remoteEnv(m: BrokerManifest, c: BrokerCommand, v: unknown): string | Plan | undefined {
  const env = envId(v);
  if (env === null) return deny('env must be the id of an environment.', 'invalid');
  if (env && !c.remote) return deny(`${m.id} does not allow ${c.name} to run on an environment.`);
  return env;
}

/** Decision for a streamed command (plugins.execStream). */
export function authorizeStream(m: BrokerManifest, cmd: unknown, args: unknown, u: BrokerUser, envArg?: unknown): Plan {
  const c = command(m, cmd);
  if (!c) return deny(`${m.id} does not declare a command ${JSON.stringify(cmd)}.`);
  if (c.pty) return deny(`${c.name} is a terminal command: open it with sdk.api.pty.`, 'invalid');
  const list = argList(args);
  if (!list) return deny('Command arguments must be a list of at most 16 strings.', 'invalid');
  const env = remoteEnv(m, c, envArg);
  if (typeof env === 'object' && env) return env;
  return { kind: 'stream', method: 'plugins.execStream', params: { plugin: m.id, command: c.name, args: list, ...(env ? { env } : {}) }, admin: env ? false : needsAdmin(c, u) };
}

/** Decision for a streamed HTTP request (plugins.httpStream). */
export function authorizeHttpStream(m: BrokerManifest, req: unknown, u: BrokerUser): Plan {
  if (!req || typeof req !== 'object') return deny('Give an HTTP request.', 'invalid');
  const r = httpRequest(m, req as Record<string, unknown>, u);
  if ('kind' in r) return r;
  return { kind: 'stream', method: 'plugins.httpStream', params: r.params, admin: r.admin };
}

/** Decision for a terminal command (plugins.pty): only commands declared `pty: true`. */
export function authorizePty(m: BrokerManifest, cmd: unknown, args: unknown, cols: unknown, rows: unknown, u: BrokerUser, envArg?: unknown): Plan {
  const c = command(m, cmd);
  if (!c) return deny(`${m.id} does not declare a command ${JSON.stringify(cmd)}.`);
  if (!c.pty) return deny(`${c.name} is not declared as a terminal (pty) command.`, 'invalid');
  const list = argList(args);
  if (!list) return deny('Command arguments must be a list of at most 16 strings.', 'invalid');
  const size = (v: unknown, d: number) => (Number.isInteger(v) && (v as number) >= 1 && (v as number) <= 1000 ? (v as number) : d);
  const env = remoteEnv(m, c, envArg);
  if (typeof env === 'object' && env) return env;
  return { kind: 'stream', method: 'plugins.pty', params: { plugin: m.id, command: c.name, args: list, cols: size(cols, 80), rows: size(rows, 24), ...(env ? { env } : {}) }, admin: env ? false : needsAdmin(c, u) };
}
