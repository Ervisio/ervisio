import { remoteTarget } from '../files/remoteLocation';

const images = new Set([
  'png',
  'jpg',
  'jpeg',
  'gif',
  'webp',
  'avif',
  'bmp',
  'svg',
  'ico',
  'tif',
  'tiff',
  'heic',
  'heif',
]);
const videos = new Set(['mp4', 'm4v', 'webm', 'ogv', 'mov', 'mkv', 'avi']);
const audio = new Set(['mp3', 'm4a', 'ogg', 'opus', 'wav', 'flac', 'aac']);

export function mediaKind(entry: {
  name: string;
  mime?: string;
}): 'image' | 'video' | 'audio' | null {
  const ext = entry.name.split('.').at(-1)?.toLowerCase() ?? '';
  if (images.has(ext) || entry.mime?.startsWith('image/')) return 'image';
  if (videos.has(ext) || entry.mime?.startsWith('video/')) return 'video';
  if (audio.has(ext) || entry.mime?.startsWith('audio/')) return 'audio';
  return null;
}
export const isMediaEntry = (entry: { name: string; mime?: string }) => mediaKind(entry) !== null;
export function validMediaPath(path: string) {
  return (
    path.length > 0 &&
    path.length <= 4096 &&
    !path.includes('\0') &&
    (!!remoteTarget(path) ||
      path.startsWith('/') ||
      /^[a-z]:[\\/]/i.test(path) ||
      path.startsWith('\\\\'))
  );
}
export function mediaLink(path: string, admin = false) {
  return `/media?path=${encodeURIComponent(path)}${admin && !remoteTarget(path) ? '&admin=1' : ''}`;
}
