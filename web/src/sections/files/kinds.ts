import type { IconName } from '../../ui';
import type { RawEntry } from './types';
import { isDirLike } from './types';

export type Kind = 'dir' | 'img' | 'code' | 'arc' | 'doc' | 'txt' | 'pdf' | 'media' | 'other';

const ext = (n: string) => {
  const i = n.lastIndexOf('.');
  return i > 0 ? n.slice(i + 1).toLowerCase() : '';
};

const IMG = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'avif', 'bmp', 'ico', 'tif', 'tiff']);
const CODE = new Set(['sh', 'bash', 'zsh', 'py', 'js', 'ts', 'tsx', 'jsx', 'go', 'rs', 'c', 'h', 'cpp', 'hpp', 'java', 'php', 'rb', 'lua', 'css', 'html', 'xml', 'json', 'yaml', 'yml', 'toml', 'sql', 'service', 'timer', 'conf', 'ini', 'cfg', 'env', 'nix', 'mk', 'dockerfile']);
const ARC = new Set(['zip', 'tar', 'gz', 'tgz', 'bz2', 'xz', 'zst', '7z', 'rar', 'iso', 'deb', 'rpm', 'pkg', 'appimage']);
const DOC = new Set(['doc', 'docx', 'odt', 'xls', 'xlsx', 'ods', 'ppt', 'pptx', 'odp', 'csv', 'rtf', 'epub']);
const TXT = new Set(['txt', 'md', 'log', 'rst', 'readme', 'license', 'text']);
const MEDIA = new Set(['mp3', 'flac', 'ogg', 'opus', 'wav', 'm4a', 'mp4', 'mkv', 'webm', 'avi', 'mov']);
const TEXT_NAMES = new Set(['makefile', 'dockerfile', 'readme', 'license', 'authors', 'changelog', 'fstab', 'hosts', 'passwd', 'group', 'shadow', 'crontab', 'sudoers', 'profile', 'bashrc', 'zshrc', 'gitignore']);

export function kindOf(e: Pick<RawEntry, 'name' | 'type' | 'targetType' | 'mime'>): Kind {
  if (isDirLike(e)) return 'dir';
  const x = ext(e.name);
  if (IMG.has(x) || e.mime?.startsWith('image/')) return 'img';
  if (x === 'pdf') return 'pdf';
  if (CODE.has(x)) return 'code';
  if (ARC.has(x)) return 'arc';
  if (DOC.has(x)) return 'doc';
  if (TXT.has(x)) return 'txt';
  if (MEDIA.has(x) || e.mime?.startsWith('audio/') || e.mime?.startsWith('video/')) return 'media';
  if (e.mime?.startsWith('text/')) return 'txt';
  return 'other';
}

export const KIND_ICON: Record<Kind, IconName> = {
  dir: 'files',
  img: 'image',
  code: 'code',
  arc: 'archive',
  doc: 'file',
  txt: 'file',
  pdf: 'file',
  media: 'video',
  other: 'file',
};

/** Image types the daemon can decode for thumbnails. */
export const canThumb = (name: string) => ['png', 'jpg', 'jpeg', 'gif'].includes(ext(name));
/** Image types a browser shows directly. */
export const canInlineImage = (name: string) => ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'avif', 'bmp', 'ico'].includes(ext(name));

/** Might be editable text (the server still refuses binary content). */
export function looksTextual(e: Pick<RawEntry, 'name' | 'type' | 'targetType' | 'mime' | 'size'>): boolean {
  if (e.type === 'dir' || e.type === 'other') return false;
  if (e.size > 8 << 20) return false;
  const k = kindOf(e);
  if (k === 'code' || k === 'txt') return true;
  if (k !== 'other') return false;
  const x = ext(e.name);
  const base = e.name.replace(/^\./, '').toLowerCase();
  return !x || TEXT_NAMES.has(base) || e.mime === 'application/json' || e.mime === 'application/xml';
}
