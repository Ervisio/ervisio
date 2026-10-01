import { ApiError, call, requestUnlock } from '../../api';

let unlockedProbe: () => boolean = () => false;
/** The page tells fapi whether administrator rights are currently unlocked (skips a second password prompt). */
export const setUnlockProbe = (f: () => boolean) => {
  unlockedProbe = f;
};
export const isUnlocked = () => unlockedProbe();

export interface RunOpts {
  /** Run on the root bridge from the start. */
  admin?: boolean;
  /** Called when the call only worked after the user unlocked administrator rights. */
  onElevate?: () => void;
  reason?: string;
  signal?: AbortSignal;
}

/**
 * Calls a files.* method. When the user bridge answers needs_admin the unlock dialog opens and
 * the call is repeated on the root bridge (User methods need admin:true to run as root).
 */
export async function fcall<T = unknown>(method: string, params: unknown, o: RunOpts = {}): Promise<T> {
  try {
    return await call<T>(method, params, { admin: o.admin, noUnlock: true, signal: o.signal });
  } catch (e) {
    if (e instanceof ApiError && e.code === 'needs_admin') {
      if (!unlockedProbe()) await requestUnlock(o.reason ?? method);
      const r = await call<T>(method, params, { admin: true, signal: o.signal });
      if (!o.admin) o.onElevate?.();
      return r;
    }
    throw e;
  }
}

/* ---------- change bus: panes reload when something happened in a folder ---------- */
const subs = new Set<(dir?: string) => void>();
export const onFilesChanged = (fn: (dir?: string) => void) => {
  subs.add(fn);
  return () => void subs.delete(fn);
};
export const emitChanged = (dir?: string) => subs.forEach((f) => f(dir));

/* ---------- small concurrency-limited loader with cache for previews ---------- */
class Loader<V> {
  private cache = new Map<string, V | null>();
  private queue: (() => void)[] = [];
  private running = 0;
  constructor(private limit: number, private max: number) {}
  get(key: string): V | null | undefined {
    return this.cache.get(key);
  }
  load(key: string, fn: () => Promise<V>): Promise<V | null> {
    if (this.cache.has(key)) return Promise.resolve(this.cache.get(key) ?? null);
    return new Promise((resolve) => {
      const run = () => {
        this.running++;
        fn()
          .then((v) => this.set(key, v), () => this.set(key, null))
          .then(() => resolve(this.cache.get(key) ?? null))
          .finally(() => {
            this.running--;
            this.queue.shift()?.();
          });
      };
      if (this.running < this.limit) run();
      else this.queue.push(run);
    });
  }
  private set(key: string, v: V | null) {
    this.cache.set(key, v);
    if (this.cache.size > this.max) this.cache.delete(this.cache.keys().next().value as string);
  }
}

const thumbs = new Loader<string>(3, 400);
const snippets = new Loader<string>(3, 400);

export const cachedThumb = (key: string) => thumbs.get(key);
export const cachedSnippet = (key: string) => snippets.get(key);

export function loadThumb(path: string, stamp: string, admin: boolean): Promise<string | null> {
  return thumbs.load(`${admin ? 'a' : 'u'}:${path}:${stamp}`, async () => {
    const r = await call<{ data: string }>('files.thumbnail', { path, size: 320 }, { admin, noUnlock: true });
    return 'data:image/png;base64,' + r.data;
  });
}

export function loadSnippet(path: string, stamp: string, admin: boolean): Promise<string | null> {
  return snippets.load(`${admin ? 'a' : 'u'}:${path}:${stamp}`, async () => {
    const r = await call<{ content: string }>('files.readText', { path, maxBytes: 1200 }, { admin, noUnlock: true });
    return r.content.split('\n').slice(0, 7).map((l) => l.slice(0, 60)).join('\n');
  });
}

export const thumbKey = (admin: boolean, path: string, stamp: string) => `${admin ? 'a' : 'u'}:${path}:${stamp}`;
