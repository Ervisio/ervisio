// Tiny in-browser fake daemon for `VITE_MOCK=1 npm run dev`.
import { ApiError, type PublicHost, type Session } from './types';

const LS = 'la.mock.';
const rd = <T,>(k: string, d: T): T => {
  try {
    const v = localStorage.getItem(LS + k);
    return v ? (JSON.parse(v) as T) : d;
  } catch {
    return d;
  }
};
const wr = (k: string, v: unknown) => {
  try {
    localStorage.setItem(LS + k, JSON.stringify(v));
  } catch {
    /* ignore */
  }
};

export const mockPublicHost: PublicHost = {
  hostname: 'arch',
  ip: '192.168.1.137',
  distro: { id: 'arch', name: 'Arch Linux', color: '#1793D1', logo: 'archlinux' },
};

let unlockedUntil = 0;
const delay = <T,>(v: T, ms = 120) => new Promise<T>((r) => setTimeout(() => r(v), ms));

export function mockSession(): Session {
  const user = rd<string | null>('user', null);
  if (!user) throw new ApiError('unauthenticated', 'Not signed in', undefined, 401);
  return { user, uid: 1000, isAdmin: unlockedUntil > Date.now(), canSudo: true, unlockedUntil: unlockedUntil > Date.now() ? unlockedUntil : undefined };
}

export async function mockAuth(path: string, body: any): Promise<any> {
  switch (path) {
    case '/api/public/host':
      return delay(mockPublicHost);
    case '/api/auth/session':
      return delay(mockSession());
    case '/api/auth/login': {
      if (body.user === 'root') throw new ApiError('forbidden', 'Root login is disabled on this server.', { reason: 'root_disabled' }, 403);
      if (!body.password || body.password === 'wrong') throw new ApiError('unauthenticated', 'Wrong username or password.', undefined, 401);
      if (body.password === 'slow') throw new ApiError('unavailable', 'Too many attempts.', { retryAfter: 900 }, 429);
      wr('user', body.user);
      return delay({ user: body.user, isAdmin: true, isRoot: false });
    }
    case '/api/auth/logout':
      wr('user', null);
      unlockedUntil = 0;
      return {};
    case '/api/auth/unlock':
      if (body.password === 'wrong') throw new ApiError('unauthenticated', 'Wrong password.', undefined, 401);
      unlockedUntil = Date.now() + 5 * 60_000;
      return delay({ unlockedUntil });
    case '/api/auth/lock':
      unlockedUntil = 0;
      return {};
  }
  throw new ApiError('not_found', path);
}

const cfg: Record<string, unknown> = rd('config', {
  allow_root: false,
  'login.show_ip': true,
  'login.max_failures': 5,
  'session.timeout': '12h',
  'session.admin_unlock': '5m',
  listen: '0.0.0.0:9090',
  'tls.mode': 'self-signed',
  'tls.redirect': true,
  'plugins.allow_unsigned': true,
  'plugins.dev': false,
  'tls.cert': '',
  'tls.key': '',
});

const MOCK_KEYS = [
  { key: 'listen', type: 'string', restart: true }, { key: 'allow_root', type: 'bool' }, { key: 'login.show_ip', type: 'bool' },
  { key: 'login.max_failures', type: 'int' }, { key: 'session.timeout', type: 'duration' }, { key: 'session.admin_unlock', type: 'duration' },
  { key: 'tls.mode', type: 'enum', values: ['self-signed', 'letsencrypt', 'custom'], restart: true }, { key: 'tls.redirect', type: 'bool', restart: true },
  { key: 'tls.cert', type: 'path', restart: true }, { key: 'tls.key', type: 'path', restart: true },
  { key: 'plugins.allow_unsigned', type: 'bool' }, { key: 'plugins.dev', type: 'bool' },
];
const mockState = () => ({ path: '/etc/linuxadmin/linuxadmin.conf', exists: true, values: { ...cfg }, defaults: {}, keys: MOCK_KEYS, warnings: [] });

export async function mockCall(method: string, params: any, _admin?: boolean): Promise<any> {
  switch (method) {
    case 'system.host':
      return delay({
        hostname: 'arch',
        kernel: '6.9.7-arch1-1',
        uptime: 86400 * 3 + 4000,
        os: { id: 'arch', name: 'Arch Linux', logo: 'archlinux', color: '#1793D1' },
        cpu: { model: 'Ryzen 7 5800X', cores: 16 },
        memTotal: 32 * 2 ** 30,
        ip: '192.168.1.137',
      });
    case 'system.metrics':
      return delay({
        cpu: 12 + Math.random() * 20,
        mem: { used: 9.2 * 2 ** 30, total: 32 * 2 ** 30 },
        load: [0.4, 0.5, 0.6],
        net: { rx: 120_000 * Math.random(), tx: 40_000 * Math.random() },
      });
    case 'prefs.get':
      return delay(rd('prefs', {}), 30);
    case 'prefs.set': {
      const p = rd<Record<string, unknown>>('prefs', {});
      if (params.value === null || params.value === undefined) delete p[params.key];
      else p[params.key] = params.value;
      wr('prefs', p);
      return {};
    }
    case 'config.get':
      return delay(mockState(), 60);
    case 'config.set':
      if (unlockedUntil < Date.now()) throw new ApiError('needs_admin', 'Administrator rights needed');
      if (!MOCK_KEYS.some((k) => k.key === params.key)) throw new ApiError('invalid', `unknown key "${params.key}"`);
      cfg[params.key] = params.value;
      wr('config', cfg);
      return mockState();
    case 'plugins.list':
      return delay([]);
  }
  throw new ApiError('not_found', `Mock has no method ${method}`);
}
