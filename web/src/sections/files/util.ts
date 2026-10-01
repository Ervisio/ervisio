import type { FEntry, RawEntry, SortState } from './types';
import { isDirLike } from './types';

export const join = (dir: string, name: string) => (dir === '/' ? '/' + name : dir + '/' + name);
export const dirname = (p: string) => {
  const i = p.lastIndexOf('/');
  return i <= 0 ? '/' : p.slice(0, i);
};
export const basename = (p: string) => (p === '/' ? '/' : p.slice(p.lastIndexOf('/') + 1));
export const isVirtual = (loc: string) => !loc.startsWith('/');

export function crumbs(path: string): { name: string; path: string }[] {
  const out = [{ name: '/', path: '/' }];
  let acc = '';
  for (const part of path.split('/').filter(Boolean)) {
    acc += '/' + part;
    out.push({ name: part, path: acc });
  }
  return out;
}

export function withPath(dir: string, e: RawEntry): FEntry {
  return { ...e, path: e.path ?? join(dir, e.name) };
}

export function sortEntries(list: FEntry[], s: SortState): FEntry[] {
  const m = s.dir === 'asc' ? 1 : -1;
  const cmpName = (a: FEntry, b: FEntry) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });
  return [...list].sort((a, b) => {
    const da = isDirLike(a);
    const db = isDirLike(b);
    if (da !== db) return da ? -1 : 1;
    let c = 0;
    switch (s.key) {
      case 'size':
        c = da ? 0 : a.size - b.size;
        break;
      case 'mtime':
        c = a.mtime - b.mtime;
        break;
      case 'owner':
        c = a.owner.localeCompare(b.owner);
        break;
      case 'perm':
        c = a.perm.localeCompare(b.perm);
        break;
    }
    return c !== 0 ? c * m : cmpName(a, b) * m;
  });
}

export function formatWhen(ms: number, lang: string): string {
  if (!ms) return '';
  const d = new Date(ms);
  const now = new Date();
  const time = d.toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit' });
  if (d.toDateString() === now.toDateString()) return time;
  const y = new Date(now);
  y.setDate(now.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return time;
  return d.toLocaleDateString(lang, d.getFullYear() === now.getFullYear() ? { day: 'numeric', month: 'short' } : { day: 'numeric', month: 'short', year: 'numeric' });
}

/** Parses an octal mode string such as "0644" into 9 permission bits (owner, group, others x r/w/x). */
export function modeBits(mode: string): boolean[] {
  const v = parseInt(mode, 8) || 0;
  const bits: boolean[] = [];
  for (let i = 8; i >= 0; i--) bits.push(((v >> i) & 1) === 1);
  return bits;
}

/** Rebuilds the octal string keeping the special bits (setuid, setgid, sticky) of the original mode. */
export function bitsToMode(bits: boolean[], original: string): string {
  const old = parseInt(original, 8) || 0;
  let v = old & 0o7000;
  bits.forEach((b, i) => {
    if (b) v |= 1 << (8 - i);
  });
  return v.toString(8).padStart(4, '0');
}

export function nextId(): string {
  return Math.random().toString(36).slice(2, 10);
}

export function validFileName(name: string): boolean {
  return name.length > 0 && name !== '.' && name !== '..' && !name.includes('/') && !name.includes('\0');
}
