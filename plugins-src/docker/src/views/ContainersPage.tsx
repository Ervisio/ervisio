import { useEffect, useMemo, useState } from 'react';
import { containerAction, removeContainer, runBulk, type ContainerAction } from '../api/actions';
import { resetEngine } from '../api/engine';
import { containerName, formatBytes, formatPercent, localStatus, shortImage } from '../api/format';
import { useStats } from '../api/hooks';
import { countContainers, groupByStack, isProblem, isStopped, matchesContainer, portUrl, publishedPorts, healthOf, type StackGroup } from '../api/model';
import { containers, info } from '../api/resources';
import type { Container } from '../api/types';
import { useFile } from '../settings';
import { t } from '../i18n';
import { Button, Checkbox, Chip, ConfirmDialog, EmptyState, Icon, IconButton, Skeleton, toast } from '../kit';
import { navigate, useSearch } from '../router';
import { MiniMeter, MiniSpark } from '../ui/Charts';
import { ErrorState } from '../ui/ErrorState';
import { PageHeader } from '../ui/PageHeader';
import { StatusDot } from '../ui/StatusDot';
import { openUrl } from '../ui/openUrl';

type Filter = 'all' | 'running' | 'stopped' | 'unhealthy';
type Bulk = Exclude<ContainerAction, 'kill' | 'pause' | 'unpause'>;

const matchesFilter = (c: Container, f: Filter) =>
  f === 'all' ? true : f === 'running' ? c.State === 'running' : f === 'stopped' ? isStopped(c) : isProblem(c);

