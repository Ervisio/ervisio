import { call } from '../../api';

/* Notification channels (Settings > Notification channels): daemon methods notify.*, docs/api/notify.md. */

export type ChannelType = 'email' | 'telegram' | 'webhook' | 'ntfy' | 'gotify';
export type ChannelEvent = 'alerts' | 'updates' | 'plugins' | 'jobs';
export type ChannelLevel = 'info' | 'warn' | 'error';

/** A channel as the server returns it: no secrets, only flags saying which ones are set. */
export interface Channel {
  id: string;
  name: string;
  type: ChannelType;
  enabled: boolean;
  minLevel: ChannelLevel;
  events: ChannelEvent[];
  host?: string;
  port?: number;
  security?: 'none' | 'starttls' | 'tls';
  username?: string;
  from?: string;
  to?: string[];
  chatId?: string;
  method?: string;
  template?: string;
  headerName?: string;
  server?: string;
  topic?: string;
  urlHint?: string;
  secrets: Record<string, boolean>;
  last?: { at: number; ok: boolean; error?: string };
}

/** What the form submits; a secret left empty keeps the stored one. */
export interface ChannelInput {
  id?: string;
  name: string;
  type: ChannelType;
  enabled: boolean;
  minLevel: ChannelLevel;
  events: ChannelEvent[];
  host?: string;
  port?: number;
  security?: string;
  username?: string;
  password?: string;
  from?: string;
  to?: string[];
  botToken?: string;
  chatId?: string;
  url?: string;
  method?: string;
  template?: string;
  headerName?: string;
  headerValue?: string;
  server?: string;
  topic?: string;
  token?: string;
}

const quiet = { noUnlock: true } as const;

export const listChannels = () => call<{ channels: Channel[]; file: string }>('notify.list', {}, quiet);
export const saveChannel = (c: ChannelInput) => call<Channel>('notify.save', c);
export const deleteChannel = (id: string) => call<unknown>('notify.delete', { id });
export const testChannel = (c: ChannelInput) => call<unknown>('notify.test', c);

/* Plugin job instances (Settings > Plugin jobs): daemon methods jobs.*, docs/api/jobs.md. */

export interface Schedule {
  every?: number;
  at?: string[];
  days?: number[];
}

export interface RunSummary {
  id: string;
  trigger: 'schedule' | 'manual' | 'webhook';
  status: 'queued' | 'running' | 'ok' | 'failed' | 'timeout' | 'cancelled';
  started: number;
  ended?: number;
  error?: string;
}

export interface JobInstance {
  id: string;
  plugin: string;
  job: string;
  name: string;
  params: Record<string, string>;
  schedule?: Schedule;
  owner: string;
  enabled: boolean;
  disabledReason?: string;
  needsAdmin: boolean;
  approval?: { by: string; at: number; valid: boolean };
  webhooks: { id: string; label?: string; created: number; lastUsed?: number }[];
  running: boolean;
  nextRun?: number;
  last?: RunSummary;
}

export interface StepLog {
  id: string;
  kind: string;
  status: 'ok' | 'failed' | 'skipped';
  durationMs?: number;
  exitCode?: number;
  httpStatus?: number;
  stdout?: string;
  stderr?: string;
  error?: string;
  truncated?: boolean;
  admin?: boolean;
  handled?: boolean;
}

export interface RunLog extends RunSummary {
  by?: string;
  steps: StepLog[];
}

export const listJobs = () => call<{ instances: JobInstance[] }>('jobs.list', {}, quiet);
export const setJobEnabled = (id: string, enabled: boolean) => call<JobInstance>('jobs.setEnabled', { id, enabled });
export const runJob = (id: string) => call<{ run: string }>('jobs.runNow', { id });
export const deleteJob = (id: string) => call<unknown>('jobs.delete', { id });
export const jobHistory = (id: string) => call<{ runs: RunLog[] }>('jobs.history', { id }, quiet);
export interface NewWebhook {
  id: string;
  label?: string;
  token: string;
  /** /hooks/<plugin>/<token>, to put after the console's address. */
  path: string;
}
export const createWebhook = (id: string, label: string) => call<NewWebhook>('jobs.webhooks.create', { id, label });
export const regenerateWebhook = (id: string, webhook: string) => call<NewWebhook>('jobs.webhooks.regenerate', { id, webhook });
export const revokeWebhook = (id: string, webhook: string) => call<unknown>('jobs.webhooks.revoke', { id, webhook });
