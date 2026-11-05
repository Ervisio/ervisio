import { API_BASE } from '../api/base';

const key = (user: string) => `ervisio.login-shell:${JSON.stringify([API_BASE, user])}`;

/** Only the interface name is cached before sign-in, never desktop or account contents. */
export function readLoginMode(user: string): 'classic' | 'ervisios' | undefined {
  if (!user) return undefined;
  try {
    const mode = localStorage.getItem(key(user));
    return mode === 'classic' || mode === 'ervisios' ? mode : undefined;
  } catch { return undefined; }
}

export function rememberLoginMode(user: string, mode: 'classic' | 'ervisios') {
  try { localStorage.setItem(key(user), mode); } catch { /* Server preferences remain authoritative. */ }
}
