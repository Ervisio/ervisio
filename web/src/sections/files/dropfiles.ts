export interface Dropped {
  file: File;
  /** Path inside the drop target, using "/" (folders are kept). */
  rel: string;
}

/** Reads the files (and, in browsers that support it, whole folders) of a drop event. */
export async function collectDropped(dt: DataTransfer): Promise<Dropped[]> {
  const out: Dropped[] = [];
  const items = Array.from(dt.items ?? []);
  const entries = items.map((i) => (i.kind === 'file' && typeof i.webkitGetAsEntry === 'function' ? i.webkitGetAsEntry() : null));
  if (entries.length && entries.every(Boolean)) {
    for (const e of entries) await walk(e as FileSystemEntry, '', out);
    return out;
  }
  for (const f of Array.from(dt.files)) out.push({ file: f, rel: f.name });
  return out;
}

async function walk(e: FileSystemEntry, prefix: string, out: Dropped[]): Promise<void> {
  if (e.isFile) {
    const file = await new Promise<File>((res, rej) => (e as FileSystemFileEntry).file(res, rej));
    out.push({ file, rel: prefix + e.name });
    return;
  }
  if (e.isDirectory) {
    const reader = (e as FileSystemDirectoryEntry).createReader();
    for (;;) {
      const batch = await new Promise<FileSystemEntry[]>((res, rej) => reader.readEntries(res, rej));
      if (!batch.length) break;
      for (const c of batch) await walk(c, prefix + e.name + '/', out);
    }
  }
}

export const DRAG_TYPE = 'application/x-linuxadmin-files';
