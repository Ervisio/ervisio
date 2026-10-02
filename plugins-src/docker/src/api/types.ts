/** Docker Engine API shapes, only the fields the UI reads. Extend as views need more. */

export interface PortBinding {
  IP?: string;
  PrivatePort: number;
  PublicPort?: number;
  Type: 'tcp' | 'udp' | 'sctp' | string;
}

export interface Container {
  Id: string;
  Names: string[];
  Image: string;
  ImageID: string;
  Command: string;
  Created: number;
  Ports: PortBinding[];
  Labels: Record<string, string>;
  /** created, running, paused, restarting, removing, exited, dead */
  State: string;
  /** Human text, for example "Up 3 days (healthy)" or "Exited (0) 2 hours ago". */
  Status: string;
  HostConfig?: { NetworkMode?: string };
  NetworkSettings?: { Networks?: Record<string, { IPAddress?: string; NetworkID?: string }> };
  Mounts?: { Type: string; Name?: string; Source: string; Destination: string; RW: boolean }[];
}

export interface ContainerInspect {
  Id: string;
  Name: string;
  Created: string;
  Path: string;
  Args: string[];
  State: {
    Status: string;
    Running: boolean;
    Paused: boolean;
    Restarting: boolean;
    OOMKilled: boolean;
    Dead: boolean;
    Pid: number;
    ExitCode: number;
    Error: string;
    StartedAt: string;
    FinishedAt: string;
    Health?: { Status: string; FailingStreak: number; Log?: { Start: string; End: string; ExitCode: number; Output: string }[] };
  };
  Image: string;
  RestartCount: number;
  HostConfig: Record<string, any> & {
    RestartPolicy?: { Name: string; MaximumRetryCount: number };
    Memory?: number;
    NanoCpus?: number;
    CpuShares?: number;
    NetworkMode?: string;
  };
  Config: {
    Hostname?: string;
    User?: string;
    Tty: boolean;
    OpenStdin?: boolean;
    Env?: string[];
    Cmd?: string[] | null;
    Entrypoint?: string[] | null;
    Image: string;
    Labels?: Record<string, string>;
    WorkingDir?: string;
    ExposedPorts?: Record<string, unknown>;
  };
  Mounts: { Type: string; Name?: string; Source: string; Destination: string; Mode?: string; RW: boolean }[];
  NetworkSettings: {
    Ports?: Record<string, { HostIp: string; HostPort: string }[] | null>;
    Networks?: Record<string, { IPAddress: string; Gateway: string; MacAddress: string; Aliases?: string[] | null }>;
  };
}

export interface ImageSummary {
  Id: string;
  ParentId: string;
  RepoTags: string[] | null;
  RepoDigests: string[] | null;
  Created: number;
  Size: number;
  SharedSize: number;
  Labels: Record<string, string> | null;
  /** Number of containers using the image, or -1 when not computed. */
  Containers: number;
}

export interface VolumeInfo {
  Name: string;
  Driver: string;
  Mountpoint: string;
  CreatedAt?: string;
  Labels: Record<string, string> | null;
  Scope: string;
  Options: Record<string, string> | null;
  UsageData?: { Size: number; RefCount: number } | null;
}

export interface NetworkInfo {
  Id: string;
  Name: string;
  Driver: string;
  Scope: string;
  Internal: boolean;
  EnableIPv6?: boolean;
  Created?: string;
  Labels?: Record<string, string> | null;
  IPAM?: { Driver: string; Config?: { Subnet?: string; Gateway?: string }[] | null };
  Containers?: Record<string, { Name: string; IPv4Address: string; IPv6Address: string; MacAddress: string }>;
}

export interface DiskUsage {
  LayersSize: number;
  Images: { Id: string; Size: number; SharedSize: number; Containers: number }[] | null;
  Containers: { Id: string; State: string; SizeRw?: number; SizeRootFs?: number }[] | null;
  Volumes: { Name: string; UsageData?: { Size: number; RefCount: number } | null }[] | null;
  BuildCache: { ID: string; Size: number; InUse: boolean; Shared?: boolean }[] | null;
}

export interface SystemInfo {
  ID: string;
  Containers: number;
  ContainersRunning: number;
  ContainersPaused: number;
  ContainersStopped: number;
  Images: number;
  Driver: string;
  OperatingSystem: string;
  OSType: string;
  Architecture: string;
  KernelVersion: string;
  NCPU: number;
  MemTotal: number;
  Name: string;
  ServerVersion: string;
  DockerRootDir: string;
  CgroupVersion?: string;
}

export interface VersionInfo {
  Version: string;
  ApiVersion: string;
  MinAPIVersion?: string;
  Os: string;
  Arch: string;
  KernelVersion?: string;
}

export interface DockerEvent {
  Type: string;
  Action: string;
  /** Container id or image name, depending on Type. */
  Actor?: { ID: string; Attributes?: Record<string, string> };
  scope?: string;
  time: number;
  timeNano?: number;
}

export interface StatsRaw {
  read: string;
  preread?: string;
  cpu_stats: {
    cpu_usage: { total_usage: number; percpu_usage?: number[] };
    system_cpu_usage?: number;
    online_cpus?: number;
  };
  precpu_stats?: StatsRaw['cpu_stats'];
  memory_stats: { usage?: number; limit?: number; stats?: Record<string, number> };
  networks?: Record<string, { rx_bytes: number; tx_bytes: number }>;
  blkio_stats?: { io_service_bytes_recursive?: { op: string; value: number }[] | null };
  pids_stats?: { current?: number };
}

/** One computed stats point. */
export interface StatPoint {
  t: number;
  /** Percent of one core (can pass 100 on multi-core use). */
  cpu: number;
  memUsed: number;
  memLimit: number;
  /** memUsed / memLimit * 100 */
  memPct: number;
  netRx: number;
  netTx: number;
  /** Network bytes per second since the previous point (0 for the first). */
  netRxRate: number;
  netTxRate: number;
  blkRead: number;
  blkWrite: number;
  pids: number;
}

export const COMPOSE_PROJECT = 'com.docker.compose.project';
export const COMPOSE_SERVICE = 'com.docker.compose.service';
export const COMPOSE_WORKDIR = 'com.docker.compose.project.working_dir';
export const COMPOSE_FILES = 'com.docker.compose.project.config_files';
