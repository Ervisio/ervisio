/**
 * Plugin settings as JSON files in ~/.config/linuxadmin/plugins/docker (declared in manifest files.write).
 * Missing or unreadable files give the defaults. Writes to one file are queued so they cannot overlap.
 *
 *   const s = await loadFile('settings');           // Settings
 *   await saveFile('settings', { ...s, stacksDir: '/srv/stacks' });
 *   const [settings, update] = useFile('settings'); // hook, shared between components
 */
import { useEffect, useSyncExternalStore } from 'react';
import { getSdk } from './sdk';

export const CONFIG_DIR = '~/.config/linuxadmin/plugins/docker';

export interface Settings {
  /** Folder that holds managed compose stacks, one sub-folder per stack. */
  stacksDir: string;
  /** Update the home with stats for running containers. */
  liveStats: boolean;
  /** Image of the auto-update container. */
  watchtowerImage: string;
}

export interface Registry {
  id: string;
  /** Registry host, for example ghcr.io or registry.example.com:5000. Docker Hub is docker.io. */
  server: string;
  username: string;
  /** Stored as given; the file is private to the user. */
  password: string;
}
export interface RegistriesFile {
  registries: Registry[];
}

export interface AlertRule {
  id: string;
  kind: 'stopped' | 'restart-loop' | 'unhealthy' | 'cpu' | 'memory' | 'disk';
  enabled: boolean;
  /** What the rule covers. Missing means all containers. Disk rules ignore it. */
  scope?: 'all' | 'stack' | 'container';
  /** Stack (compose project) or container name when scope is not all. */
  target?: string;
  /** cpu and memory: percent. disk: gigabytes used by Docker. */
  threshold?: number;
  /** restart-loop: number of restarts. */
  count?: number;
  /** restart-loop: window in minutes. cpu and memory: minutes the value must stay above the threshold. */
  minutes?: number;
  /** memory: percent of the container limit, or of the host memory. */
  memBasis?: 'limit' | 'host';
  /** Older files: container names. */
  containers?: string[];
}
export interface AlertsFile {
  rules: AlertRule[];
}

export interface AlertHistoryItem {
  id: string;
  /** Unix milliseconds. */
  at: number;
  ruleId: string;
  kind: AlertRule['kind'] | 'test';
  title: string;
  detail: string;
  container?: string;
  containerId?: string;
}
export interface AlertsHistoryFile {
  items: AlertHistoryItem[];
}

export interface AutoUpdateConfig {
  enabled: boolean;
  schedule: { type: 'daily' | 'weekly' | 'custom'; hour: number; minute: number; /** 0 = Sunday */ day: number; cron: string };
  mode: 'all' | 'choose';
  containers: string[];
  cleanup: boolean;
  includeStopped: boolean;
  monitorOnly: boolean;
  rolling: boolean;
  image: string;
}
export interface AutoUpdateFile {
  config: AutoUpdateConfig;
}

export interface TemplateSource {
  id: string;
  name: string;
  /** URL of a Portainer v2 or v3 template JSON. */
  url: string;
  enabled: boolean;
}
export interface TemplateSourcesFile {
  sources: TemplateSource[];
}

export interface FileMap {
  settings: Settings;
  registries: RegistriesFile;
  alerts: AlertsFile;
  'alerts-history': AlertsHistoryFile;
  autoupdate: AutoUpdateFile;
  'templates-sources': TemplateSourcesFile;
}
export type FileName = keyof FileMap;

export const DEFAULTS: { [K in FileName]: FileMap[K] } = {
  settings: { stacksDir: '/opt/stacks', liveStats: true, watchtowerImage: 'nickfedor/watchtower' },
  registries: { registries: [] },
  alerts: { rules: [] },
  'alerts-history': { items: [] },
  autoupdate: {
    config: {
      enabled: false,
      schedule: { type: 'daily', hour: 4, minute: 0, day: 0, cron: '0 0 4 * * *' },
      mode: 'all',
      containers: [],
      cleanup: true,
      includeStopped: false,
      monitorOnly: false,
      rolling: false,
      image: '',
    },
  },
  'templates-sources': { sources: [] },
};

const path = (name: FileName) => `${CONFIG_DIR}/${name}.json`;
const clone = <T>(v: T): T => JSON.parse(JSON.stringify(v));

export async function loadFile<K extends FileName>(name: K): Promise<FileMap[K]> {
  try {
    const text = await getSdk().files.read(path(name));
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return { ...clone(DEFAULTS[name]), ...parsed };
  } catch {
    /* missing or invalid: defaults */
  }
  return clone(DEFAULTS[name]);
}

const queues = new Map<string, Promise<unknown>>();

export function saveFile<K extends FileName>(name: K, value: FileMap[K]): Promise<void> {
  const run = async () => {
    await getSdk().files.write(path(name), JSON.stringify(value, null, 2) + '\n');
  };
  const prev = queues.get(name) ?? Promise.resolve();
  const next = prev.then(run, run);
  queues.set(name, next.catch(() => undefined));
  return next;
}

/* ---------- a tiny cache so components share one copy per file ---------- */

const cache = new Map<string, unknown>();
const loading = new Set<string>();
const listeners = new Map<string, Set<() => void>>();
const notify = (name: string) => listeners.get(name)?.forEach((f) => f());

/** Current value (defaults until the file has loaded) and a function that merges a patch and saves. */
export function useFile<K extends FileName>(name: K): [FileMap[K], (patch: Partial<FileMap[K]>) => Promise<void>, boolean] {
  const subscribe = (f: () => void) => {
    if (!listeners.has(name)) listeners.set(name, new Set());
    listeners.get(name)!.add(f);
    return () => listeners.get(name)!.delete(f);
  };
  const value = useSyncExternalStore(subscribe, () => cache.get(name) as FileMap[K] | undefined);
  useEffect(() => {
    if (cache.has(name) || loading.has(name)) return;
    loading.add(name);
    void loadFile(name).then((v) => {
      cache.set(name, v);
      loading.delete(name);
      notify(name);
    });
  }, [name]);
  const update = async (patch: Partial<FileMap[K]>) => {
    const next = { ...(cache.get(name) ?? DEFAULTS[name]), ...patch } as FileMap[K];
    cache.set(name, next);
    notify(name);
    await saveFile(name, next);
  };
  return [value ?? DEFAULTS[name], update, value !== undefined];
}

/** Loads a file into the shared cache (once) and returns it. For code outside components. */
export async function ensureFile<K extends FileName>(name: K): Promise<FileMap[K]> {
  if (cache.has(name)) return cache.get(name) as FileMap[K];
  const v = await loadFile(name);
  if (!cache.has(name)) {
    cache.set(name, v);
    notify(name);
  }
  return cache.get(name) as FileMap[K];
}

/** Replaces a file's value in the cache and saves it. For code outside components. */
export async function setFile<K extends FileName>(name: K, value: FileMap[K]): Promise<void> {
  cache.set(name, value);
  notify(name);
  await saveFile(name, value);
}

/** Calls `fn` whenever the cached value of a file changes. */
export function subscribeFile(name: FileName, fn: () => void): () => void {
  if (!listeners.has(name)) listeners.set(name, new Set());
  listeners.get(name)!.add(fn);
  return () => listeners.get(name)!.delete(fn);
}
