import { useEffect, useState } from 'react';
import { call } from '../../api';
import { useSession } from '../../api';

/** Rail badge: how many installed plugins have an update (one list call every 5 minutes). */
export default function useBadge(): number | undefined {
  const { status } = useSession();
  const [n, setN] = useState(0);
  useEffect(() => {
    if (status !== 'authed') return;
    let live = true;
    const run = () =>
      call<{ updateAvailable?: unknown; enabled: boolean }[] | null>('plugins.list')
        .then((l) => live && setN((l ?? []).filter((p) => p.updateAvailable).length))
        .catch(() => undefined);
    void run();
    const id = window.setInterval(run, 300_000);
    return () => {
      live = false;
      window.clearInterval(id);
    };
  }, [status]);
  return n || undefined;
}
