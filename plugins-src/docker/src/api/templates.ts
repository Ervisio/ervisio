/**
 * Template catalog: our own (plugins/docker/templates.json, signed with the plugin) plus Portainer-format lists the
 * user adds. Portainer v2 and v3 JSON are both read. Remote lists are fetched with fetch(); the plugin frame may
 * only reach the hosts in capabilities.network (raw.githubusercontent.com and gist.githubusercontent.com).
 */
import { useCallback, useEffect, useState } from 'react';
import { getSdk } from '../sdk';
import { useFile, type TemplateSource } from '../settings';
import catalogFallback from '../../../../plugins/docker/templates.json?raw';

export type HueName = 'ov' | 'term' | 'file' | 'log' | 'svc' | 'sw' | 'usr' | 'plg';
const HUES: HueName[] = ['ov', 'term', 'file', 'log', 'svc', 'sw', 'usr', 'plg'];

export interface TemplateVar {
  name: string;
  label: string;
  default: string;
  type: 'text' | 'password' | 'port' | 'path' | 'bool';
  required?: boolean;
  hint?: string;
  /** Fill with a random value (passwords). */
  generate?: boolean;
}

export interface ContainerTemplate {
  image: string;
  ports: { host: string; container: string; proto: string }[];
  volumes: { source: string; target: string; readOnly?: boolean }[];
  env: { key: string; value: string }[];
  restart?: string;
  command?: string;
  hostname?: string;
  network?: string;
  privileged?: boolean;
  labels?: { key: string; value: string }[];
}

export interface Template {
  /** Unique over all sources: "<source>:<id>". */
  id: string;
  source: string;
  sourceName: string;
  name: string;
  description: string;
  category: string;
  hue: HueName;
  type: 'container' | 'stack';
  featured: boolean;
  website?: string;
  note?: string;
  needs?: { ports?: (number | string)[]; ram?: string; disk?: string; containers?: number; note?: string };
  variables: TemplateVar[];
  container?: ContainerTemplate;
  /** Compose text, for stacks of our own catalog. */
  compose?: string;
  /** Where to fetch the compose text (raw URL), for Portainer stacks. */
  composeUrl?: string;
  /** Values written to the .env file without asking. */
  fixedEnv?: { key: string; value: string }[];
  /** A Docker Swarm stack that is run as plain compose. */
  swarm?: boolean;
}

export const BUILTIN_ID = 'linuxadmin';
export const DEFAULT_SOURCE: TemplateSource = {
  id: 'portainer',
  name: 'Portainer templates',
  url: 'https://raw.githubusercontent.com/portainer/templates/v3/templates.json',
  enabled: true,
};
/** The hosts of capabilities.network in manifest.json. */
export const ALLOWED_HOSTS = ['raw.githubusercontent.com', 'gist.githubusercontent.com'];

/** The saved list, or the default one when nothing was saved yet. */
export const effectiveSources = (saved: TemplateSource[]): TemplateSource[] => (saved.length ? saved : [DEFAULT_SOURCE]);

export const slug = (s: string): string => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 60) || 'app';

export function hueFor(name: string): HueName {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return HUES[h % HUES.length];
}

/* ---------- values ---------- */

export function randomSecret(len = 22): string {
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789';
  const bytes = new Uint8Array(len);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('');
}

export function initialValues(vars: TemplateVar[]): Record<string, string> {
  const v: Record<string, string> = {};
  for (const x of vars) v[x.name] = x.generate && !x.default ? randomSecret() : x.default;
  return v;
}

/** Replaces ${NAME} with the values. Unknown names become empty. */
export const substitute = (text: string, values: Record<string, string>): string => text.replace(/\$\{([A-Za-z_][A-Za-z0-9_]*)\}/g, (_, n: string) => values[n] ?? '');

