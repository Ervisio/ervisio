export interface TaskProcess {
  pid: number;
  ppid: number;
  name: string;
  command: string;
  user: string;
  state: string;
  /** Percentage of one CPU core; divide by cpuCores for machine capacity. */
  cpu: number;
  memory: number;
  memPercent: number;
  threads: number;
  cpuAvailable: boolean;
  memoryAvailable: boolean;
}

export interface TaskSnapshot {
  source: 'host';
  platform: string;
  time: number;
  interval: number;
  cpuCores: number;
  scope: 'visible';
  privileged: boolean;
  processes: TaskProcess[];
  capabilities: {
    perProcessNetwork: { supported: boolean; reason?: string };
    perProcessGPU: { supported: boolean; reason?: string };
    gpu: { supported: boolean; reason?: string };
  };
  gpus: {
    id: string;
    name: string;
    utilization: number | null;
    memoryUsed: number | null;
    memoryTotal: number | null;
  }[];
}

export type ProcessSortKey = 'name' | 'pid' | 'user' | 'cpu' | 'memory';
export interface ProcessSort {
  key: ProcessSortKey;
  direction: 'asc' | 'desc';
}

/** A process may use several cores; the UI presents its fraction of the whole host. */
export function machineCpu(process: TaskProcess, cores: number): number | null {
  if (!process.cpuAvailable || !Number.isFinite(process.cpu)) return null;
  return Math.max(0, Math.min(100, process.cpu / Math.max(1, cores)));
}

export function selectProcesses(
  processes: TaskProcess[],
  query: string,
  sort: ProcessSort,
): TaskProcess[] {
  const q = query.trim().toLocaleLowerCase();
  const rows = q
    ? processes.filter((p) =>
        `${p.pid} ${p.name} ${p.user} ${p.command}`.toLocaleLowerCase().includes(q),
      )
    : [...processes];
  return rows.sort((a, b) => {
    // Unavailable samples always sort last, rather than appearing as genuine zero usage.
    if (sort.key === 'cpu' && a.cpuAvailable !== b.cpuAvailable) return a.cpuAvailable ? -1 : 1;
    if (sort.key === 'memory' && a.memoryAvailable !== b.memoryAvailable)
      return a.memoryAvailable ? -1 : 1;
    const av = a[sort.key],
      bv = b[sort.key];
    const result =
      typeof av === 'number' && typeof bv === 'number'
        ? av - bv
        : String(av).localeCompare(String(bv), undefined, { numeric: true, sensitivity: 'base' });
    return (sort.direction === 'asc' ? result : -result) || a.pid - b.pid;
  });
}

export const PROCESS_ROW_HEIGHT = 43;
/** Keep DOM work bounded even on a host with thousands of visible processes. */
export function processWindow(count: number, scrollTop: number, height: number) {
  const start = Math.max(0, Math.floor(scrollTop / PROCESS_ROW_HEIGHT) - 6);
  const end = Math.min(count, start + Math.ceil(height / PROCESS_ROW_HEIGHT) + 13);
  return { start: Math.min(start, count), end };
}
