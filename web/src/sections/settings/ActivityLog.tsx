import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, apiUrl, call } from '../../api';
import { useT } from '../../i18n';
import { formatBytes, formatDateTime } from '../../lib/format';
import { Badge, Button, EmptyState, Input, Select, Table, useIsMobile, type Column, type Tone } from '../../ui';

/** One line of the activity log (server/internal/audit). */
export interface AuditEntry {
  time: string;
  user: string;
  ip?: string;
  source: 'plugin' | 'core';
  plugin?: string;
  action: string;
  via?: string;
  target?: string;
  result: 'ok' | 'failed' | 'denied' | 'error';
  code?: number;
  bytes?: number;
  admin?: boolean;
  detail?: string;
  env?: string;
  origin?: string;
}

interface Filters {
  plugin: string;
  user: string;
  source: string;
  result: string;
  text: string;
  since: string;
  until: string;
}
const EMPTY: Filters = { plugin: '', user: '', source: '', result: '', text: '', since: '', until: '' };
const PAGE = 100;

const dayStart = (d: string) => (d ? new Date(`${d}T00:00:00`).toISOString() : '');
const dayEnd = (d: string) => (d ? new Date(`${d}T23:59:59.999`).toISOString() : '');

/** Query of audit.list and /api/audit/export. `result` has no server filter: it is applied to the loaded rows. */
function params(f: Filters): Record<string, string> {
  const p: Record<string, string> = {};
  if (f.plugin.trim()) p.plugin = f.plugin.trim();
  if (f.user.trim()) p.user = f.user.trim();
  if (f.source) p.source = f.source;
  if (f.text.trim()) p.text = f.text.trim();
  if (f.since) p.since = dayStart(f.since);
  if (f.until) p.until = dayEnd(f.until);
  return p;
}

const TONE: Record<AuditEntry['result'], Tone> = { ok: 'ok', failed: 'warn', denied: 'err', error: 'err' };

/**
 * Settings › Activity log: who did what through the console and its plugins. Administrators (root, or with
 * administrator rights unlocked) see every user, everyone else only their own entries; the daemon decides.
 */