/** Containers home: cards grouped by compose stack, live CPU and memory, quick actions, bulk bar. */
export function ContainersPage() {
  const { data, error, loading } = containers.use();
  const q = useSearch();
  const [filter, setFilter] = useState<Filter>('all');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState<Set<string>>(new Set());
  const [removing, setRemoving] = useState<Container[] | null>(null);
  const [rmVolumes, setRmVolumes] = useState(false);
  const [settings] = useFile('settings');
  const ncpu = info.use().data?.info.NCPU || 1;

  const all = data ?? [];
  const counts = countContainers(all);
  const byId = useMemo(() => new Map(all.map((c) => [c.Id, c])), [all]);

  // Forget selected containers that no longer exist.
  useEffect(() => {
    if (!data) return;
    setSelected((s) => {
      const keep = [...s].filter((id) => byId.has(id));
      return keep.length === s.size ? s : new Set(keep);
    });
  }, [data, byId]);

  const visible = useMemo(() => all.filter((c) => matchesFilter(c, filter) && matchesContainer(c, q)), [all, filter, q]);
  const groups = useMemo(() => groupByStack(visible), [visible]);

  const mark = (ids: string[], on: boolean) =>
    setBusy((b) => {
      const n = new Set(b);
      ids.forEach((id) => (on ? n.add(id) : n.delete(id)));
      return n;
    });

  const act = async (c: Container, action: Bulk) => {
    const name = containerName(c);
    mark([c.Id], true);
    try {
      await containerAction(c.Id, action);
      toast.ok(t(`containers.done.${action}`, { name }));
    } catch (e) {
      toast.err(t(`containers.fail.${action}`, { name }), (e as Error).message);
    } finally {
      mark([c.Id], false);
    }
  };

  const bulk = async (list: Container[], action: Bulk) => {
    const ids = list.map((c) => c.Id);
    mark(ids, true);
    const failed = await runBulk(ids, (id) => containerAction(id, action));
    mark(ids, false);
    if (failed.length) toast.err(t('containers.bulkFail', { failed: failed.length, total: ids.length }), failed[0].message);
    else toast.ok(t(`containers.bulkDone.${action}`, { n: ids.length }));
    if (action === 'stop') setSelected(new Set());
  };

  const doRemove = async () => {
    const list = removing ?? [];
    const ids = list.map((c) => c.Id);
    mark(ids, true);
    const failed = await runBulk(ids, (id) => removeContainer(id, { force: true, volumes: rmVolumes }));
    mark(ids, false);
    setRemoving(null);
    setRmVolumes(false);
    setSelected(new Set());
    if (failed.length) toast.err(t('containers.bulkFail', { failed: failed.length, total: ids.length }), failed[0].message);
    else toast.ok(list.length === 1 ? t('containers.done.remove', { name: containerName(list[0]) }) : t('containers.bulkDone.remove', { n: list.length }));
  };

  const toggle = (id: string, on: boolean) =>
    setSelected((s) => {
      const n = new Set(s);
      if (on) n.add(id);
      else n.delete(id);
      return n;
    });

  const subtitle = counts.total
    ? [
        counts.running && t('containers.sub.running', { n: counts.running }),
        counts.restarting && t('containers.sub.restarting', { n: counts.restarting }),
        counts.paused && t('containers.sub.paused', { n: counts.paused }),
        counts.stopped && t('containers.sub.stopped', { n: counts.stopped }),
      ].filter(Boolean).join(', ')
    : data
      ? t('containers.sub.none')
      : '';

  const header = (
    <PageHeader
      icon="box"
      title={t('containers.title')}
      subtitle={subtitle}
      actions={<Button variant="primary" icon="plus" onClick={() => navigate({ view: 'create' })}>{t('containers.new')}</Button>}
    />
  );

  if (!data && error) return <>{header}<ErrorState error={error} onRetry={() => { resetEngine(); void containers.refresh(); }} /></>;
  if (!data && loading) {
    return (
      <>
        {header}
        <div className="dk-grid">{Array.from({ length: 6 }, (_, i) => <Skeleton key={i} height={132} style={{ borderRadius: 18 }} />)}</div>
      </>
    );
  }

  const chips: [Filter, number][] = [
    ['all', counts.total],
    ['running', counts.running],
    ['stopped', counts.stopped],
    ['unhealthy', counts.problems],
  ];
  const chosen = [...selected].map((id) => byId.get(id)).filter((c): c is Container => !!c);

  return (
    <>
      {header}
      {error && <p className="dk-muted dk-stale">{t('containers.stale')}</p>}
      {counts.total > 0 && (
        <div className="dk-chips" role="group" aria-label={t('containers.title')}>
          {chips.map(([f, n]) => (
            <Chip key={f} pressed={filter === f} count={n} onClick={() => setFilter(f)}>{t(`containers.filter.${f}`)}</Chip>
          ))}
        </div>
      )}
      {counts.total === 0 ? (
        <EmptyState
          icon="box"
          hue="file"
          title={t('containers.empty.title')}
          text={t('containers.empty.text')}
          action={
            <div className="dk-ph-act" style={{ margin: 0 }}>
              <Button variant="primary" icon="plus" onClick={() => navigate({ view: 'create' })}>{t('containers.new')}</Button>
              <Button icon="store" onClick={() => navigate({ view: 'templates' }, { root: true })}>{t('containers.empty.template')}</Button>
            </div>
          }
        />
      ) : groups.length === 0 ? (
        <EmptyState icon="search" hue="file" title={t('containers.noMatch.title')} text={t('containers.noMatch.text')} />
      ) : (
        <div className="dk-groups">
          {groups.map((g) => (
            <StackSection
              key={g.name || '\u0000standalone'}
              group={g}
              selected={selected}
              busy={busy}
              liveStats={settings.liveStats}
              ncpu={ncpu}
              onToggle={toggle}
              onSelectGroup={(on) => setSelected((s) => { const n = new Set(s); g.containers.forEach((c) => (on ? n.add(c.Id) : n.delete(c.Id))); return n; })}
              onAction={act}
              onRestartStack={() => bulk(g.containers, 'restart')}
            />
          ))}
        </div>
      )}

      {chosen.length > 0 && (
        <div className="dk-bulk" role="toolbar" aria-label={t('containers.selected', { n: chosen.length })}>
          <b>{t('containers.selected', { n: chosen.length })}</b>
          <button type="button" onClick={() => bulk(chosen, 'start')}><Icon name="play" />{t('common.start')}</button>
          <button type="button" onClick={() => bulk(chosen, 'stop')}><Icon name="stop" />{t('common.stop')}</button>
          <button type="button" onClick={() => bulk(chosen, 'restart')}><Icon name="refresh" />{t('common.restart')}</button>
          <button type="button" onClick={() => setRemoving(chosen)}><Icon name="trash" />{t('common.remove')}</button>
          <button type="button" aria-label={t('containers.clearSelection')} title={t('containers.clearSelection')} onClick={() => setSelected(new Set())}><Icon name="close" /></button>
        </div>
      )}

      <ConfirmDialog
        open={!!removing}
        onClose={() => { setRemoving(null); setRmVolumes(false); }}
        onConfirm={doRemove}
        title={removing?.length === 1 ? t('containers.removeTitle', { name: containerName(removing[0]) }) : t('containers.removeMany', { n: removing?.length ?? 0 })}
        description={t('containers.removeDesc')}
        confirmLabel={t('common.remove')}
        confirmText={removing?.length === 1 ? containerName(removing[0]) : String(removing?.length ?? '')}
        icon="trash"
      >
        <div className="dk-rm-list">{(removing ?? []).slice(0, 12).map((c) => <div key={c.Id}>{containerName(c)}</div>)}{(removing?.length ?? 0) > 12 && <div>…</div>}</div>
        <Checkbox checked={rmVolumes} onChange={setRmVolumes} label={t('containers.removeVolumes')} />
      </ConfirmDialog>
    </>
  );
}

