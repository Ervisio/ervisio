import { apiUrl, FETCH_CREDENTIALS } from './base';
import { ApiError } from './types';

export const MOCK = import.meta.env.VITE_MOCK === '1';

/** Fired when any request answers 401 so the session context can send the user to /login. */
export const authEvents = new EventTarget();

export async function http<T>(
  path: string,
  init: { method?: 'GET' | 'POST'; body?: unknown; signal?: AbortSignal } = {},
): Promise<T> {
  let res: Response;
  try {
    res = await fetch(apiUrl(path), {
      method: init.method ?? (init.body !== undefined ? 'POST' : 'GET'),
      headers: {
        'X-Requested-With': 'ervisio',
        ...(init.body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      },
      body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
      credentials: FETCH_CREDENTIALS,
      signal: init.signal,
    });
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw new ApiError('cancelled', 'Request cancelled');
    throw new ApiError('network', 'Cannot reach the server.');
  }
  let json: any = null;
  const text = await res.text();
  if (text) {
    try {
      json = JSON.parse(text);
    } catch {
      json = null;
    }
  }
  if (!res.ok || (json && json.error)) {
    const e = json?.error;
    const code = e?.code ?? (res.status === 401 ? 'unauthenticated' : res.status === 403 ? 'forbidden' : 'internal');
    if (res.status === 401 && path !== '/api/auth/login' && path !== '/api/auth/login-key' && path !== '/api/auth/unlock') {
      authEvents.dispatchEvent(new Event('unauthenticated'));
    }
    throw new ApiError(code, e?.message ?? res.statusText ?? 'Request failed', e?.data, res.status);
  }
  if (json && typeof json === 'object' && 'result' in json && Object.keys(json).length === 1) return json.result as T;
  return json as T;
}
