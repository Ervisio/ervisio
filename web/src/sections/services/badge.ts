import { useEffect, useRef } from 'react';
import { useT } from '../../i18n';
import { notify } from '../../shell/notifications';
import { toast } from '../../ui';
import { useBackgroundPoll, useShared } from './store';

/** Rail badge: number of failed units (one summary call per minute). Also tells the user when a unit fails
 * while the app is open: a toast and an entry in the notification center (not for units already failed at start). */
export default function useBadge(): number | undefined {
  useBackgroundPoll(false);
  const t = useT('services');
  const { failed, failedUnits } = useShared();
  const known = useRef<Set<string> | null>(null);
  useEffect(() => {
    if (!failedUnits) return;
    const now = new Set(failedUnits.map((u) => u.name));
    if (known.current) {
      for (const u of failedUnits) {
        if (known.current.has(u.name)) continue;
        const title = t('notice.failed', { name: u.name });
        notify({ title, detail: u.hint || u.description, tone: 'err', icon: 'services', to: `/services?unit=${encodeURIComponent(u.name)}`, key: `svc:${u.name}` });
        toast.err(title, u.hint || u.description);
      }
    }
    known.current = now;
  }, [failedUnits, t]);
  return failed || undefined;
}
