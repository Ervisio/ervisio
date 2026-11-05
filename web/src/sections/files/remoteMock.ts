// Development-only fixture, imported solely by VITE_MOCK's fake daemon.
import { ApiError } from '../../api/types';
import type { Remote, RawEntry } from './types';

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
const fixtureKey = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeFixtureOnly';

export function mockRemoteCall(method: string, params: Record<string, any>) {
  const connections = read<Remote[]>('remotes', []);
  const files = read<Record<string, RawEntry[]>>('remoteFiles', { '/srv': [], '/': [] });
  const content = read<Record<string, string>>('remoteText', {});
  const connection = connections.find((r) => r.id === (params.conn ?? params.id));
  switch (method) {
    case 'remote.list':
      return { connections };
    case 'remote.test':
      return params.kind === 'smb' || params.hostKey === fixtureKey
        ? { ok: true, path: params.path || '/srv' }
        : {
            ok: false,
            needsTrust: true,
            hostKey: fixtureKey,
            keyType: 'ssh-ed25519',
            fingerprint: 'SHA256:fixture-server-identity',
            changed: !!params.hostKey,
          };
    case 'remote.save': {
      if (params.kind !== 'smb' && params.hostKey !== fixtureKey)
        throw new ApiError('invalid', 'Test and trust the server key first.');
      const saved: Remote = {
        id: params.id || '0123456789abcdef',
        name: params.name || params.host,
        kind: params.kind,
        share: params.share,
        domain: params.domain,
        host: params.host,
        port: params.port,
        user: params.user,
        path: params.path || '/srv',
        auth: params.auth,
        hostKey: params.hostKey,
        fingerprint: 'SHA256:fixture-server-identity',
        hasPassword: !!params.password || !!connection?.hasPassword,
        hasKey: !!params.key || !!connection?.hasKey,
        hasPassphrase: !!params.passphrase || !!connection?.hasPassphrase,
      };
      write('remotes', [...connections.filter((r) => r.id !== saved.id), saved]);
      return saved;
    }
    case 'remote.forget':
      write(
        'remotes',
        connections.filter((r) => r.id !== params.id),
      );
      return { ok: true };
    case 'remote.start':
      if (!connection) throw new ApiError('not_found', 'Connection not found.');
      return { path: connection.path || '/srv' };
    case 'remote.ls':
      return { path: params.path, entries: files[params.path] ?? [], truncated: false };
    case 'remote.stat': {
      const path = params.path as string;
      const item = Object.values(files)
        .flat()
        .find((f) => f.path === path || path.endsWith('/' + f.name));
      if (item) return { ...item, path };
      if (files[path])
        return {
          name: path.split('/').at(-1),
          path,
          type: 'dir',
          size: 0,
          mtime: 1,
          mode: '0755',
          perm: 'drwxr-xr-x',
        };
      throw new ApiError('not_found', 'File not found.');
    }
    case 'remote.readText':
      return {
        path: params.path,
        content: content[params.path] ?? '',
        mtime: 1,
        encoding: 'utf-8',
        truncated: false,
      };
    case 'remote.writeText':
      content[params.path] = params.content;
      write('remoteText', content);
      return { path: params.path, mtime: 2 };
    case 'remote.mkdir':
    case 'remote.create': {
      const path = params.path as string;
      const index = path.lastIndexOf('/');
      const parent = path.slice(0, index) || '/';
      const entry: RawEntry = {
        name: path.slice(index + 1),
        path,
        type: method === 'remote.mkdir' ? 'dir' : 'file',
        size: 0,
        mode: '0644',
        perm: '-rw-r--r--',
        owner: 'deploy',
        group: 'deploy',
        uid: 1000,
        gid: 1000,
        mtime: 1,
      };
      files[parent] = [...(files[parent] ?? []), entry];
      if (entry.type === 'dir') files[path] = [];
      write('remoteFiles', files);
      return { path };
    }
    case 'remote.remove': {
      for (const dir of Object.keys(files))
        files[dir] = files[dir].filter((e) => !params.paths.includes(e.path || dir + '/' + e.name));
      write('remoteFiles', files);
      return { deleted: params.paths, failed: [] };
    }
    default:
      throw new ApiError('unavailable', 'Remote fixture operation is not available.');
  }
}
