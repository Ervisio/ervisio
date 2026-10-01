export type Kind = 'repo' | 'aur' | 'flatpak';

export interface Summary {
  manager: string;
  sources: Kind[];
  aurHelper: string;
  aurSupported: boolean;
  updates: number;
  counts: Record<Kind, number>;
  downloadSize: number;
  rebootNeeded: boolean;
  rebootPending: boolean;
  security: number;
  /** unix ms, 0 = never */
  lastCheck: number;
  schedule: { at: string } | null;
  busy: boolean;
  warnings: string[];
}

export interface Update {
  name: string;
  title?: string;
  from: string;
  to: string;
  source: string;
  kind: Kind;
  size: number;
  /** reboot | security | restartService:<unit> */
  notes: string[];
  scope?: 'system' | 'user';
}

export interface Pkg {
  name: string;
  title?: string;
  version: string;
  source: string;
  kind: Kind;
  size: number;
  reason: 'explicit' | 'dependency';
  installDate: number;
  description: string;
  orphan: boolean;
  scope?: 'system' | 'user';
}

export interface SearchResult {
  name: string;
  title?: string;
  version: string;
  source: string;
  kind: Kind;
  description: string;
  installed: boolean;
  remote?: string;
}

export interface App {
  id: string;
  name: string;
  comment: string;
  icon: string;
  exec: string;
  categories: string[];
  package: string;
  source: string;
  kind: Kind;
  version: string;
  scope?: 'system' | 'user';
  explicit: boolean;
}

export interface Detail {
  name: string;
  source: string;
  kind: Kind;
  installed: boolean;
  version: string;
  description: string;
  fields: { key: string; value: string }[];
}

export interface HistoryEntry {
  time: number;
  action: 'installed' | 'upgraded' | 'removed' | 'downgraded' | 'reinstalled';
  name: string;
  from?: string;
  to?: string;
  tx: number;
}

export interface TxParams {
  op: 'upgrade' | 'install' | 'remove';
  packages: string[];
  source: string;
  scope?: 'system' | 'user';
}

export type TxEvent =
  | { type: 'start'; op: string; source: string; packages: string[]; steps: number }
  | { type: 'step'; index: number; title: string; command: string }
  | { type: 'log'; line: string }
  | { type: 'progress'; done: number; total: number; current: string }
  | { type: 'done'; ok: boolean; message: string; hint: string; rebootNeeded: boolean };

export interface ServerTx {
  running: boolean;
  op: string;
  source: string;
  packages: string[];
  startedAt: number;
  finishedAt?: number;
  done: number;
  total: number;
  current: string;
  step: number;
  steps: number;
  ok: boolean;
  message?: string;
  hint?: string;
  rebootNeeded: boolean;
  log: string[];
}
