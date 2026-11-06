export interface Dropped {
  file: File;
  /** Path inside the drop target, using "/" (folders are kept). */
  rel: string;
}

/** Reads the files (and, in browsers that support it, whole folders) of a drop event. */
export async function collectDropped(dt: DataTransfer): Promise<Dropped[]> {
  const out: Dropped[] = [];
  const items = Array.from(dt.items ?? []);
  const entries = items.map((i) =>
    i.kind === 'file' && typeof i.webkitGetAsEntry === 'function' ? i.webkitGetAsEntry() : null,
  );
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
      const batch = await new Promise<FileSystemEntry[]>((res, rej) =>
        reader.readEntries(res, rej),
      );
      if (!batch.length) break;
      for (const c of batch) await walk(c, prefix + e.name + '/', out);
    }
  }
}

export const DRAG_TYPE = 'application/x-ervisio-files';

/** Browsers may attach Files to an internal drag too: our payload always takes precedence. */
export const hasExternalFiles = (dt: DataTransfer) =>
  Array.from(dt.types).includes('Files') &&
  !Array.from(dt.types).some((type) => type.startsWith('application/x-ervisio-'));

/** Dragging within the current parent is a no-op, including Ctrl-drag. Paste can still duplicate. */
export function dropSources(paths: unknown, destination: string): string[] {
  if (!Array.isArray(paths)) return [];
  const key = (path: string) => {
    const windows = /^[a-z]:[\\/]/i.test(path) || path.startsWith('\\\\');
    const normalized = windows ? path.replace(/\\/g, '/').toLowerCase() : path;
    return normalized.replace(/\/+$/, '') || '/';
  };
  const dest = key(destination);
  return [
    ...new Set(
      paths.filter(
        (p): p is string =>
          typeof p === 'string' && p.length > 0 && p.length <= 4096 && !p.includes('\0'),
      ),
    ),
  ]
    .slice(0, 10000)
    .filter((path) => {
      const source = key(path);
      const index = source.lastIndexOf('/');
      return source !== dest && (index <= 0 ? '/' : source.slice(0, index)) !== dest;
    });
}
