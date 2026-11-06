import { useNavigate } from 'react-router-dom';
import { useEffect } from 'react';
import { useT } from '../../i18n';
import type { RailBadge } from '../registry';
import { useBackgroundSummary, useSoftware } from './store';
import { attachIfBusy, registerRuntime } from './tx';
import { useAccountScope } from '../../lib/accountScope';

/** Rail badge: number of pending updates (a summary every 5 minutes; the server caches it for 10). */
export default function useBadge(): RailBadge | undefined {
  const t = useT('software');
  const nav = useNavigate();
  const scope = useAccountScope();
  useBackgroundSummary();
  // Toasts of a running transaction must be translated even when the page is closed.
  useEffect(() => {
    registerRuntime(t, nav, scope);
    if (scope) void attachIfBusy();
  }, [t, nav, scope]);
  const { summary } = useSoftware();
  return summary && summary.updates > 0 ? { count: summary.updates, tone: 'info' } : undefined;
}
