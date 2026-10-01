import { useEffect, useMemo, useState } from 'react';
import { ApiError, call } from '../../api';
import { useI18n, useT } from '../../i18n';
import { Button, EmptyState, Icon, Skeleton, type IconName } from '../../ui';
import type { HistoryEntry } from './types';

interface Group {
  tx: number;
  time: number;
  entries: HistoryEntry[];
}

const ICON: Record<string, IconName> = { installed: 'plus', upgraded: 'download', removed: 'trash', downgraded: 'sort', reinstalled: 'refresh' };

export function HistoryTab({ reloadKey }: { reloadKey: number }) {
  const t = useT('software');
  const { lang } = useI18n();
  const [entries, setEntries] = useState<HistoryEntry[] | null>(null);
  const [error, setError] = useState('');
  const [open, setOpen] = useState<Set<number>>(new Set());
  const [limit, setLimit] = useState(40);

  useEffect(() => {
    setError('');
    call<HistoryEntry[]>('software.history', { limit: 1500 })
      .then(setEntries)
      .catch((e) => (setError(e instanceof ApiError ? e.message : String(e)), setEntries([])));
  }, [reloadKey]);

  const groups = useMemo(() => {
    const m = new Map<number, Group>();
    for (const e of entries ?? []) {
      const g = m.get(e.tx);
      if (g) {
        g.entries.push(e);
        g.time = Math.max(g.time, e.time);
      } else m.set(e.tx, { tx: e.tx, time: e.time, entries: [e] });
    }
    return [...m.values()].sort((a, b) => b.time - a.time);
  }, [entries]);

  const day = useMemo(() => new Intl.DateTimeFormat(lang, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' }), [lang]);
  const clock = useMemo(() => new Intl.DateTimeFormat(lang, { hour: '2-digit', minute: '2-digit' }), [lang]);

  if (!entries) return <div className="sw-body"><Skeleton lines={6} height={54} /></div>;
  if (groups.length === 0) {
    return <div className="sw-body"><EmptyState icon="clock" hue="sw" title={error ? t('error.load') : t('history.none')} text={error || t('history.noneText')} /></div>;
  }

  const summarize = (g: Group) => {
    const c: Record<string, number> = {};
    for (const e of g.entries) c[e.action] = (c[e.action] ?? 0) + 1;
    return Object.entries(c).map(([a, n]) => t(`history.${a}`, { count: n })).join(', ');
  };
  const main = (g: Group): string => {
    const c: Record<string, number> = {};
    for (const e of g.entries) c[e.action] = (c[e.action] ?? 0) + 1;
    return Object.entries(c).sort((a, b) => b[1] - a[1])[0][0];
  };

  let lastDay = '';
  return (
    <div className="sw-body">
      {groups.slice(0, limit).map((g) => {
        const d = day.format(new Date(g.time));
        const head = d !== lastDay ? <div key={`d${g.tx}`} className="sw-day">{d}</div> : null;
        lastDay = d;
        const isOpen = open.has(g.tx);
        const kind = main(g);
        return (
          <div key={g.tx}>
            {head}
            <div className={`sw-hx sw-hx--${kind}`}>
              <button
                type="button"
                className="sw-hx-hd"
                aria-expanded={isOpen}
                onClick={() => setOpen((s) => { const n = new Set(s); if (n.has(g.tx)) n.delete(g.tx); else n.add(g.tx); return n; })}
              >
                <span className="sw-hx-ic"><Icon name={ICON[kind]} /></span>
                <span className="sw-nm">
                  <b>{summarize(g)}</b>
                  <small className="sw-muted">{g.entries.slice(0, 4).map((e) => e.name).join(', ')}{g.entries.length > 4 ? ` +${g.entries.length - 4}` : ''}</small>
                </span>
                <span className="sw-muted">{clock.format(new Date(g.time))}</span>
                <Icon name={isOpen ? 'chevronup' : 'chevron'} />
              </button>
              {isOpen && (
                <div className="sw-hx-list">
                  {g.entries.map((e, i) => (
                    <div key={i} className="sw-hx-row">
                      <span className={`sw-act sw-act--${e.action}`}>{t(`history.verb.${e.action}`)}</span>
                      <b>{e.name}</b>
                      <span className="sw-ver">{e.from && e.to ? <>{e.from}<span className="sw-ar">→</span><b>{e.to}</b></> : e.to || e.from}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        );
      })}
      {groups.length > limit && <Button onClick={() => setLimit((l) => l + 40)}>{t('history.more')}</Button>}
    </div>
  );
}
