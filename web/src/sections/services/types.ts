export type UnitKind = 'service' | 'timer' | 'socket';
export type UnitState = 'running' | 'failed' | 'stopped' | 'finished';

export interface Unit {
  name: string;
  description: string;
  load: string;
  active: string;
  sub: string;
  state: UnitState;
  enabled: string;
  memory: number | null;
  cpuNs: number | null;
  pid: number;
  since: number;
  purpose: 'web' | 'containers' | 'system';
  next?: number;
  last?: number;
  triggers?: string;
  listen?: string[];
}

export interface FailedUnit {
  name: string;
  description: string;
  since: number;
  result: string;
  exitCode: number;
  hint?: string;
  hintId?: string;
}

export interface Summary {
  total: number;
  running: number;
  stopped: number;
  timers: number;
  sockets: number;
  failed: FailedUnit[];
}

export interface Detail extends Unit {
  type: string;
  path: string;
  dropIns: string[];
  result: string;
  exitCode: number;
  canStart: boolean;
  canStop: boolean;
  canReload: boolean;
  dependencies: Record<string, string[]>;
}

export interface UnitFile {
  path: string;
  content: string;
  overrides: { path: string; content: string }[];
  overridePath: string;
  override: string;
}

export interface LogLine {
  time: number;
  priority: number;
  message: string;
}

export type Action = 'start' | 'stop' | 'restart' | 'reload' | 'enable' | 'disable' | 'mask' | 'unmask';
export type PanelTab = 'info' | 'logs' | 'unit' | 'deps';

export const PURPOSES = ['web', 'containers', 'system'] as const;
