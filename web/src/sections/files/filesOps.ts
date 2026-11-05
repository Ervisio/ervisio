import { ApiError, call, errorMessage } from '../../api';
import type { TFn } from '../../i18n';
import { copyToClipboard } from '../../lib/clipboard';
import { toast } from '../../ui';
import { emitChanged, fcall, fileDownloadUrl } from './fapi';
import type { BatchFailure, NameMode } from './filesPageTypes';
import { enqueueUpload, ensureDir } from './transfers';
import { isDirLike, type FEntry } from './types';
import { join } from './util';

export interface TrashReceipt {
  ids: string[];
  admin: boolean;
  toastId?: number;
}

const DOWNLOAD_STAGGER_MS = 350;

export function fail(title: string, x: unknown) {
  if (x instanceof ApiError && x.code === 'cancelled') return;
  toast.err(title, errorMessage(x));
}

export function toastFailed(failed: BatchFailure[] | undefined, title: (count: number) => string) {
  if (failed?.length) toast.err(title(failed.length), failed[0].message);
}

export const copyText = (txt: string, t: TFn) =>
  copyToClipboard(txt, t('pathCopied'), t('err.clipboard'));

function clickDownload(href: string, name: string) {
  const a = document.createElement('a');
  a.href = href;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
}

export function downloadEntries(es: FEntry[], admin: boolean, t: TFn) {
  const files = es.filter((e) => !isDirLike(e) && e.type !== 'other');
  if (files.length === 0) return toast.info(t('err.noFolderDownload'));
  files.forEach((e, i) =>
    window.setTimeout(
      () => clickDownload(fileDownloadUrl(e.path, admin), e.name),
      i * DOWNLOAD_STAGGER_MS,
    ),
  );
  if (es.length !== files.length) toast.info(t('err.foldersSkipped'));
}

export async function undoTrash(receipt: TrashReceipt, t: TFn): Promise<boolean> {
  try {
    const result = await call<{ restored: string[]; failed: BatchFailure[] }>(
      'files.restore',
      { ids: receipt.ids },
      { admin: receipt.admin },
    );
    toastFailed(result.failed, (count) => t('err.someNotRestored', { count }));
    if (result.failed?.length) receipt.ids = result.failed.map((failure) => failure.path);
    emitChanged();
    if (result.restored.length) toast.ok(t('restored', { count: result.restored.length }));
    if (receipt.toastId !== undefined) toast.dismiss(receipt.toastId);
    return !result.failed?.length;
  } catch (x) {
    fail(t('err.restore'), x);
    return false;
  }
}

interface NameSave {
  mode: NameMode;
  dir: string;
  entry: FEntry | undefined;
  name: string;
  admin: boolean;
}

export async function saveName({ mode, dir, entry, name, admin }: NameSave, t: TFn) {
  const target = join(dir, name);
  if (mode === 'folder') await fcall('files.mkdir', { path: target }, { admin });
  else if (mode === 'file') await fcall('files.create', { path: target }, { admin });
  else {
    if (target === entry!.path) return;
    await fcall('files.rename', { from: entry!.path, to: target }, { admin });
  }
  emitChanged(dir);
  if (mode !== 'rename')
    toast.ok(mode === 'folder' ? t('folderCreated', { name }) : t('fileCreated', { name }));
}

export async function uploadInto(
  items: { file: File; rel: string }[],
  dest: string,
  admin: boolean,
  onElevate: () => void,
  t: TFn,
) {
  const made = new Set<string>();
  try {
    for (const it of items) {
      const sub = it.rel.includes('/') ? it.rel.slice(0, it.rel.lastIndexOf('/')) : '';
      if (sub && !made.has(sub)) {
        made.add(sub);
        await ensureDir(join(dest, sub), admin, onElevate);
      }
      enqueueUpload({ file: it.file, dest, rel: it.rel, admin, onElevate });
    }
  } catch (x) {
    fail(t('err.upload'), x);
  }
}
