import type { ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { useSession } from '../api';

export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useSession();
  const loc = useLocation();
  if (status === 'loading') return <div style={{ minHeight: '100vh', background: 'var(--bg)' }} aria-busy="true" />;
  if (status === 'anon') return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search + loc.hash }} />;
  return <>{children}</>;
}
