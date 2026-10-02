/** The new-container form: its state, conversion to and from the Engine's create body, validation, `docker run` text. */
import type { ContainerInspect, Container } from '../../api/types';
import { publishedPorts } from '../../api/model';
import { containerName } from '../../api/format';
import type { CreatePrefill } from '../../router';

let seq = 0;
export const rid = (): string => `r${++seq}`;

export interface PortRow {
  id: string;
  /** Host side: "8080", "127.0.0.1:8080" or empty for a random port. */
  host: string;
  container: string;
  proto: 'tcp' | 'udp';
}
export interface MountRow {
  id: string;
  kind: 'volume' | 'bind';
  source: string;
  target: string;
  ro: boolean;
}
export interface KV {
  id: string;
  key: string;
  value: string;
}
export type Restart = 'no' | 'always' | 'unless-stopped' | 'on-failure';

export interface Spec {
  image: string;
  name: string;
  restart: Restart;
  ports: PortRow[];
  mounts: MountRow[];
  env: KV[];
  network: string;
  hostname: string;
  memoryMb: string;
  cpus: string;
  labels: KV[];
  command: string;
  user: string;
  privileged: boolean;
  /** Pull the image again before creating (always happens when it is not on this host). */
  pullFirst: boolean;
  start: boolean;
}

/** What a recreate keeps from the old container and the image it came from. */
export interface Base {
  inspect: ContainerInspect;
  imageConfig?: { Env?: string[] | null; Cmd?: string[] | null; Entrypoint?: string[] | null; Labels?: Record<string, string> | null; WorkingDir?: string; Healthcheck?: unknown };
  extraNetworks: string[];
  wasRunning: boolean;
}

export const emptySpec = (): Spec => ({
  image: '',
  name: '',
  restart: 'unless-stopped',
  ports: [],
  mounts: [],
  env: [],
  network: 'bridge',
  hostname: '',
  memoryMb: '',
  cpus: '',
  labels: [],
  command: '',
  user: '',
  privileged: false,
  pullFirst: false,
  start: true,
});

export const kv = (key = '', value = ''): KV => ({ id: rid(), key, value });

export function specFromPrefill(image: string | undefined, p: CreatePrefill | undefined): Spec {
  const s = emptySpec();
  if (image) s.image = image;
  if (!p) return s;
  if (p.name) s.name = p.name;
  if (p.restart && ['no', 'always', 'unless-stopped', 'on-failure'].includes(p.restart)) s.restart = p.restart as Restart;
  s.ports = (p.ports ?? []).map((x) => ({ id: rid(), host: x.host, container: x.container, proto: x.proto === 'udp' ? 'udp' : 'tcp' }));
  s.mounts = (p.volumes ?? []).map((v) => ({ id: rid(), kind: v.source.startsWith('/') || v.source.startsWith('.') || v.source.startsWith('~') ? 'bind' : 'volume', source: v.source, target: v.target, ro: !!v.readOnly }));
  s.env = (p.env ?? []).map((e) => kv(e.key, e.value));
  s.labels = (p.labels ?? []).map((e) => kv(e.key, e.value));
  if (p.network) s.network = p.network;
  if (p.hostname) s.hostname = p.hostname;
  if (p.command) s.command = p.command;
  if (p.user) s.user = p.user;
  if (p.privileged) s.privileged = true;
  if (p.memoryMb) s.memoryMb = p.memoryMb;
  if (p.cpus) s.cpus = p.cpus;
  return s;
}

/* ---------- command line text <-> array ---------- */

/** Splits a command like a shell would (quotes and backslashes), without any expansion. */
export function splitCommand(text: string): string[] {
  const out: string[] = [];
  let cur = '';
  let q: '"' | "'" | '' = '';
  let has = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (q) {
      if (ch === q) q = '';
      else if (ch === '\\' && q === '"' && i + 1 < text.length && '"\\$`'.includes(text[i + 1])) cur += text[++i];
      else cur += ch;
    } else if (ch === '"' || ch === "'") {
      q = ch;
      has = true;
    } else if (ch === '\\' && i + 1 < text.length) {
      cur += text[++i];
      has = true;
    } else if (/\s/.test(ch)) {
      if (cur || has) out.push(cur);
      cur = '';
      has = false;
    } else cur += ch;
  }
  if (cur || has) out.push(cur);
  return out;
}

