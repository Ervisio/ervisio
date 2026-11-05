// In-browser mutation fixtures. These do not participate in production RPC calls.
import { ApiError } from '../../api/types';
const prefix = 'ervisio.mock.';
const read = <T>(key: string, fallback: T): T => {
  try {
    return JSON.parse(localStorage.getItem(prefix + key) ?? 'null') ?? fallback;
  } catch {
    return fallback;
  }
};
const write = (key: string, value: unknown) =>
  localStorage.setItem(prefix + key, JSON.stringify(value));
const parent = (path: string) => path.slice(0, path.lastIndexOf('/')) || '/';
const name = (path: string) => path.slice(path.lastIndexOf('/') + 1);

export function mockFilesOp(method: string, params: any, admin: boolean): unknown {
  if (
    ![
      'files.delete',
      'files.trashList',
      'files.restore',
      'files.trashDelete',
      'files.trashEmpty',
      'files.copy',
      'files.mkdir',
      'files.rename',
    ].includes(method)
  )
    return undefined;
  const files = read<Record<string, any[]>>('files', {});
  const scope = admin ? 'trash.admin' : 'trash.user';
  const trash = read<any[]>(scope, []);
  const calls = read<unknown[]>('fileMutations', []);
  write('fileMutations', [...calls.slice(-99), { method, params, admin }]);
  if (method === 'files.trashList') return { items: trash };
  if (method === 'files.delete') {
    if (
      !admin &&
      (params.paths as string[]).some((p) =>
        read<string[]>('files.requireAdmin', []).some((root) => p.startsWith(root)),
      )
    )
      throw new ApiError('needs_admin', 'Administrator rights are required.');
    const deleted: string[] = [];
    const trashed: { id: string; path: string }[] = [];
    for (const path of params.paths as string[]) {
      const directory = parent(path);
      const entry = files[directory]?.find((e) => e.name === name(path));
      if (!entry) continue;
      files[directory] = files[directory].filter((e) => e !== entry);
      deleted.push(path);
      if (params.trash) {
        const id = crypto.randomUUID();
        trashed.push({ id, path });
        trash.push({
          id,
          originalPath: path,
          deletedAt: Date.now(),
          entry: { ...entry, path: `/fixture-trash/${id}` },
        });
      }
    }
    write('files', files);
    write(scope, trash);
    return { deleted, trashed, failed: [] };
  }
  if (method === 'files.restore') {
    const restored: string[] = [];
    const failed: { path: string; message: string }[] = [];
    for (const id of params.ids as string[]) {
      const item = trash.find((entry) => entry.id === id);
      if (!item) {
        failed.push({ path: id, message: 'Trash item not found.' });
        continue;
      }
      const directory = parent(item.originalPath);
      if ((files[directory] ?? []).some((entry) => entry.name === item.entry.name)) {
        failed.push({ path: id, message: 'The original location already exists.' });
        continue;
      }
      files[directory] = [...(files[directory] ?? []), { ...item.entry, path: item.originalPath }];
      restored.push(item.originalPath);
      trash.splice(trash.indexOf(item), 1);
    }
    write('files', files);
    write(scope, trash);
    return { restored, failed };
  }
  if (method === 'files.trashDelete' || method === 'files.trashEmpty') {
    write(
      scope,
      method === 'files.trashEmpty' ? [] : trash.filter((item) => !params.ids.includes(item.id)),
    );
    return {};
  }
  if (method === 'files.copy') {
    const texts = read<Record<string, unknown>>('texts', {});
    for (const path of params.from as string[]) {
      const entry = files[parent(path)]?.find((item) => item.name === name(path));
      if (!entry) throw new ApiError('not_found', 'The source does not exist.');
      const target = params.to.replace(/\/$/, '') + '/' + entry.name;
      if ((files[params.to] ?? []).some((item) => item.name === entry.name))
        throw new ApiError('conflict', 'The destination already exists.');
      files[params.to] = [...(files[params.to] ?? []), { ...entry, path: target }];
      if (texts[path]) texts[target] = texts[path];
      if (entry.type === 'dir') files[target] = files[path] ?? [];
      if (params.move) {
        files[parent(path)] = files[parent(path)].filter((item) => item !== entry);
        delete texts[path];
      }
    }
    write('files', files);
    write('texts', texts);
    return { done: true };
  }
  if (method === 'files.mkdir') {
    files[parent(params.path)] = [
      ...(files[parent(params.path)] ?? []),
      {
        name: name(params.path),
        type: 'dir',
        size: 0,
        mtime: Date.now(),
        mode: '0755',
        perm: 'drwxr-xr-x',
        owner: 'alice',
        group: 'alice',
        uid: 1000,
        gid: 1000,
      },
    ];
    files[params.path] = [];
    write('files', files);
    return { path: params.path };
  }
  if (method === 'files.rename') {
    const source = files[parent(params.from)]?.find((item) => item.name === name(params.from));
    if (!source) throw new ApiError('not_found', 'The source does not exist.');
    files[parent(params.from)] = files[parent(params.from)].filter((item) => item !== source);
    files[parent(params.to)] = [
      ...(files[parent(params.to)] ?? []),
      { ...source, name: name(params.to), path: params.to },
    ];
    write('files', files);
    return { path: params.to };
  }
}