export function ActivityLog({ canSeeAll, canUnlock }: { canSeeAll: boolean; canUnlock: boolean }) {
  const t = useT('settings');
  const mobile = useIsMobile();
  const [f, setF] = useState<Filters>(EMPTY);
  const [applied, setApplied] = useState<Filters>(EMPTY);
  const [rows, setRows] = useState<AuditEntry[]>([]);
  const [next, setNext] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const seq = useRef(0);

  const load = useCallback(async (flt: Filters, cursor: string) => {
    const my = ++seq.current;
    setLoading(true);
    setError('');
    try {
      const r = await call<{ entries: AuditEntry[]; next: string; enabled: boolean }>('audit.list', { ...params(flt), limit: PAGE, ...(cursor ? { cursor } : {}) }, { noUnlock: true });
      if (my !== seq.current) return;
      setRows((old) => (cursor ? [...old, ...r.entries] : r.entries));
      setNext(r.next ?? '');
      setEnabled(r.enabled !== false);
    } catch (e) {
      if (my === seq.current) setError(e instanceof ApiError || e instanceof Error ? e.message : String(e));
    } finally {
      if (my === seq.current) setLoading(false);
    }
  }, []);

  // Reloads when the filters change, and when administrator rights are unlocked or lock again (more or fewer users' entries).
  useEffect(() => {
    void load(applied, '');
  }, [applied, load, canSeeAll]);

  // Filters apply a moment after typing stops.
  useEffect(() => {
    if (JSON.stringify(f) === JSON.stringify(applied)) return;
    const id = window.setTimeout(() => setApplied(f), 350);
    return () => window.clearTimeout(id);
  }, [f, applied]);

  const set = (patch: Partial<Filters>) => setF((x) => ({ ...x, ...patch }));
  const shown = f.result ? rows.filter((r) => r.result === f.result) : rows;
  const dirty = JSON.stringify(f) !== JSON.stringify(EMPTY);

  const exportUrl = (format: 'csv' | 'json') => apiUrl(`/api/audit/export?${new URLSearchParams({ ...params(applied), format }).toString()}`);

  const actionLabel = (e: AuditEntry) => {
    const key = `activity.actions.${e.action.replace(/\./g, '_')}`;
    const label = t(key);
    return label === key ? e.action : label;
  };

  // A narrow screen gets one column: what happened, to what, the result, and who/when underneath.
  const cardCols: Column<AuditEntry>[] = [
    {
      key: 'entry',
      header: t('activity.col.what'),
      render: (e) => (
        <span className="st-act-card">
          <span className="top">
            <b>{actionLabel(e)}</b>
            <Badge tone={TONE[e.result] ?? 'neutral'}>{t(`activity.result.${e.result}`)}</Badge>
          </span>
          <span className="st-act-target" title={[e.target, e.detail].filter(Boolean).join('\n')}>
            {e.target || '-'}
            {e.detail && <small>{e.detail}</small>}
          </span>
          <span className="meta">
            {[formatDateTime(e.time), e.user, e.ip, e.source === 'plugin' ? e.plugin : t('activity.core'), e.env, e.origin, e.code !== undefined ? String(e.code) : '', e.bytes !== undefined ? formatBytes(e.bytes) : ''].filter(Boolean).join(' · ')}
          </span>
        </span>
      ),
    },
  ];

  const cols: Column<AuditEntry>[] = [
    { key: 'time', header: t('activity.col.time'), render: (e) => <span title={e.time}>{formatDateTime(e.time)}</span>, width: 128 },
    {
      key: 'who',
      header: t('activity.col.who'),
      render: (e) => (
        <span className="st-act-who">
          <b>{e.user || '-'}</b>
          {e.ip && <small>{e.ip}</small>}
        </span>
      ),
      width: 120,
    },
    {
      key: 'what',
      header: t('activity.col.what'),
      render: (e) => (
        <span className="st-act-what">
          <b>{actionLabel(e)}</b>
          <small>{[e.source === 'plugin' ? e.plugin : t('activity.core'), e.env, e.admin ? t('activity.admin') : '', e.origin].filter(Boolean).join(' · ')}</small>
        </span>
      ),
      width: 150,
    },
    {
      key: 'target',
      header: t('activity.col.target'),
      render: (e) => (
        <span className="st-act-target" title={[e.target, e.detail].filter(Boolean).join('\n')}>
          {e.target || '-'}
          {e.detail && <small>{e.detail}</small>}
        </span>
      ),
    },
    {
      key: 'result',
      header: t('activity.col.result'),
      render: (e) => (
        <span className="st-act-result">
          <Badge tone={TONE[e.result] ?? 'neutral'}>{t(`activity.result.${e.result}`)}</Badge>
          {(e.code !== undefined || e.bytes !== undefined) && (
            <small>{[e.code !== undefined ? String(e.code) : '', e.bytes !== undefined ? formatBytes(e.bytes) : ''].filter(Boolean).join(' · ')}</small>
          )}
        </span>
      ),
      width: 110,
    },
  ];

  return (
    <div className="st-act">
      {!enabled && <div className="st-warn"><span>{t('activity.off')}</span></div>}
      {!canSeeAll && <p className="st-act-note">{canUnlock ? t('activity.ownOnlyUnlock') : t('activity.ownOnly')}</p>}
      <div className="st-act-filters">
        <Input compact aria-label={t('activity.f.text')} icon="search" placeholder={t('activity.f.text')} value={f.text} onChange={(e) => set({ text: e.target.value })} />
        <Input compact mono aria-label={t('activity.f.plugin')} placeholder={t('activity.f.plugin')} value={f.plugin} onChange={(e) => set({ plugin: e.target.value })} />
        {canSeeAll && <Input compact aria-label={t('activity.f.user')} placeholder={t('activity.f.user')} value={f.user} onChange={(e) => set({ user: e.target.value })} />}
        <Select
          compact
          aria-label={t('activity.f.source')}
          value={f.source}
          onChange={(v) => set({ source: v })}
          options={[{ value: '', label: t('activity.f.anySource') }, { value: 'plugin', label: t('activity.f.plugins') }, { value: 'core', label: t('activity.f.console') }]}
        />
        <Select
          compact
          aria-label={t('activity.f.result')}
          value={f.result}
          onChange={(v) => set({ result: v })}
          options={[{ value: '', label: t('activity.f.anyResult') }, ...(['ok', 'failed', 'denied', 'error'] as const).map((r) => ({ value: r, label: t(`activity.result.${r}`) }))]}
        />
        <label className="st-act-date">
          <span>{t('activity.f.since')}</span>
          <Input compact type="date" aria-label={t('activity.f.since')} value={f.since} max={f.until || undefined} onChange={(e) => set({ since: e.target.value })} />
        </label>
        <label className="st-act-date">
          <span>{t('activity.f.until')}</span>
          <Input compact type="date" aria-label={t('activity.f.until')} value={f.until} min={f.since || undefined} onChange={(e) => set({ until: e.target.value })} />
        </label>
        {dirty && <Button variant="ghost" icon="x" onClick={() => setF(EMPTY)}>{t('activity.f.clear')}</Button>}
      </div>
      <div className="st-act-bar">
        <span className="st-act-count" aria-live="polite">{loading && rows.length === 0 ? t('activity.loading') : t('activity.count', { count: shown.length })}{next ? '+' : ''}</span>
        <span className="st-act-export">
          <Button icon="download" onClick={() => window.location.assign(exportUrl('csv'))}>{t('activity.exportCsv')}</Button>
          <Button icon="download" onClick={() => window.location.assign(exportUrl('json'))}>{t('activity.exportJson')}</Button>
        </span>
      </div>
      {error ? (
        <div className="st-warn" role="alert"><span>{t('activity.failed', { error })}</span><Button icon="refresh" onClick={() => void load(applied, '')}>{t('server.failed.action')}</Button></div>
      ) : (
        <Table<AuditEntry>
          columns={mobile ? cardCols : cols}
          rows={shown}
          rowKey={(e) => `${e.time}|${e.user}|${e.action}|${e.target ?? ''}|${e.result}|${e.plugin ?? ''}`}
          caption={t('activity.title')}
          empty={<EmptyState icon={dirty ? 'search' : 'clock'} hue="log" title={dirty ? t('activity.noMatch') : t('activity.empty')} text={dirty ? undefined : t('activity.emptyText')} />}
        />
      )}
      {next && (
        <div className="st-act-more">
          <Button loading={loading} onClick={() => void load(applied, next)}>{t('activity.more')}</Button>
        </div>
      )}
    </div>
  );
}
