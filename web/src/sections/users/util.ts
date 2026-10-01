import type { UserInfo } from './types';

const HUES = ['ov', 'term', 'file', 'log', 'svc', 'sw', 'usr', 'plg'] as const;

/** Stable section-hue class for a name (hue-ov ... hue-plg). */
export function hueOf(name: string): string {
  let h = 5381;
  for (let i = 0; i < name.length; i++) h = ((h << 5) + h + name.charCodeAt(i)) | 0;
  return `hue-${HUES[Math.abs(h) % HUES.length]}`;
}

export const initial = (name: string) => (name.replace(/^[^a-zA-Z0-9]+/, '')[0] ?? '?').toUpperCase();

export const NAME_RE = /^[a-z_][a-z0-9_-]{0,31}$/;

/** 0 = empty, 1 = weak ... 4 = strong. */
export function strength(pw: string): number {
  if (!pw) return 0;
  let s = 0;
  if (pw.length >= 8) s++;
  if (pw.length >= 12) s++;
  if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) s++;
  if (/\d/.test(pw) && /[^A-Za-z0-9]/.test(pw)) s++;
  else if (/\d|[^A-Za-z0-9]/.test(pw) && pw.length >= 10) s++;
  return Math.max(1, Math.min(4, s));
}

export const loginDisabled = (u: UserInfo) => u.locked === true || u.passwordState === 'disabled' || u.noLoginShell;

export type Role = 'disabled' | 'admin' | 'new' | 'user';
export function roleOf(u: UserInfo): Role {
  if (loginDisabled(u)) return 'disabled';
  if (u.isAdmin) return 'admin';
  if (u.neverLoggedIn) return 'new';
  return 'user';
}

export const ADMIN_GROUPS = ['wheel', 'sudo', 'admin'];

/** Groups with an Italian description in the dictionary (users.json groupDesc). */
export const KNOWN_GROUPS = new Set([
  'wheel', 'sudo', 'admin', 'docker', 'podman', 'libvirt', 'kvm', 'audio', 'video', 'input', 'storage', 'network', 'networkmanager',
  'lp', 'scanner', 'render', 'optical', 'disk', 'adm', 'systemd-journal', 'uucp', 'dialout', 'tty', 'users', 'power', 'bluetooth',
  'plugdev', 'cdrom', 'wireshark', 'sambashare', 'vboxusers', 'root', 'nobody', 'nogroup', 'floppy', 'games', 'http', 'ftp',
]);

/** Groups worth showing in the per-user Groups tab without "Show all". */
export const COMMON_GROUPS = new Set([
  'docker', 'podman', 'libvirt', 'kvm', 'audio', 'video', 'input', 'storage', 'network', 'networkmanager', 'lp', 'scanner',
  'render', 'optical', 'uucp', 'dialout', 'plugdev', 'bluetooth', 'wireshark', 'systemd-journal', 'sambashare', 'vboxusers',
  'users', 'power', 'adm',
]);
