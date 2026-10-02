/**
 * "Is there a newer image?" For each tagged image: ask the registry (through the Engine's /distribution endpoint) for
 * the digest of the tag and compare it with the digests the local copy came from. Results live in memory only.
 */
import { useSyncExternalStore } from 'react';
import { docker } from '../../api/engine';
import { registryAuthFor } from '../../api/registries';
import type { ImageSummary } from '../../api/types';
import { realTags, refPath } from './imageRef';

export type UpdateState =
  | { kind: 'current'; at: number }
  | { kind: 'newer'; at: number }
  /** Built or loaded locally: no registry digest to compare. */
  | { kind: 'local'; at: number }
  | { kind: 'error'; at: number; message: string };

const cache = new Map<string, UpdateState>();
const pending = new Set<string>();
const subs = new Set<() => void>();
let version = 0;
const bump = () => {
  version++;
  subs.forEach((f) => f());
};

export const FRESH_MS = 30 * 60 * 1000;

export function useUpdates(): { get(ref: string): UpdateState | undefined; checking: number; lastAt: number } {
  useSyncExternalStore(
    (f) => {
      subs.add(f);
      return () => subs.delete(f);
    },
    () => version,
  );
  let lastAt = 0;
  cache.forEach((v) => (lastAt = Math.max(lastAt, v.at)));
  return { get: (ref) => cache.get(ref), checking: pending.size, lastAt };
}

/** Checks one reference ("repo:tag") against its registry. */
export async function checkOne(ref: string, img: Pick<ImageSummary, 'RepoDigests'>): Promise<UpdateState> {
  const repo = ref.replace(/:[^/:]*$/, '');
  const local = (img.RepoDigests ?? []).filter((d) => d.startsWith(repo + '@')).map((d) => d.slice(repo.length + 1));
  const at = Date.now();
  let state: UpdateState;
  if (!local.length) state = { kind: 'local', at };
  else {
    try {
      const auth = await registryAuthFor(ref);
      const d = await docker.json<{ Descriptor?: { digest?: string } }>('GET', `/distribution/${refPath(ref)}/json`, { headers: auth ? { 'X-Registry-Auth': auth } : undefined });
      const remote = d?.Descriptor?.digest;
      state = !remote ? { kind: 'error', at, message: 'No digest in the answer' } : local.includes(remote) ? { kind: 'current', at } : { kind: 'newer', at };
    } catch (e) {
      state = { kind: 'error', at, message: (e as Error).message };
    }
  }
  cache.set(ref, state);
  return state;
}

/** Checks every tagged image, three at a time. Skips results younger than FRESH_MS unless `force`. */
export async function checkAll(images: ImageSummary[], force = false): Promise<void> {
  const jobs: { ref: string; img: ImageSummary }[] = [];
  for (const img of images) for (const ref of realTags(img.RepoTags)) {
    const c = cache.get(ref);
    if (!force && c && Date.now() - c.at < FRESH_MS) continue;
    if (pending.has(ref)) continue;
    jobs.push({ ref, img });
  }
  jobs.forEach((j) => pending.add(j.ref));
  bump();
  const worker = async () => {
    for (let j = jobs.shift(); j; j = jobs.shift()) {
      await checkOne(j.ref, j.img);
      pending.delete(j.ref);
      bump();
    }
  };
  await Promise.all([worker(), worker(), worker()]);
}

/** Forget a result (after the image was pulled or removed). */
export function forget(ref: string): void {
  cache.delete(ref);
  bump();
}

export async function recheck(ref: string, img: ImageSummary): Promise<void> {
  pending.add(ref);
  bump();
  await checkOne(ref, img);
  pending.delete(ref);
  bump();
}