export function containerPrefill(c: ContainerTemplate, values: Record<string, string>, name: string) {
  const s = (x: string) => substitute(x, values);
  return {
    name,
    ports: c.ports.map((p) => ({ host: s(p.host), container: s(p.container), proto: p.proto })).filter((p) => p.container),
    volumes: c.volumes.map((v) => ({ source: s(v.source), target: s(v.target), readOnly: v.readOnly })).filter((v) => v.source && v.target),
    env: c.env.map((e) => ({ key: e.key, value: s(e.value) })).filter((e) => e.value !== '' || /^(true|false)$/.test(e.value)),
    restart: c.restart,
    network: c.network,
    hostname: c.hostname,
    command: c.command ? s(c.command) : undefined,
    privileged: c.privileged,
    labels: c.labels,
  };
}

const envQuote = (v: string): string => (/^[A-Za-z0-9_./:@,+-]*$/.test(v) ? v : !v.includes("'") && !/[\r\n]/.test(v) ? `'${v}'` : `"${v.replace(/[\\"$]/g, '\\$&').replace(/\r?\n/g, '\\n')}"`);

/** Text of the .env file of a stack. */
export function envText(values: Record<string, string>, vars: TemplateVar[], fixed: { key: string; value: string }[] = []): string {
  const lines = vars.map((v) => `${v.name}=${envQuote(values[v.name] ?? '')}`);
  for (const f of fixed) if (!vars.some((v) => v.name === f.key)) lines.push(`${f.key}=${envQuote(f.value)}`);
  return lines.join('\n') + '\n';
}

/* ---------- reading lists ---------- */

type Json = Record<string, any>;

const stripHtml = (s: unknown): string => (typeof s === 'string' ? s.replace(/<[^>]*>/g, '').replace(/\s+/g, ' ').trim() : '');

function parseBuiltin(doc: Json): Template[] {
  const out: Template[] = [];
  for (const raw of (doc.templates ?? []) as Json[]) {
    if (!raw.id || !raw.name || (raw.type !== 'container' && raw.type !== 'stack')) continue;
    out.push({
      id: `${BUILTIN_ID}:${raw.id}`,
      source: BUILTIN_ID,
      sourceName: String(doc.name ?? 'LinuxAdmin catalog'),
      name: raw.name,
      description: raw.description ?? '',
      category: raw.category ?? 'Other',
      hue: HUES.includes(raw.hue) ? raw.hue : hueFor(raw.name),
      type: raw.type,
      featured: !!raw.featured,
      website: raw.website,
      needs: raw.needs,
      variables: raw.variables ?? [],
      container: raw.container,
      compose: raw.compose,
    });
  }
  return out;
}

function parsePorts(list: unknown): ContainerTemplate['ports'] {
  const out: ContainerTemplate['ports'] = [];
  for (const entry of Array.isArray(list) ? list : []) {
    // "80/tcp", "8080:80/tcp", "8080:80"
    const m = /^(?:(\d+):)?(\d+)(?:\/(tcp|udp))?$/i.exec(String(entry).trim());
    if (m) out.push({ host: m[1] ?? m[2], container: m[2], proto: (m[3] ?? 'tcp').toLowerCase() });
  }
  return out;
}

function portainerVars(env: unknown): { vars: TemplateVar[]; fixed: { key: string; value: string }[] } {
  const vars: TemplateVar[] = [];
  const fixed: { key: string; value: string }[] = [];
  for (const e of Array.isArray(env) ? (env as Json[]) : []) {
    if (!e?.name) continue;
    const def = e.default !== undefined ? String(e.default) : e.select?.find?.((o: Json) => o.default)?.value ?? e.select?.[0]?.value ?? '';
    if (e.label && !e.preset) {
      const secret = /pass|secret|token|key/i.test(e.name);
      vars.push({ name: e.name, label: e.label, default: def, type: secret ? 'password' : 'text', required: false, hint: stripHtml(e.description) || undefined, generate: secret && !def });
    } else if (def !== '') fixed.push({ key: e.name, value: def });
  }
  return { vars, fixed };
}

