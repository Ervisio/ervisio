/**
 * Recently used accounts shown as tiles on the sign-in page. They are kept in
 * this browser's localStorage only when the user opted in (Settings → This
 * browser), because on a shared browser they reveal who uses the server.
 */
export interface RecentUser {
  user: string;
  isAdmin?: boolean;
  /** How this account signed in last time (never the key itself). */
  method?: 'key';
}

const LIST = 'ervisio.recentUsers';
const OPT_IN = 'ervisio.rememberUsers';

export function recentUsersEnabled(): boolean {
  try {
    return localStorage.getItem(OPT_IN) === '1';
  } catch {
    return false;
  }
}

/** Turning it off also forgets the stored names. */
export function setRecentUsersEnabled(on: boolean): void {
  try {
    if (on) localStorage.setItem(OPT_IN, '1');
    else {
      localStorage.removeItem(OPT_IN);
      localStorage.removeItem(LIST);
    }
  } catch {
    /* storage unavailable */
  }
}

export function readRecentUsers(): RecentUser[] {
  try {
    if (!recentUsersEnabled()) {
      localStorage.removeItem(LIST); // names stored before this was opt-in
      return [];
    }
    const v = JSON.parse(localStorage.getItem(LIST) || '[]');
    return Array.isArray(v) ? v.filter((x) => x && typeof x.user === 'string').slice(0, 3) : [];
  } catch {
    return [];
  }
}

export function rememberRecentUser(r: RecentUser): void {
  if (!recentUsersEnabled()) return;
  try {
    localStorage.setItem(LIST, JSON.stringify([r, ...readRecentUsers().filter((x) => x.user !== r.user)].slice(0, 3)));
  } catch {
    /* ignore */
  }
}
