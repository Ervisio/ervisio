/**
 * sdk.api.jobs and sdk.api.notify (SDK 0.2): background jobs and notifications, in the frame. Each call goes to
 * the host broker (brokerJobs.ts) as the ops `jobs` and `notify`.
 */
import { request } from './channel';

type Json = Record<string, unknown>;

const jobs = (action: string, args: Json = {}) => request<Json>('jobs', { action, ...args });

export const jobsApi = {
  create: (o: Json) => jobs('create', o),
  async list(o: { job?: string } = {}): Promise<unknown[]> {
    const r = await jobs('list', o ?? {});
    return (r.instances as unknown[]) ?? [];
  },
  get: (id: string) => jobs('get', { id }),
  update: (id: string, patch: Json = {}) => jobs('update', { ...patch, id }),
  async delete(id: string): Promise<void> {
    await jobs('delete', { id });
  },
  async runNow(id: string): Promise<{ run: string }> {
    return (await jobs('runNow', { id })) as { run: string };
  },
  async history(id: string, limit?: number): Promise<unknown[]> {
    const r = await jobs('history', { id, ...(limit ? { limit } : {}) });
    return (r.runs as unknown[]) ?? [];
  },
  webhooks: {
    create: (id: string, label?: string) => jobs('webhookCreate', { id, ...(label ? { label } : {}) }),
    regenerate: (id: string, webhook: string) => jobs('webhookRegenerate', { id, webhook }),
    async revoke(id: string, webhook: string): Promise<void> {
      await jobs('webhookRevoke', { id, webhook });
    },
  },
};

export const notifyApi = (n: Json) => request<{ channels: number; delivered: number; failed: number }>('notify', n);
