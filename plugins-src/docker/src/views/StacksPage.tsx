import { useMemo, useState } from 'react';
import { stackAction, type Stack } from '../api/compose';
import { healthOf, isProblem } from '../api/model';
import { t, tn } from '../i18n';
import { Badge, Button, Chip, EmptyState, IconButton, Skeleton, toast } from '../kit';
import { navigate, useSearch } from '../router';
import { ErrorState } from '../ui/ErrorState';
import { PageHeader } from '../ui/PageHeader';
import { SetupCard } from './stack/SetupCard';
import { useStacks } from './stack/useStacks';

type Filter = 'all' | 'managed' | 'detected';

/** Route { view: 'stacks' }: compose stacks as cards, managed ones in /opt/stacks first, detected ones marked. */
export function StacksPage() {
  const { stacks, sources, loading, error, reload } = useStacks();
  const q = useSearch().trim().toLowerCase();
  const [filter, setFilter] = useState<Filter>('all');
  const [busy, setBusy] = useState<Set<string>>(new Set());

  const visible = useMemo(
    () =>
      stacks
        .filter((s) => (filter === 'all' ? true : filter === 'managed' ? s.managed : !s.managed))
        .filter((s) => !q || s.name.includes(q) || s.serviceNames.some((n) => n.toLowerCase().includes(q)))
        .sort((a, b) => Number(b.managed) - Number(a.managed) || a.name.localeCompare(b.name)),
    [stacks, filter, q],
  );
  const managed = stacks.filter((s) => s.managed).length;
  const running = stacks.filter((s) => s.running > 0).length;

  const quick = async (s: Stack, action: 'start' | 'stop' | 'restart') => {
    setBusy((b) => new Set(b).add(s.name));
    let last = '';
    try {
      const code = await stackAction(s.name, action, (_st, line) => { if (line.trim()) last = line.trim(); }, { detected: !s.managed });
      if (code === 0) toast.ok(t(`stacks.done.${action}`, { name: s.name }));
      else toast.err(t('stacks.runFail'), last || t('stacks.runFailText'));
    } catch (e) {
      toast.err(t('stacks.runFail'), (e as Error).message);
    } finally {
      setBusy((b) => { const n = new Set(b); n.delete(s.name); return n; });
      void reload();
    }
  };

  const header = (
    <PageHeader
      icon="layers"
      title={t('stacks.title')}
      subtitle={stacks.length ? tn('stacks.sub', { n: stacks.length, running }) : sources ? t('stacks.sub.none') : ''}
      actions={<Button variant="primary" icon="plus" onClick={() => navigate({ view: 'stack', name: '' })}>{t('stacks.new')}</Button>}
    />
  );

  if (!sources && error) return <>{header}<ErrorState error={error} onRetry={() => void reload()} /></>;
  if (loading) return <>{header}<div className="dk-sk-list">{Array.from({ length: 4 }, (_, i) => <Skeleton key={i} height={150} style={{ borderRadius: 18 }} />)}</div></>;

  return (
    <>
      {header}
      {sources && !sources.folder && <SetupCard onDone={() => void reload()} />}
      {stacks.length > 0 && (
        <div className="dk-chips" role="group" aria-label={t('stacks.title')}>
          {([['all', stacks.length], ['managed', managed], ['detected', stacks.length - managed]] as [Filter, number][]).map(([f, n]) => (
            <Chip key={f} pressed={filter === f} count={n} onClick={() => setFilter(f)}>{t(`stacks.filter.${f}`)}</Chip>
          ))}
        </div>
      )}
      {stacks.length === 0 ? (
        <EmptyState
          icon="layers"
          hue="file"
          title={t('stacks.empty.title')}
          text={t('stacks.empty.text')}
          action={
            <div className="dk-ph-act" style={{ margin: 0 }}>
              <Button variant="primary" icon="plus" onClick={() => navigate({ view: 'stack', name: '' })}>{t('stacks.new')}</Button>
              <Button icon="store" onClick={() => navigate({ view: 'templates' }, { root: true })}>{t('containers.empty.template')}</Button>
            </div>
          }
        />
      ) : visible.length === 0 ? (
        <EmptyState icon="search" hue="file" title={t('containers.noMatch.title')} text={t('containers.noMatch.text')} />
      ) : (
        <div className="dk-sk-list">
          {visible.map((s) => (
            <StackCard key={s.name} s={s} busy={busy.has(s.name)} onAction={(a) => void quick(s, a)} />
          ))}
        </div>
      )}
    </>
  );
}

function StackCard({ s, busy, onAction }: { s: Stack; busy: boolean; onAction(a: 'start' | 'stop' | 'restart'): void }) {
  const open = () => navigate({ view: 'stack', name: s.name });
  const stop = (e: { stopPropagation(): void }) => e.stopPropagation();
  const bad = s.containers.some((c) => isProblem(c) || healthOf(c) === 'unhealthy');
  const tone = s.total === 0 ? 'neutral' : s.running === s.total ? 'ok' : s.running ? 'warn' : 'neutral';
  const dot = bad ? 'err' : tone === 'ok' ? 'ok' : tone === 'warn' ? 'warn' : 'n';
  const names = s.serviceNames;
  return (
    <div className={`dk-sk-card${bad ? ' dk-sk-card--bad' : ''}`} role="link" tabIndex={0} aria-label={t('stacks.open', { name: s.name })} onClick={open} onKeyDown={(e) => { if (e.key === 'Enter' && e.target === e.currentTarget) open(); }}>
      <div className="dk-sk-card-t">
        <span className={`dk-dot dk-dot--${dot === 'n' ? 'n' : dot}`} />
        <div className="dk-sk-card-tx">
          <b title={s.name}>{s.name}</b>
          <span className="dk-sk-path" title={s.dir}>{s.dir || '–'}</span>
        </div>
        <Badge tone={s.managed ? 'ok' : 'info'}>{s.managed ? t('stacks.managed') : t('stacks.detected')}</Badge>
      </div>
      <div className="dk-sk-card-n">
        <span className="dk-sk-sc">{t(names.length === 1 ? 'stacks.serviceOne' : 'stacks.serviceMany', { n: names.length })}</span>
        <span className="dk-muted">{s.total ? t('stacks.runningOf', { running: s.running, total: s.total }) : t('stacks.notDeployed')}</span>
      </div>
      <div className="dk-sk-chips">
        {names.slice(0, 5).map((n) => {
          const sv = s.services.find((x) => x.name === n);
          return <span key={n} className={`dk-sk-chip dk-sk-chip--${sv?.state === 'running' ? 'ok' : sv?.state === 'restarting' ? 'warn' : 'off'}`}>{n}</span>;
        })}
        {names.length > 5 && <span className="dk-muted">+{names.length - 5}</span>}
      </div>
      <div className="dk-sk-card-f" onClick={stop}>
        <span className="dk-muted dk-sk-st">{s.status}</span>
        <span className="dk-sk-card-act">
          {s.total > 0 && (s.running > 0 ? <IconButton icon="stop" label={t('common.stop')} loading={busy} onClick={() => onAction('stop')} /> : <IconButton icon="play" label={t('common.start')} loading={busy} onClick={() => onAction('start')} />)}
          {s.total > 0 && <IconButton icon="refresh" label={t('common.restart')} disabled={busy} onClick={() => onAction('restart')} />}
          <IconButton icon="edit" label={t('stacks.openLabel')} onClick={open} />
        </span>
      </div>
    </div>
  );
}