export function githubRaw(repoUrl: string, file: string): string | undefined {
  const m = /^https:\/\/github\.com\/([^/]+)\/([^/]+?)(?:\.git)?\/?$/.exec(repoUrl.trim());
  if (!m) return undefined;
  return `https://raw.githubusercontent.com/${m[1]}/${m[2]}/HEAD/${file.replace(/^\/+/, '')}`;
}

function parsePortainer(doc: Json, src: { id: string; name: string }): Template[] {
  const out: Template[] = [];
  const list: Json[] = Array.isArray(doc) ? doc : doc.templates;
  if (!Array.isArray(list)) throw new Error('This is not a Portainer template list (no "templates" array).');
  list.forEach((raw, idx) => {
    const kind = Number(raw?.type);
    if (!raw || !raw.title || ![1, 2, 3].includes(kind)) return;
    if (raw.platform && String(raw.platform).toLowerCase() === 'windows') return;
    const base = {
      id: `${src.id}:${raw.id ?? idx}`,
      source: src.id,
      sourceName: src.name,
      name: String(raw.title),
      description: stripHtml(raw.description),
      category: String(raw.categories?.[0] ?? 'Other').replace(/^./, (c: string) => c.toUpperCase()),
      hue: hueFor(String(raw.title)),
      featured: false,
      note: stripHtml(raw.note) || undefined,
    };
    const { vars, fixed } = portainerVars(raw.env);
    if (kind === 1) {
      if (!raw.image) return;
      const key = slug(raw.title);
      const volumes = (Array.isArray(raw.volumes) ? (raw.volumes as Json[]) : [])
        .filter((v) => v?.container)
        .map((v, n) => ({ source: v.bind ? String(v.bind) : `${key}-${slug(String(v.container).split('/').pop() || `data-${n}`)}`, target: String(v.container), readOnly: !!v.readonly }));
      const env = [
        ...vars.map((v) => ({ key: v.name, value: `\${${v.name}}` })),
        ...fixed,
      ];
      out.push({
        ...base,
        type: 'container',
        variables: vars,
        needs: { ports: parsePorts(raw.ports).map((p) => Number(p.host)) },
        container: {
          image: String(raw.image),
          ports: parsePorts(raw.ports),
          volumes,
          env,
          restart: raw.restart_policy && ['no', 'always', 'unless-stopped', 'on-failure'].includes(raw.restart_policy) ? raw.restart_policy : 'unless-stopped',
          command: raw.command ? String(raw.command) : undefined,
          hostname: raw.hostname,
          network: raw.network && typeof raw.network === 'string' ? raw.network : undefined,
          privileged: !!raw.privileged,
          labels: (Array.isArray(raw.labels) ? (raw.labels as Json[]) : []).filter((l) => l?.name).map((l) => ({ key: String(l.name), value: String(l.value ?? '') })),
        },
      });
    } else {
      const repo = raw.repository;
      const composeUrl = repo?.url && repo?.stackfile ? githubRaw(String(repo.url), String(repo.stackfile)) : undefined;
      out.push({ ...base, type: 'stack', variables: vars, fixedEnv: fixed, composeUrl, swarm: kind === 2, needs: undefined });
    }
  });
  return out;
}

/* ---------- fetching ---------- */

function describeFetchError(url: string, e: unknown): Error {
  let host = '';
  try {
    host = new URL(url).hostname;
  } catch {
    return new Error('This is not a valid address.');
  }
  if (!ALLOWED_HOSTS.includes(host)) return new Error(`This plugin can only reach ${ALLOWED_HOSTS.join(' and ')}, not ${host}.`);
  const msg = (e as Error)?.message ?? String(e);
  return new Error(/failed to fetch|networkerror|load failed/i.test(msg) ? `Could not reach ${host}. Check the internet connection of this machine.` : msg);
}

