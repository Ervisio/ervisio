import { API_BASE, useSession } from '../api';

/** Read caches are never shared across API servers or authenticated system accounts. */
export function useAccountScope(): string | null {
  const { session, status, host } = useSession();
  return status === 'authed' && session
    ? JSON.stringify([API_BASE || location.origin, host?.hostname ?? '', session.uid, session.user])
    : null;
}