const SAFE = /^[A-Za-z0-9_@%+=:,./-]+$/;
export const shQuote = (s: string): string => (s === '' ? "''" : SAFE.test(s) ? s : `'${s.replace(/'/g, `'\\''`)}'`);
export const joinCommand = (a: string[]): string => a.map(shQuote).join(' ');

/* ---------- from an existing container ---------- */

const same = (a: unknown, b: unknown) => JSON.stringify(a ?? null) === JSON.stringify(b ?? null);

export function specFromInspect(ins: ContainerInspect, imageConfig: Base['imageConfig']): { spec: Spec; base: Base } {
  const hc = ins.HostConfig;
  const cfg = ins.Config;
  const imgEnv = new Set(imageConfig?.Env ?? []);
  const imgLabels = imageConfig?.Labels ?? {};
  const ports: PortRow[] = [];
  for (const [key, list] of Object.entries((hc.PortBindings ?? {}) as Record<string, { HostIp?: string; HostPort?: string }[] | null>)) {
    const [cport, proto] = key.split('/');
    for (const b of list ?? []) {
      const ip = b.HostIp && b.HostIp !== '0.0.0.0' && b.HostIp !== '::' ? `${b.HostIp}:` : '';
      ports.push({ id: rid(), host: `${ip}${b.HostPort ?? ''}`, container: cport, proto: proto === 'udp' ? 'udp' : 'tcp' });
    }
  }
  const mounts: MountRow[] = ins.Mounts.filter((m) => m.Type === 'bind' || m.Type === 'volume').map((m) => ({
    id: rid(),
    kind: m.Type === 'bind' ? 'bind' : 'volume',
    source: m.Type === 'bind' ? m.Source : (m.Name ?? m.Source),
    target: m.Destination,
    ro: !m.RW,
  }));
  const env = (cfg.Env ?? []).filter((e) => !imgEnv.has(e)).map((e) => {
    const i = e.indexOf('=');
    return kv(i < 0 ? e : e.slice(0, i), i < 0 ? '' : e.slice(i + 1));
  });
  const labels = Object.entries(cfg.Labels ?? {})
    .filter(([k, v]) => imgLabels[k] !== v)
    .map(([k, v]) => kv(k, v));
  const mode = hc.NetworkMode && hc.NetworkMode !== 'default' ? hc.NetworkMode : 'bridge';
  const nets = Object.keys(ins.NetworkSettings.Networks ?? {});
  const extraNetworks = nets.filter((n) => n !== mode && !(mode === 'bridge' && n === 'bridge'));
  const policy = hc.RestartPolicy?.Name || 'no';
  const spec: Spec = {
    image: cfg.Image,
    name: ins.Name.replace(/^\//, ''),
    restart: (['no', 'always', 'unless-stopped', 'on-failure'].includes(policy) ? policy : 'no') as Restart,
    ports,
    mounts,
    env,
    network: mode,
    hostname: cfg.Hostname && cfg.Hostname !== ins.Id.slice(0, 12) ? cfg.Hostname : '',
    memoryMb: hc.Memory ? String(Math.round(hc.Memory / 1048576)) : '',
    cpus: hc.NanoCpus ? String(hc.NanoCpus / 1e9) : '',
    labels,
    command: cfg.Cmd && !same(cfg.Cmd, imageConfig?.Cmd) ? joinCommand(cfg.Cmd) : '',
    user: cfg.User ?? '',
    privileged: !!hc.Privileged,
    pullFirst: false,
    start: ins.State.Running || ins.State.Restarting,
  };
  return { spec, base: { inspect: ins, imageConfig, extraNetworks, wasRunning: ins.State.Running || ins.State.Restarting } };
}

/* ---------- to the Engine ---------- */

export const NO_PORTS_MODES = (n: string): boolean => n === 'host' || n === 'none' || n.startsWith('container:');

export function parseHostPort(s: string): { ip: string; port: string } {
  const t = s.trim();
  const i = t.lastIndexOf(':');
  return i < 0 ? { ip: '', port: t } : { ip: t.slice(0, i).replace(/^\[|\]$/g, ''), port: t.slice(i + 1) };
}

export function envLine(e: KV): string {
  return `${e.key.trim()}=${e.value}`;
}

export function createBody(spec: Spec, base?: Base): Record<string, any> {
  const bhc: Record<string, any> = base ? { ...base.inspect.HostConfig } : {};
  const cfg: Record<string, any> = base ? { ...base.inspect.Config } : {};
  const img = base?.imageConfig;
  // Things the image already provides are left out so a newer image can change them.
  if (same(cfg.Entrypoint, img?.Entrypoint)) delete cfg.Entrypoint;
  if (same(cfg.WorkingDir, img?.WorkingDir)) delete cfg.WorkingDir;
  if (same(cfg.Healthcheck, img?.Healthcheck)) delete cfg.Healthcheck;
  delete cfg.Volumes;
  delete cfg.Hostname;
  delete cfg.ExposedPorts;
  delete cfg.Cmd;

  const noPorts = NO_PORTS_MODES(spec.network);
  const exposed: Record<string, object> = {};
  const bindings: Record<string, { HostIp: string; HostPort: string }[]> = {};
  if (!noPorts) {
    for (const p of spec.ports) {
      if (!p.container.trim()) continue;
      const key = `${p.container.trim()}/${p.proto}`;
      exposed[key] = {};
      const hp = parseHostPort(p.host);
      (bindings[key] ??= []).push({ HostIp: hp.ip, HostPort: hp.port });
    }
  }
  const mem = spec.memoryMb.trim() ? Math.round(parseFloat(spec.memoryMb) * 1048576) : 0;
  const hc: Record<string, any> = {
    ...bhc,
    Binds: spec.mounts.filter((m) => m.source.trim() && m.target.trim()).map((m) => `${m.source.trim()}:${m.target.trim()}${m.ro ? ':ro' : ''}`),
    PortBindings: bindings,
    NetworkMode: spec.network === 'bridge' ? 'bridge' : spec.network,
    RestartPolicy: { Name: spec.restart === 'no' ? '' : spec.restart, MaximumRetryCount: spec.restart === 'on-failure' ? (bhc.RestartPolicy?.MaximumRetryCount || 5) : 0 },
    Memory: mem,
    NanoCpus: spec.cpus.trim() ? Math.round(parseFloat(spec.cpus) * 1e9) : 0,
    Privileged: spec.privileged,
  };
  if (bhc.Memory !== mem) hc.MemorySwap = 0;
  if (Array.isArray(bhc.Mounts)) {
    const keep = bhc.Mounts.filter((m: { Type?: string }) => m.Type === 'tmpfs');
    if (keep.length) hc.Mounts = keep;
    else delete hc.Mounts;
  }
  const body: Record<string, any> = {
    ...cfg,
    Image: spec.image.trim(),
    Env: spec.env.filter((e) => e.key.trim()).map(envLine),
    Labels: Object.fromEntries(spec.labels.filter((l) => l.key.trim()).map((l) => [l.key.trim(), l.value])),
    HostConfig: hc,
  };
  if (Object.keys(exposed).length) body.ExposedPorts = exposed;
  if (spec.hostname.trim() && spec.network !== 'host' && !spec.network.startsWith('container:')) body.Hostname = spec.hostname.trim();
  if (spec.command.trim()) body.Cmd = splitCommand(spec.command);
  if (spec.user.trim()) body.User = spec.user.trim();
  else delete body.User;
  if (!['bridge', 'host', 'none', 'default'].includes(spec.network) && !spec.network.startsWith('container:')) {
    body.NetworkingConfig = { EndpointsConfig: { [spec.network]: {} } };
  }
  return body;
}

/* ---------- docker run text ---------- */

export const isSecretKey = (k: string): boolean => /pass|secret|token|key|pwd|credential/i.test(k);

export function runText(spec: Spec, opts: { mask?: boolean } = {}): string {
  const mask = opts.mask !== false;
  const parts: string[] = ['docker run -d'];
  const add = (s: string) => parts.push(s);
  if (spec.name.trim()) add(`--name ${shQuote(spec.name.trim())}`);
  if (spec.restart !== 'no') add(`--restart ${spec.restart}`);
  if (spec.network !== 'bridge') add(`--network ${shQuote(spec.network)}`);
  if (spec.hostname.trim()) add(`--hostname ${shQuote(spec.hostname.trim())}`);
  if (!NO_PORTS_MODES(spec.network)) {
    for (const p of spec.ports) {
      if (!p.container.trim()) continue;
      add(`-p ${p.host.trim() ? `${p.host.trim()}:` : ''}${p.container.trim()}${p.proto === 'udp' ? '/udp' : ''}`);
    }
  }
  for (const m of spec.mounts) if (m.source.trim() && m.target.trim()) add(`-v ${shQuote(`${m.source.trim()}:${m.target.trim()}${m.ro ? ':ro' : ''}`)}`);
  for (const e of spec.env) if (e.key.trim()) add(`-e ${shQuote(`${e.key.trim()}=${mask && isSecretKey(e.key) && e.value ? '********' : e.value}`)}`);
  if (spec.memoryMb.trim()) add(`--memory ${spec.memoryMb.trim()}m`);
  if (spec.cpus.trim()) add(`--cpus ${spec.cpus.trim()}`);
  for (const l of spec.labels) if (l.key.trim()) add(`-l ${shQuote(`${l.key.trim()}=${l.value}`)}`);
  if (spec.user.trim()) add(`--user ${shQuote(spec.user.trim())}`);
  if (spec.privileged) add('--privileged');
  add(spec.image.trim() || '<image>');
  if (spec.command.trim()) add(spec.command.trim());
  return parts.join(' \\\n  ');
}

/* ---------- checks ---------- */

export const NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/;
export const ENV_RE = /^[A-Za-z_][A-Za-z0-9_.-]*$/;

export const validPort = (s: string): boolean => /^\d+$/.test(s) && +s >= 1 && +s <= 65535;
export const validHost = (s: string): boolean => {
  const t = s.trim();
  if (!t) return true;
  const { port } = parseHostPort(t);
  return validPort(port);
};

/** "port/proto" -> container name, for running containers. `except` is the container being recreated. */
export function usedHostPorts(list: Container[] | undefined, except?: string): Map<string, string> {
  const m = new Map<string, string>();
  for (const c of list ?? []) {
    if (c.Id === except || !['running', 'restarting', 'paused'].includes(c.State)) continue;
    for (const p of publishedPorts(c)) m.set(`${p.host}/${p.proto}`, containerName(c));
  }
  return m;
}

export interface PortIssue {
  kind: 'conflict' | 'duplicate' | 'invalid';
  /** The container that has the port, for conflicts. */
  by?: string;
}

export function portIssue(rows: PortRow[], i: number, used: Map<string, string>): PortIssue | undefined {
  const p = rows[i];
  if (p.container.trim() && !validPort(p.container.trim())) return { kind: 'invalid' };
  if (!validHost(p.host)) return { kind: 'invalid' };
  const hp = parseHostPort(p.host).port;
  if (!hp) return undefined;
  if (rows.some((o, j) => j < i && parseHostPort(o.host).port === hp && o.proto === p.proto)) return { kind: 'duplicate' };
  const by = used.get(`${hp}/${p.proto}`);
  return by ? { kind: 'conflict', by } : undefined;
}

export interface Problems {
  image?: string;
  name?: string;
  ports?: string;
  mounts?: string;
  env?: string;
  limits?: string;
}

/** Empty object when the form can be submitted. Values are i18n keys. */
export function problems(spec: Spec, ctx: { names: Set<string>; used: Map<string, string>; recreate: boolean }): Problems {
  const p: Problems = {};
  const img = spec.image.trim();
  if (!img) p.image = 'create.err.image';
  else if (/\s/.test(img)) p.image = 'create.err.imageSpace';
  const n = spec.name.trim();
  if (!n && ctx.recreate) p.name = 'create.err.nameNeeded';
  else if (n && !NAME_RE.test(n)) p.name = 'create.err.nameBad';
  else if (n && ctx.names.has(n)) p.name = 'create.err.nameTaken';
  if (spec.ports.some((_, i) => portIssue(spec.ports, i, ctx.used)?.kind === 'invalid' || (!spec.ports[i].container.trim() && spec.ports[i].host.trim()))) p.ports = 'create.err.port';
  else if (spec.ports.some((_, i) => portIssue(spec.ports, i, ctx.used)?.kind === 'duplicate')) p.ports = 'create.err.portDup';
  if (spec.mounts.some((m) => (m.source.trim() || m.target.trim()) && (!m.source.trim() || !m.target.trim() || !m.target.trim().startsWith('/') || (m.kind === 'bind' && !m.source.trim().startsWith('/'))))) p.mounts = 'create.err.mount';
  if (spec.env.some((e) => (e.key.trim() || e.value) && !ENV_RE.test(e.key.trim()))) p.env = 'create.err.env';
  const m = spec.memoryMb.trim();
  const c = spec.cpus.trim();
  if ((m && !(parseFloat(m) >= 6)) || (c && !(parseFloat(c) > 0)) || (m && !/^\d+(\.\d+)?$/.test(m)) || (c && !/^\d+(\.\d+)?$/.test(c))) p.limits = 'create.err.limits';
  return p;
}

/* ---------- .env text ---------- */

export function parseDotenv(text: string): { key: string; value: string }[] {
  const out: { key: string; value: string }[] = [];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith('#')) continue;
    const m = /^(?:export\s+)?([A-Za-z_][A-Za-z0-9_.-]*)\s*=\s*(.*)$/.exec(line);
    if (!m) continue;
    let v = m[2];
    if ((v.startsWith('"') && v.endsWith('"') && v.length >= 2) || (v.startsWith("'") && v.endsWith("'") && v.length >= 2)) v = v.slice(1, -1);
    else v = v.replace(/\s+#.*$/, '');
    out.push({ key: m[1], value: v });
  }
  return out;
}

/** Path of an image reference for the Engine: each part encoded, the slashes kept (the rules forbid %2F). */
export const imagePath = (ref: string): string => ref.split('/').map(encodeURIComponent).join('/');
