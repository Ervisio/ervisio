import { useNavigate } from 'react-router-dom';
import { useEffect } from 'react';
import { useT } from '../../i18n';
import type { RailBadge } from '../registry';
import { useBackgroundSummary, useSoftware } from './store';
import { registerRuntime } from './tx';

/** Rail badge: number of pending updates (a summary every 5 minutes; the server caches it for 10). */
export default function useBadge(): RailBadge | undefined {
  const t = useT('software');
  const nav = useNavigate();
  useBackgroundSummary();
  // Toasts of a running transaction must be translated even when the page is closed.
  useEffect(() => {
    registerRuntime(t, nav);
  }, [t, nav]);
  const { summary } = useSoftware();
  return summary && summary.updates > 0 ? { count: summary.updates, tone: 'info' } : undefined;
}
