/** A saved connection id plus a POSIX path; credentials never belong in a location. */
export interface RemoteTarget {
  conn: string;
  path: string;
}

export function remoteTarget(value: string): RemoteTarget | null {
  const match = /^remote:([a-f0-9]{16}):(\/[^\0]*)$/i.exec(value);
  return match ? { conn: match[1], path: match[2] } : null;
}

export const isRemoteLocation = (value: string) => remoteTarget(value) !== null;

export function remoteLocation(conn: string, path = '/'): string {
  if (!/^[a-f0-9]{16}$/i.test(conn) || !path.startsWith('/') || path.includes('\0'))
    throw new Error('Invalid remote location');
  return `remote:${conn}:${path}`;
}

/** Explicit remote method allowlist: an unsupported operation can never fall through locally. */
export function remoteCallPlan(method: string, input: unknown) {
  const params =
    input && typeof input === 'object' ? ({ ...input } as Record<string, unknown>) : {};
  const values = [
    params.path,
    params.from,
    params.to,
    ...(Array.isArray(params.paths) ? params.paths : []),
  ];
  const targets = values
    .filter((v): v is string => typeof v === 'string')
    .map(remoteTarget)
    .filter((v): v is RemoteTarget => !!v);
  if (!targets.length) return null;
  const conn = targets[0].conn;
  if (targets.some((v) => v.conn !== conn))
    throw new Error('Use copy or move to transfer between connections.');
  const names: Record<string, string> = {
    'files.list': 'remote.ls',
    'files.stat': 'remote.stat',
    'files.mkdir': 'remote.mkdir',
    'files.create': 'remote.create',
    'files.rename': 'remote.rename',
    'files.move': 'remote.rename',
    'files.readText': 'remote.readText',
    'files.writeText': 'remote.writeText',
    'files.delete': 'remote.remove',
  };
  const mapped = names[method];
  if (!mapped) throw new Error('This operation is not supported on a remote connection.');
  const strip = (v: unknown) => {
    if (typeof v !== 'string') return v;
    const target = remoteTarget(v);
    if (!target || target.conn !== conn)
      throw new Error('Remote and local paths cannot be mixed in this operation.');
    return target.path;
  };
  for (const key of ['path', 'from', 'to']) if (key in params) params[key] = strip(params[key]);
  if (Array.isArray(params.paths)) params.paths = params.paths.map(strip);
  if (method === 'files.delete' && params.trash)
    throw new Error('Remote files cannot be moved to the local Trash.');
  delete params.trash;
  return { method: mapped, params: { ...params, conn }, conn };
}

export function remoteCopyPlan(from: string[], to: string) {
  const source = from.map((p) => remoteTarget(p) ?? { conn: '', path: p });
  const dest = remoteTarget(to) ?? { conn: '', path: to };
  if (!source.some((p) => p.conn) && !dest.conn) return null;
  if (source.some((p) => p.conn !== source[0].conn))
    throw new Error('Copy one connection at a time.');
  return { from: { conn: source[0].conn, paths: source.map((p) => p.path) }, to: dest };
}