function StackSection({ group, selected, busy, liveStats, ncpu, onToggle, onSelectGroup, onAction, onRestartStack }: {
  group: StackGroup;
  selected: Set<string>;
  busy: Set<string>;
  liveStats: boolean;
  ncpu: number;
  onToggle(id: string, on: boolean): void;
  onSelectGroup(on: boolean): void;
  onAction(c: Container, a: Bulk): void;
  onRestartStack(): void;
}) {
  const allOn = group.containers.every((c) => selected.has(c.Id));
  return (
    <section className="dk-stack">
      <div className="dk-stack-h">
        <span className="dk-stack-n"><Icon name={group.name ? 'layers' : 'box'} />{group.name || t('containers.standalone')}</span>
        <span className="dk-muted">{t('containers.stackRunning', { running: group.running, total: group.containers.length })}</span>
        <span className="dk-stack-act">
          <Checkbox checked={allOn} indeterminate={!allOn && group.containers.some((c) => selected.has(c.Id))} onChange={onSelectGroup} label={t('containers.selectAll')} />
          {group.name && <Button size="sm" variant="ghost" icon="edit" onClick={() => navigate({ view: 'stack', name: group.name })}>{t('containers.openStack')}</Button>}
          {group.name && <Button size="sm" variant="ghost" icon="refresh" onClick={onRestartStack}>{t('containers.restartStack')}</Button>}
        </span>
      </div>
      <div className="dk-grid">
        {group.containers.map((c) => (
          <ContainerCard key={c.Id} c={c} selected={selected.has(c.Id)} busy={busy.has(c.Id)} live={liveStats} ncpu={ncpu} onToggle={(on) => onToggle(c.Id, on)} onAction={(a) => onAction(c, a)} />
        ))}
      </div>
    </section>
  );
}

function ContainerCard({ c, selected, busy, live, ncpu, onToggle, onAction }: {
  c: Container;
  selected: boolean;
  busy: boolean;
  live: boolean;
  /** CPU threads of the host: a full bar is every thread busy. */
  ncpu: number;
  onToggle(on: boolean): void;
  onAction(a: Bulk): void;
}) {
  const name = containerName(c);
  const running = c.State === 'running';
  const { last, history } = useStats(c.Id, running && live);
  const bad = isProblem(c);
  const ports = publishedPorts(c).filter((p) => p.proto === 'tcp');
  const open = () => navigate({ view: 'container', id: c.Id });
  const stop = (e: { stopPropagation(): void }) => e.stopPropagation();
  return (
    <div
      className={`dk-cc${bad ? ' dk-cc--bad' : ''}${selected ? ' dk-cc--sel' : ''}`}
      role="link"
      tabIndex={0}
      aria-label={t('containers.openContainer', { name })}
      onClick={open}
      onKeyDown={(e) => { if (e.key === 'Enter' && e.target === e.currentTarget) open(); }}
    >
      <div className="dk-cc-t">
        <span className="dk-cc-ck" onClick={stop}><Checkbox checked={selected} onChange={onToggle} aria-label={t('containers.select', { name })} /></span>
        <StatusDot state={c.State} unhealthy={healthOf(c) === 'unhealthy'} />
        <div className="dk-cc-tx">
          <b title={name}>{name}</b>
          <span className="dk-cc-img" title={c.Image}>{shortImage(c.Image)}</span>
        </div>
      </div>
      <div className="dk-cc-m">
        <div>
          <small>{t('containers.cpu')}<span>{last ? formatPercent(last.cpu) : '–'}</span></small>
          <MiniMeter value={last?.cpu ?? 0} max={ncpu * 100} label={t('containers.cpu')} />
        </div>
        <div>
          <small>{t('containers.memory')}<span>{last ? formatBytes(last.memUsed) : '–'}</span></small>
          <MiniMeter value={last?.memPct ?? 0} max={100} label={t('containers.memory')} />
        </div>
      </div>
      {running && live && <MiniSpark values={history.map((p) => p.cpu)} />}
      <span className="dk-cc-st" title={localStatus(c.Status)}>{localStatus(c.Status)}</span>
      <div className="dk-cc-f">
        <div className="dk-cc-ports">
          {ports.slice(0, 3).map((p) => (
            <button key={p.host} type="button" className="dk-port" title={t('containers.port', { port: p.host })} onClick={(e) => { stop(e); openUrl(portUrl(p)); }}>
              {p.host}<Icon name="externallink" />
            </button>
          ))}
          {ports.length > 3 && <span className="dk-muted">+{ports.length - 3}</span>}
        </div>
        <div className="dk-cc-act" onClick={stop}>
          {c.State === 'running' || c.State === 'restarting' || c.State === 'paused' ? (
            <IconButton icon="stop" label={t('common.stop')} loading={busy} onClick={() => onAction('stop')} />
          ) : (
            <IconButton icon="play" label={t('common.start')} loading={busy} onClick={() => onAction('start')} />
          )}
          <IconButton icon="refresh" label={t('common.restart')} disabled={busy} onClick={() => onAction('restart')} />
        </div>
      </div>
    </div>
  );
}
