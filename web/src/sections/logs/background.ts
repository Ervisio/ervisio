import { useEffect, useRef } from 'react';
import { stream, usePrefs, useSession } from '../../api';
import { useT } from '../../i18n';
import { notify } from '../../shell/notifications';
import { toast } from '../../ui';
import { normalizeWatchers } from './helpers';
import type { Entry } from './types';

/** A toast at most once a minute per watched file; the bell entry keeps counting meanwhile. */
const TOAST_EVERY = 60_000;

/**
 * "Notify on errors" for log watchers: while the app is open (any page), follow the watched files that have
 * notify on, error level only, and raise a toast plus a notification center entry. One stream for all of them,
 * as the user (a file that needs administrator rights is skipped quietly rather than asking for the password).
 */
export default function useLogsBackground() {
  const t = useT('logs');
  const tRef = useRef(t);
  tRef.current = t;
  const { prefs, ready } = usePrefs();
  const { status } = useSession();
  const watched = normalizeWatchers(prefs['logs.watchers']).filter((w) => w.notify);
  const key = JSON.stringify(watched.map((w) => [w.path, w.name, w.format]));

  useEffect(() => {
    if (status !== 'authed' || !ready || !watched.length) return;
    const names = new Map(watched.map((w) => [`file:${w.path}`, w]));
    const seen = new Map<string, { count: number; toastAt: number }>();
    let closed = false;
    const h = stream<Omit<Entry, 'key'>[]>(
      'logs.follow',
      { sources: [...names.keys()], levels: ['err'], watchers: watched },
      {
        reopen: true,
        onData: (batch) => {
          if (closed || !Array.isArray(batch)) return;
          for (const e of batch) {
            if (e.level !== 'err') continue;
            const w = names.get(e.srcId) ?? (e.file ? names.get(`file:${e.file}`) : undefined);
            if (!w) continue;
            const s = seen.get(w.path) ?? { count: 0, toastAt: 0 };
            s.count++;
            seen.set(w.path, s);
            const tr = tRef.current;
            const title = s.count === 1 ? tr('notice.error', { name: w.name }) : tr('notice.errors', { name: w.name, count: s.count });
            const msg = e.message.length > 300 ? `${e.message.slice(0, 300)}…` : e.message;
            notify({ title, detail: msg, tone: 'err', icon: 'logs', to: `/logs?file=${encodeURIComponent(w.path)}&level=err`, key: `logs:${w.path}` });
            const now = Date.now();
            if (now - s.toastAt > TOAST_EVERY) {
              s.toastAt = now;
              toast.err(tr('notice.error', { name: w.name }), msg);
            }
          }
        },
        // Unreadable file (needs_admin), missing file, daemon gone: stay quiet, the Logs page shows the problem.
        onError: () => undefined,
      },
    );
    return () => {
      closed = true;
      h.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, ready, key]);
}
