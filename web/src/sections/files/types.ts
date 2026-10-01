export type EntryType = 'file' | 'dir' | 'symlink' | 'other';

/** One item as returned by files.list / files.stat / files.search. */
export interface RawEntry {
  name: string;
  path?: string;
  type: EntryType;
  size: number;
  mode: string;
  perm: string;
  owner: string;
  group: string;
  uid: number;
  gid: number;
  /** Unix milliseconds. */
  mtime: number;
  target?: string;
  targetType?: string;
  mime?: string;
}

/** An entry with its full path (and Trash bookkeeping when it comes from the Trash). */
export interface FEntry extends RawEntry {
  path: string;
  trashId?: string;
  origin?: string;
  deletedAt?: number;
}

export type View = 'grid' | 'list';
export type SortKey = 'name' | 'size' | 'owner' | 'perm' | 'mtime';
export interface SortState {
  key: SortKey;
  dir: 'asc' | 'desc';
}

export interface PaneState {
  /** An absolute path, or a virtual place: "trash:", "recent:", "starred:". */
  loc: string;
  view: View;
  /** Browsing through the root bridge (set after the user unlocked administrator rights). */
  admin: boolean;
}

export interface TabState {
  id: string;
  left: PaneState;
  right: PaneState;
  split: boolean;
}

export interface Disk {
  mount: string;
  device: string;
  fstype: string;
  total: number;
  used: number;
  avail: number;
}

export interface Places {
  home: string;
  recent: RawEntry[];
  disks: Disk[];
  trashCount: number;
}

export interface Remote {
  name: string;
  host?: string;
}

export const isDirLike = (e: Pick<RawEntry, 'type' | 'targetType'>) => e.type === 'dir' || (e.type === 'symlink' && e.targetType === 'dir');
