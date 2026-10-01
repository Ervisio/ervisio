/**
 * The one place that knows where the daemon lives. Default: same origin as the page (production build served by
 * linuxadmind, or the Vite dev proxy). Set VITE_API_BASE=https://host:9090 for a wrapper that is not served by
 * the daemon (e.g. a desktop shell). Never build API URLs anywhere else.
 */
export const API_BASE: string = ((import.meta.env.VITE_API_BASE as string | undefined) ?? '').replace(/\/$/, '');

/** Absolute-or-relative URL for an HTTP path such as "/api/rpc" or "/plugins/docker/index.js". */
export const apiUrl = (path: string): string => API_BASE + path;

/** WebSocket URL for a path such as "/api/ws". */
export function wsUrl(path: string): string {
  const base = API_BASE || (typeof location !== 'undefined' ? location.origin : '');
  return base.replace(/^http/, 'ws') + path;
}

/** Cookies must travel when the API is on another origin. */
export const FETCH_CREDENTIALS: RequestCredentials = API_BASE ? 'include' : 'same-origin';
