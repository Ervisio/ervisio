/** Container actions shared by the home, the container page and bulk operations. */
import { docker } from './engine';
import { containers } from './resources';

export type ContainerAction = 'start' | 'stop' | 'restart' | 'kill' | 'pause' | 'unpause';

/** POST /containers/{id}/{action}. A 304 (already in that state) is not an error. */
export async function containerAction(id: string, action: ContainerAction, opts?: { timeoutSec?: number }): Promise<void> {
  const r = await docker.request('POST', `/containers/${encodeURIComponent(id)}/${action}`, {
    query: opts?.timeoutSec !== undefined ? { t: String(opts.timeoutSec) } : undefined,
  });
  if (r.status >= 400) {
    let msg = '';
    try {
      msg = r.json().message;
    } catch {
      /* ignore */
    }
    throw new Error(msg || `Docker answered ${r.status}`);
  }
  void containers.refresh();
}

/** DELETE /containers/{id}. `force` removes a running container; `volumes` also removes its anonymous volumes. */
export async function removeContainer(id: string, opts: { force?: boolean; volumes?: boolean } = {}): Promise<void> {
  await docker.delete(`/containers/${encodeURIComponent(id)}`, { force: opts.force ? '1' : '0', v: opts.volumes ? '1' : '0' });
  void containers.refresh();
}

/** Runs `fn` over `ids` with a small parallel limit; returns the ids that failed with their messages. */
export async function runBulk(ids: string[], fn: (id: string) => Promise<void>, limit = 4): Promise<{ id: string; message: string }[]> {
  const failed: { id: string; message: string }[] = [];
  let next = 0;
  const worker = async () => {
    while (next < ids.length) {
      const id = ids[next++];
      try {
        await fn(id);
      } catch (e) {
        failed.push({ id, message: (e as Error).message });
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, ids.length) }, worker));
  return failed;
}
