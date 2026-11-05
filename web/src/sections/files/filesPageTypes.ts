import type { FEntry } from './types';

export type Side = 'L' | 'R';
export const otherOf = (s: Side): Side => (s === 'L' ? 'R' : 'L');

export interface Clip {
  paths: string[];
  cut: boolean;
}

export type NameMode = 'folder' | 'file' | 'rename';
export type DialogState =
  | { kind: 'name'; mode: NameMode; entry?: FEntry; side: Side }
  | { kind: 'edit'; entry: FEntry; admin: boolean }
  | { kind: 'chown'; entry: FEntry }
  | { kind: 'image'; entry: FEntry; admin: boolean };
export interface DeleteRequest {
  entries: FEntry[];
  side: Side;
  trash: boolean;
}
export type BatchFailure = { path: string; message: string };
