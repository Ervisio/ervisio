import { ApiError, type CallOptions, type LoginResult, type PublicHost, type Session } from './types';
import { apiUrl } from './base';
import { http, MOCK } from './http';
import { requestUnlock } from './unlock';
import { mockAuth, mockCall } from './mock';

async function rpc<T>(method: string, params: unknown, opts: CallOptions): Promise<T> {
  if (MOCK) return (await mockCall(method, params ?? {}, opts.admin)) as T;
  return http<T>('/api/rpc', {
    body: { method, params: params ?? {}, ...(opts.admin ? { admin: true } : {}) },
    signal: opts.signal,
  });
}

/**
 * Call a bridge method. On `needs_admin` the global "Administrator rights needed" dialog opens;
 * after a successful unlock the same call is retried once.
 */
export async function call<T = unknown>(method: string, params?: unknown, opts: CallOptions = {}): Promise<T> {
  try {
    return await rpc<T>(method, params, opts);
  } catch (e) {
    if (e instanceof ApiError && e.code === 'needs_admin' && !opts.noUnlock) {
      await requestUnlock(method);
      return rpc<T>(method, params, opts);
    }
    throw e;
  }
}

const post = <T,>(path: string, body: unknown) => (MOCK ? (mockAuth(path, body) as Promise<T>) : http<T>(path, { method: 'POST', body }));

function normUntil(v: unknown): number | undefined {
  if (v == null || v === '') return undefined;
  if (typeof v === 'number') return v < 1e12 ? v * 1000 : v;
  const t = Date.parse(String(v));
  return Number.isNaN(t) ? undefined : t;
}

export async function session(): Promise<Session> {
  const s = await (MOCK ? mockAuth('/api/auth/session', {}) : http<any>('/api/auth/session'));
  return {
    user: s.user, name: s.name, uid: s.uid, home: s.home, groups: s.groups, isRoot: !!s.isRoot,
    isAdmin: !!s.isAdmin, canSudo: !!s.canSudo || !!s.isRoot, unlockedUntil: normUntil(s.unlockedUntil), unlockedForever: !!s.unlockedForever,
    authMethod: s.authMethod === 'ssh-key' ? 'ssh-key' : 'password', keyFingerprint: s.keyFingerprint || undefined,
  };
}

/** remember: cookie lives for session.timeout instead of until the browser closes. */
export const login = (user: string, password: string, remember = true) => post<LoginResult>('/api/auth/login', { user, password, remember });
/** SSH-key sign-in steps (see auth/sshkey for the whole flow). */
export const authChallenge = (user: string, host: string) =>
  post<{ nonce: string; challenge: string; host: string; expires: number }>('/api/auth/challenge', { user, host });
export const loginKey = (body: { user: string; publicKey: string; signature: string; nonce: string; remember: boolean }) =>
  post<LoginResult>('/api/auth/login-key', body);
export const logout = () => post<unknown>('/api/auth/logout', {});
/** password "" tries sudo without a password (NOPASSWD); it fails with code "invalid" and data.reason
 * "password_required" when sudo needs one (nothing is counted as a failed attempt). */
export async function unlock(password: string): Promise<{ until?: number; forever: boolean }> {
  const r = await post<{ unlockedUntil?: unknown; unlockedForever?: unknown }>('/api/auth/unlock', { password });
  return { until: normUntil(r?.unlockedUntil), forever: !!r?.unlockedForever };
}
export const lock = () => post<unknown>('/api/auth/lock', {});
export async function publicHost(): Promise<PublicHost> {
  const h = await (MOCK ? (mockAuth('/api/public/host', {}) as Promise<PublicHost>) : http<PublicHost>('/api/public/host'));
  return h.distro?.logoUrl ? { ...h, distro: { ...h.distro, logoUrl: apiUrl(h.distro.logoUrl) } } : h;
}

/** URL helpers for streamed downloads / uploads. */
export const downloadUrl = (path: string, admin = false, inline = false) =>
  apiUrl(`/api/files/download?path=${encodeURIComponent(path)}&admin=${admin ? 1 : 0}${inline ? '&inline=1' : ''}`);
/** POST the raw body here with header X-Requested-With: linuxadmin. */
export const uploadUrl = (path: string, admin = false, overwrite = false) =>
  apiUrl(`/api/files/upload?path=${encodeURIComponent(path)}&admin=${admin ? 1 : 0}&overwrite=${overwrite ? 1 : 0}`);