export async function fetchText(url: string): Promise<string> {
  let host = '';
  try {
    host = new URL(url).hostname;
  } catch {
    throw new Error('This is not a valid address.');
  }
  if (!url.startsWith('https://') || !ALLOWED_HOSTS.includes(host)) throw describeFetchError(url, null);
  let r: Response;
  try {
    r = await fetch(url, { credentials: 'omit', cache: 'no-store' });
  } catch (e) {
    throw describeFetchError(url, e);
  }
  if (!r.ok) throw new Error(r.status === 404 ? `${host} answered "not found" for this address.` : `${host} answered ${r.status}.`);
  return r.text();
}

/** The compose text of a stack template. */
export async function composeOf(t: Template): Promise<string> {
  if (t.compose) return t.compose;
  if (!t.composeUrl) throw new Error('This template has no compose file this plugin can fetch. Only GitHub repositories are supported.');
  return fetchText(t.composeUrl);
}

async function loadBuiltin(): Promise<Template[]> {
  try {
    const url = await getSdk().asset('templates.json');
    const r = await fetch(url);
    if (r.ok) return parseBuiltin(await r.json());
  } catch {
    /* the frame may not fetch blob: URLs: use the copy compiled into the plugin */
  }
  return parseBuiltin(JSON.parse(catalogFallback) as Json);
}

const remote = new Map<string, Promise<Template[]>>();

function loadRemote(src: TemplateSource): Promise<Template[]> {
  let p = remote.get(src.url);
  if (!p) {
    p = fetchText(src.url).then((text) => {
      let doc: Json;
      try {
        doc = JSON.parse(text);
      } catch {
        throw new Error('The address did not return JSON.');
      }
      return parsePortainer(doc, { id: src.id, name: src.name });
    });
    remote.set(src.url, p);
    p.catch(() => remote.delete(src.url));
  }
  return p;
}

let builtin: Promise<Template[]> | undefined;

export interface SourceStatus {
  id: string;
  name: string;
  count: number;
  error?: string;
}

export interface TemplateCatalog {
  templates: Template[];
  sources: SourceStatus[];
  loading: boolean;
  reload(): void;
}

/** All templates of the enabled sources. Our own catalog loads first; remote lists arrive when they can. */
export function useTemplates(): TemplateCatalog {
  const [file] = useFile('templates-sources');
  const [state, setState] = useState<{ templates: Template[]; sources: SourceStatus[]; loading: boolean }>({ templates: [], sources: [], loading: true });
  const [tick, setTick] = useState(0);
  const key = JSON.stringify(effectiveSources(file.sources).map((s) => [s.id, s.url, s.enabled]));

  useEffect(() => {
    let live = true;
    const sources = effectiveSources(file.sources).filter((s) => s.enabled);
    if (tick > 0) {
      for (const s of sources) remote.delete(s.url);
      builtin = undefined;
    }
    (async () => {
      builtin ??= loadBuiltin();
      const own = await builtin;
      if (!live) return;
      const status: SourceStatus[] = [{ id: BUILTIN_ID, name: own[0]?.sourceName ?? 'LinuxAdmin catalog', count: own.length }];
      let all = [...own];
      setState({ templates: all, sources: [...status], loading: sources.length > 0 });
      await Promise.all(
        sources.map(async (s) => {
          try {
            const list = await loadRemote(s);
            status.push({ id: s.id, name: s.name, count: list.length });
            all = [...all, ...list];
          } catch (e) {
            status.push({ id: s.id, name: s.name, count: 0, error: (e as Error).message });
          }
          if (live) setState({ templates: all, sources: [...status], loading: true });
        }),
      );
      if (live) setState({ templates: all, sources: [...status], loading: false });
    })();
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, tick]);

  const reload = useCallback(() => setTick((n) => n + 1), []);
  return { ...state, reload };
}
