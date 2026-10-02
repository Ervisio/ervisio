/**
 * Broker policy of the background-job and notification operations (SDK: sdk.api.jobs, sdk.api.notify). Kept apart
 * from broker.ts, which calls authorizeJobsOp for the ops `jobs` and `notify`.
 *
 * What a plugin can ask: create, list, change, delete, run and read the history of ITS OWN job instances (the
 * jobs its manifest declares in capabilities.jobs), manage their webhook URLs, and send a notification when
 * capabilities.notify is true. Never admin, and never an approval: an instance whose job runs steps as root is
 * created waiting for an administrator, who approves it in Settings > Plugin jobs (jobs.approve, which no plugin
 * can reach); confirmAdmin from the frame is dropped. The daemon runs the instance as its creator.
 * The daemon checks all of this again (plugins.jobs.*, plugins.notify).
 */
import type { BrokerManifest, Plan } from './broker';

const deny = (message: string, code: 'forbidden' | 'invalid' = 'invalid'): Plan => ({ kind: 'deny', code, message });

const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const str = (v: unknown, max: number): string | null => (typeof v === 'string' && v.length <= max ? v : null);

/** An object whose values are all short strings (job params). */
function strMap(v: unknown): Record<string, string> | null {
  if (!isObj(v)) return null;
  const out: Record<string, string> = {};
  const entries = Object.entries(v);
  if (entries.length > 16) return null;
  for (const [k, x] of entries) {
    if (k.length > 40 || typeof x !== 'string' || x.length > 1024) return null;
    out[k] = x;
  }
  return out;
}

/** A schedule {every} or {at, days}; the daemon validates the values. */
function schedule(v: unknown): Record<string, unknown> | null {
  if (!isObj(v)) return null;
  const out: Record<string, unknown> = {};
  if (v.every !== undefined) {
    if (typeof v.every !== 'number') return null;
    out.every = v.every;
  }
  if (v.at !== undefined) {
    if (!Array.isArray(v.at) || v.at.length > 24 || !v.at.every((x) => typeof x === 'string' && x.length <= 5)) return null;
    out.at = v.at;
  }
  if (v.days !== undefined) {
    if (!Array.isArray(v.days) || v.days.length > 7 || !v.days.every((x) => typeof x === 'number')) return null;
    out.days = v.days;
  }
  return out;
}

const ACTIONS: Record<string, string> = {
  create: 'plugins.jobs.create',
  list: 'plugins.jobs.list',
  get: 'plugins.jobs.get',
  update: 'plugins.jobs.update',
  delete: 'plugins.jobs.delete',
  runNow: 'plugins.jobs.runNow',
  history: 'plugins.jobs.history',
  webhookCreate: 'plugins.jobs.webhooks.create',
  webhookRegenerate: 'plugins.jobs.webhooks.regenerate',
  webhookRevoke: 'plugins.jobs.webhooks.revoke',
};

const ID = /^[a-f0-9]{8}$/;

export function authorizeJobsOp(m: BrokerManifest, op: string, a: Record<string, unknown>): Plan {
  if (op === 'notify') {
    if (!m.capabilities?.notify) return deny(`${m.id} does not declare capabilities.notify.`, 'forbidden');
    const title = str(a.title, 1000);
    if (!title) return deny('A notification needs a title.');
    const params: Record<string, unknown> = { plugin: m.id, title };
    if (a.body !== undefined) {
      const body = str(a.body, 20000);
      if (body === null) return deny('The body must be text.');
      params.body = body;
    }
    if (a.level !== undefined) {
      if (a.level !== 'info' && a.level !== 'success' && a.level !== 'warn' && a.level !== 'error') return deny('The level must be info, success, warn or error.');
      params.level = a.level;
    }
    if (a.link !== undefined) {
      const link = str(a.link, 500);
      if (link === null) return deny('The link must be text.');
      params.link = link;
    }
    return { kind: 'call', method: 'plugins.notify', params, admin: false };
  }

  const action = typeof a.action === 'string' ? a.action : '';
  const method = Object.prototype.hasOwnProperty.call(ACTIONS, action) ? ACTIONS[action] : undefined;
  if (!method) return deny(`Plugins cannot use jobs.${JSON.stringify(action)}.`);
  const jobs = m.capabilities?.jobs ?? [];
  if (!jobs.length) return deny(`${m.id} declares no jobs (capabilities.jobs).`, 'forbidden');
  const params: Record<string, unknown> = { plugin: m.id };

  if (action === 'list' || action === 'create') {
    if (action === 'create' || a.job !== undefined) {
      const job = typeof a.job === 'string' ? jobs.find((j) => j.name === a.job) : undefined;
      if (!job) return deny(`${m.id} does not declare the job ${JSON.stringify(a.job)}.`, 'forbidden');
      params.job = job.name;
    }
  } else {
    const id = str(a.id, 8);
    if (!id || !ID.test(id)) return deny('Give the id of a job instance.');
    params.id = id;
  }

  if (action === 'create' || action === 'update') {
    if (a.name !== undefined) {
      const name = str(a.name, 60);
      if (name === null) return deny('The name must be text of at most 60 characters.');
      params.name = name;
    }
    if (a.params !== undefined) {
      const p = strMap(a.params);
      if (!p) return deny('params must be an object of short strings.');
      params.params = p;
    }
    if (a.schedule !== undefined) {
      if (a.schedule === null && action === 'update') params.schedule = null;
      else {
        const s = schedule(a.schedule);
        if (!s) return deny('schedule is {every: seconds} or {at: ["HH:MM"], days: [0-6]}.');
        params.schedule = s;
      }
    }
    if (a.enabled !== undefined) {
      if (typeof a.enabled !== 'boolean') return deny('enabled must be true or false.');
      params.enabled = a.enabled;
    }
    // confirmAdmin is never passed on: a plugin cannot approve a job that runs steps as root. Such an instance
    // waits for an administrator, who approves it in Settings > Plugin jobs (jobs.approve, admin level).
    if (action === 'create' && a.runAs !== undefined) {
      const runAs = str(a.runAs, 64);
      if (runAs === null) return deny('runAs must be a user name.');
      params.runAs = runAs;
    }
  }
  if (action === 'history' && a.limit !== undefined) {
    if (!Number.isInteger(a.limit) || (a.limit as number) < 1 || (a.limit as number) > 100) return deny('limit is a number from 1 to 100.');
    params.limit = a.limit;
  }
  if (action === 'webhookCreate' && a.label !== undefined) {
    const label = str(a.label, 60);
    if (label === null) return deny('The label must be text of at most 60 characters.');
    params.label = label;
  }
  if (action === 'webhookRegenerate' || action === 'webhookRevoke') {
    const w = str(a.webhook, 8);
    if (!w || !ID.test(w)) return deny('Give the id of a webhook.');
    params.webhook = w;
  }
  return { kind: 'call', method, params, admin: false };
}
