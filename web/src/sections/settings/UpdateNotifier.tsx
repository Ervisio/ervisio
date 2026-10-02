import { useEffect } from 'react';
import { useSession } from '../../api';
import { Name } from '../../brand';
import { useT } from '../../i18n';
import { notify } from '../../shell/notifications';
import { checkUpdates, updateStatus } from './updates';

const EVERY = 6 * 60 * 60_000;

/**
 * Mounted once by the app shell: for administrators, when updates.auto_check is on, asks updates.check (cached
 * an hour on the server) at sign-in and every 6 hours, and puts "Ervisio X is available" in the bell menu,
 * linking to Settings › About. Renders nothing.
 */
export function UpdateNotifier() {
  const t = useT('settings');
  const { session } = useSession();
  const isAdmin = !!session && (session.isAdmin || session.canSudo || !!session.isRoot);

  useEffect(() => {
    if (!isAdmin) return;
    let stop = false;
    const run = async () => {
      try {
        const st = await updateStatus();
        if (stop || st.settings?.autoCheck === false) return;
        const r = await checkUpdates(false);
        if (stop || !r.newer || !r.latest) return;
        notify({
          key: 'ervisio-update',
          title: t('updates.notice.title', { name: Name, version: r.latest.version }),
          detail: t('updates.notice.detail', { current: r.current }),
          tone: 'info',
          icon: 'download',
          to: '/settings#about',
        });
      } catch {
        /* offline or GitHub unreachable: try again later */
      }
    };
    const first = window.setTimeout(() => void run(), 4000);
    const timer = window.setInterval(() => void run(), EVERY);
    return () => {
      stop = true;
      window.clearTimeout(first);
      window.clearInterval(timer);
    };
  }, [isAdmin, t]);

  return null;
}
