import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { ApiError, call, stream, usePrefs, useSession } from '../../api';
import { useI18n, useT } from '../../i18n';
import { Badge, Button, Chip, DropdownMenu, EmptyState, Icon, IconButton, Input, Page, Segmented, Skeleton, Switch, Table, Tabs, useIsMobile, useMediaQuery, type Column, type MenuItem } from '../../ui';
import { useRailBadge } from '../index';
import { bootMode, short, stateKey, stateTone } from './helpers';
import { UnitPanel } from './UnitPanel';
import { publishRunning, publishSummary } from './store';
import type { Action, FailedUnit, PanelTab, Summary, Unit, UnitKind } from './types';
import { PURPOSES } from './types';
import { useActions } from './useActions';
import { formatBytes, relativeTime } from '../../lib/format';
import { usePolling, useRefreshInterval } from '../../lib/refresh';
import './services.css';

type Tab = 'all' | 'running' | 'failed' | 'stopped' | 'timers' | 'sockets';
const TAB_IDS: Tab[] = ['all', 'running', 'failed', 'stopped', 'timers', 'sockets'];
type Purpose = 'all' | (typeof PURPOSES)[number];
const PURPOSE_HUE = { web: 'term', containers: 'plg', system: 'ov' } as const;

const kindOf = (tab: Tab): UnitKind => (tab === 'timers' ? 'timer' : tab === 'sockets' ? 'socket' : 'service');

export default function ServicesPage() {
  const t = useT('services');
  const { lang } = useI18n();
  const mobile = useIsMobile();
  const wide = useMediaQuery('(min-width: 1241px)');
  const nav = useNavigate();
  const [params, setParams] = useSearchParams();
  const { prefs, set: setPref } = usePrefs();
  const win = useSession().session?.os === 'windows';
  const view: 'table' | 'cards' = prefs['services.view'] === 'cards' ? 'cards' : 'table';

  // ?unit=nginx or ?unit=nginx.service (links from Overview / Logs); ?state=failed|running|… picks the tab.
  const unitParam = params.get('unit');
  const selected = unitParam ? (win || /\.[a-z]+$/.test(unitParam) ? unitParam : `${unitParam}.service`) : null;
  const rawTab = (params.get('tab') as PanelTab | null) ?? 'info';
  const panelTab: PanelTab = win && (rawTab === 'logs' || rawTab === 'unit') ? 'info' : rawTab;

  const stateParam = params.get('state');
  const tabOk = (v: string | null): v is Tab => TAB_IDS.includes(v as Tab) && !(win && (v === 'timers' || v === 'sockets'));
  const [tab, setTab] = useState<Tab>(() => (tabOk(stateParam) ? stateParam : 'all'));
  useEffect(() => {
    if (tabOk(stateParam)) setTab(stateParam);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stateParam, win]);
  const [purpose, setPurpose] = useState<Purpose>('all');
  const [q, setQ] = useState('');
  const kind = kindOf(tab);

  const [lists, setLists] = useState<Partial<Record<UnitKind, Unit[]>>>({});
  const [summary, setSummary] = useState<Summary | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [cpu, setCpu] = useState<Record<string, number>>({});
  const cpuPrev = useRef<Record<string, { ns: number; at: number }>>({});
  const [panelRev, setPanelRev] = useState(0);

  const loadSummary = useCallback(async () => {
    try {
      const s = await call<Summary>('services.summary');
      setSummary(s);
      publishSummary(s);
    } catch (e) {
      if (e instanceof ApiError) setError(e);
    }
  }, []);

  const loadList = useCallback(async (k: UnitKind, fresh = false) => {
    try {
      const r = await call<{ units: Unit[] }>('services.list', { type: k, fresh });
      const now = Date.now();
      const pct: Record<string, number> = {};
      for (const u of r.units) {
        if (u.cpuNs == null) continue;
        const p = cpuPrev.current[u.name];
        if (p && now > p.at && u.cpuNs >= p.ns) pct[u.name] = ((u.cpuNs - p.ns) / ((now - p.at) * 1e6)) * 100;
        cpuPrev.current[u.name] = { ns: u.cpuNs, at: now };
      }
      setCpu((c) => ({ ...c, ...pct }));
      setLists((l) => ({ ...l, [k]: r.units }));
      if (k === 'service') publishRunning(r.units);
      setError(null);
    } catch (e) {
      if (e instanceof ApiError) setError(e);
    }
  }, []);

  // Initial and periodic refresh (memory/CPU change without a state change).
  useEffect(() => {
    void loadSummary();
    void loadList(kind);
  }, [kind, loadList, loadSummary]);
  // Live refresh at the user's rate (the list is a heavier call, so never faster than 2 s).
  const refresh = Math.max(useRefreshInterval(), 2000);
  usePolling(() => {
    void loadList(kind);
    void loadSummary();
  }, refresh);

  // Live state changes pushed by the daemon.
  useEffect(() => {
    let timer: number | undefined;
    const s = stream<{ units: Unit[]; removed: string[] }>('services.watch', { type: kind }, {
      reopen: true,
      onData: (ev) => {
        setLists((l) => {
          const cur = l[kind];
          if (!cur) return l;
          const map = new Map(cur.map((u) => [u.name, u]));
          for (const n of ev.removed ?? []) {
            const old = map.get(n);
            // Gone from systemd's loaded set: still installed units stay as stopped.
            if (old) map.set(n, { ...old, load: 'not-loaded', active: 'inactive', sub: 'dead', state: 'stopped', pid: 0, memory: null, cpuNs: null });
          }
          for (const u of ev.units ?? []) map.set(u.name, { ...map.get(u.name), ...u, enabled: u.enabled || map.get(u.name)?.enabled || '' });
          const next = [...map.values()].sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
          if (kind === 'service') publishRunning(next);
          return { ...l, [kind]: next };
        });
        window.clearTimeout(timer);
        timer = window.setTimeout(() => {
          void loadSummary();
          setPanelRev((n) => n + 1);
        }, 300);
      },
      onError: () => undefined,
    });
    return () => {
      window.clearTimeout(timer);
      s.close();
    };
  }, [kind, loadSummary]);

  const failed = summary?.failed ?? [];
  useRailBadge('services', failed.length);

  const refreshAfter = useCallback(
    (_name: string, action: Action) => {
      void loadList(kind, action === 'enable' || action === 'disable' || action === 'mask' || action === 'unmask');
      void loadSummary();
      setPanelRev((n) => n + 1);
    },
    [kind, loadList, loadSummary],
  );
  const { run, busy, dialog } = useActions(refreshAfter);

  const openUnit = (name: string, tabId: PanelTab = 'info', replace = false) => {
    const p = new URLSearchParams(params);
    p.set('unit', name);
    if (tabId === 'info') p.delete('tab');
    else p.set('tab', tabId);
    setParams(p, { replace });
  };
  const closePanel = () => {
    const p = new URLSearchParams(params);
    p.delete('unit');
    p.delete('tab');
    setParams(p, { replace: true });
  };
  const setPanelTab = (tb: PanelTab) => openUnit(selected ?? '', tb, true);

  const all = lists[kind];
  const rows = useMemo(() => {
    if (!all) return [];
    const needle = q.trim().toLowerCase();
    return all.filter((u) => {
      if (kind === 'service') {
        if (tab === 'running' && u.state !== 'running' && u.state !== 'finished') return false;
        if (tab === 'failed' && u.state !== 'failed') return false;
        if (tab === 'stopped' && u.state !== 'stopped') return false;
      }
      if (purpose !== 'all' && u.purpose !== purpose) return false;
      return !needle || u.name.toLowerCase().includes(needle) || u.description.toLowerCase().includes(needle);
    });
  }, [all, kind, tab, purpose, q]);

  const allTabs = [
    { id: 'all' as const, label: t('tabs.all'), count: summary?.total },
    { id: 'running' as const, label: t('tabs.running'), count: summary?.running },
    { id: 'failed' as const, label: t('tabs.failed'), count: summary ? failed.length : undefined },
    { id: 'stopped' as const, label: t('tabs.stopped'), count: summary?.stopped },
    { id: 'timers' as const, label: t('tabs.timers'), count: summary?.timers },
    { id: 'sockets' as const, label: t('tabs.sockets'), count: summary?.sockets },
  ];
  const tabs = win ? allTabs.filter((x) => x.id !== 'timers' && x.id !== 'sockets') : allTabs;
  const showPurpose = !win;
  const bootLabel = (v: string) => (win ? t(`win.boot.${v}`) || t(`boot.${v}`) : v ? t(`boot.${v}`) : '');

  const toggleBoot = (u: Unit) => run(u.name, bootMode(u.enabled) === 'on' ? 'disable' : 'enable');

  const bootCell = (u: Unit) => {
    const m = bootMode(u.enabled);
    if (m === 'fixed') return <span className="svc-fixed">{u.enabled ? bootLabel(u.enabled) : '—'}</span>;
    return <Switch checked={m === 'on'} disabled={busy.has(u.name)} onChange={() => toggleBoot(u)} aria-label={t('boot.label', { name: short(u.name) })} />;
  };

  const menuFor = (u: Unit): MenuItem[] => {
    const running = u.state === 'running' || u.state === 'finished';
    const isMasked = win ? u.enabled === 'masked' : u.load === 'masked';
    return [
      running ? { id: 'stop', label: t('actions.stop'), icon: 'power', onSelect: () => run(u.name, 'stop') } : { id: 'start', label: t('actions.start'), icon: 'play', onSelect: () => run(u.name, 'start') },
      { id: 'restart', label: t('actions.restart'), icon: 'refresh', onSelect: () => run(u.name, 'restart') },
      ...(win
        ? [
            u.sub === 'paused'
              ? { id: 'continue', label: t('actions.continue'), icon: 'play' as const, onSelect: () => run(u.name, 'continue') }
              : { id: 'pause', label: t('actions.pause'), icon: 'power' as const, disabled: !running, onSelect: () => run(u.name, 'pause') },
          ]
        : [
            { id: 'reload', label: t('actions.reload'), icon: 'refresh' as const, disabled: !running, onSelect: () => run(u.name, 'reload') },
            { type: 'separator' as const },
            { id: 'logs', label: t('rowMenu.logs'), icon: 'logs' as const, onSelect: () => openUnit(u.name, 'logs') },
            { id: 'file', label: t('rowMenu.unitFile'), icon: 'file' as const, onSelect: () => openUnit(u.name, 'unit') },
            { id: 'inlogs', label: t('openInLogs'), icon: 'externallink' as const, onSelect: () => nav(`/logs?unit=${encodeURIComponent(u.name)}`) },
          ]),
      { type: 'separator' },
      isMasked
        ? { id: 'unmask', label: win ? t('win.unmask') : t('actions.unmask'), icon: 'unlock', onSelect: () => run(u.name, 'unmask') }
        : { id: 'mask', label: win ? t('win.mask') : t('actions.mask'), icon: 'lock', danger: true, onSelect: () => run(u.name, 'mask') },
    ];
  };

  const rowActions = (u: Unit) => (
    <span className="svc-rowact" onClick={(e) => e.stopPropagation()}>
      {kind === 'service' && <IconButton icon="refresh" label={t('actions.restart')} disabled={busy.has(u.name)} onClick={() => run(u.name, 'restart')} />}
      <DropdownMenu items={menuFor(u)} aria-label={t('rowMenu.label', { name: short(u.name) })} trigger={(p) => <IconButton icon="more" label={t('more')} {...p} />} />
    </span>
  );

  const nameCell = (u: Unit) => (
    <div className="svc-nm">
      <b>{u.name}</b>
      <small>{u.description}</small>
    </div>
  );
  const stateCell = (u: Unit) => <Badge tone={stateTone(u)}>{t(`state.${stateKey(u)}`)}</Badge>;
  const dash = <span className="svc-muted">—</span>;

  const columns: Column<Unit>[] = [
    { key: 'name', header: t('col.name'), sortable: true, value: (u) => u.name, render: nameCell },
    { key: 'state', header: t('col.state'), sortable: true, value: (u) => u.state, render: stateCell },
    { key: 'boot', header: t('col.boot'), render: bootCell },
  ];
  const compact = mobile || (!!selected && wide);
  if (kind === 'service') {
    if (!compact) {
      columns.push(
        { key: 'mem', header: t('col.memory'), sortable: true, mono: true, value: (u) => u.memory ?? -1, render: (u) => (u.memory != null ? formatBytes(u.memory) : dash) },
        { key: 'cpu', header: t('col.cpu'), sortable: true, mono: true, value: (u) => cpu[u.name] ?? -1, render: (u) => (cpu[u.name] != null && u.state === 'running' ? `${cpu[u.name].toFixed(1)}%` : dash) },
      );
    }
  } else if (kind === 'timer') {
    columns.push({ key: 'next', header: t('col.next'), sortable: true, value: (u) => u.next ?? 0, render: (u) => (u.next ? relativeTime(u.next * 1000, lang) : dash) });
    if (!compact) columns.push({ key: 'last', header: t('col.last'), sortable: true, value: (u) => u.last ?? 0, render: (u) => (u.last ? relativeTime(u.last * 1000, lang) : dash) });
  } else if (!compact) {
    columns.push({ key: 'listen', header: t('col.listen'), mono: true, render: (u) => (u.listen?.length ? u.listen.map((l) => l.replace(/ \((Stream|Datagram|SequentialPacket|Netlink|Special|FIFO)\)$/, '')).join(', ') : dash) });
  }
  columns.push({ key: 'act', header: '', align: 'right', render: rowActions });

  const banner = failed.length > 0 && kind === 'service' && <FailedBanner failed={failed} t={t} win={win} onLog={(n) => openUnit(n, 'logs')} onRestart={(n) => run(n, 'restart')} onDisable={(n) => run(n, 'disable')} onShow={() => setTab('failed')} />;

  const grouped: { p: (typeof PURPOSES)[number] | null; units: Unit[] }[] = win
    ? [{ p: null, units: rows }]
    : PURPOSES.map((p) => ({ p, units: rows.filter((u) => u.purpose === p) })).filter((g) => g.units.length);

  return (
    <Page flush hue="svc">
      <div className={`svc-root ${selected && wide ? 'svc-has-panel' : ''}`}>
        <div className="svc-main">
          <div className="svc-bar">
            <Tabs variant="pill" hue="svc" items={tabs} value={tab} onChange={(id) => setTab(id)} aria-label={t('title')} />
            <div className="svc-sp" />
            {kind === 'service' && showPurpose && (
              <div className="svc-chips" role="group" aria-label={t('purpose.label')}>
                <Chip hue="svc" pressed={purpose === 'all'} onClick={() => setPurpose('all')}>{t('purpose.all')}</Chip>
                {PURPOSES.map((p) => (
                  <Chip key={p} hue={PURPOSE_HUE[p]} pressed={purpose === p} onClick={() => setPurpose(p)}>
                    <span className={`svc-dot svc-dot--${p}`} />
                    {t(`purpose.${p}`)}
                  </Chip>
                ))}
              </div>
            )}
            <div className="svc-filter">
              <Input icon="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('filter')} aria-label={t('filter')} />
            </div>
            <Segmented
              value={view}
              onChange={(v) => void setPref('services.view', v)}
              aria-label={t('view.label')}
              options={[
                { value: 'table', icon: 'list', label: t('view.table') },
                { value: 'cards', icon: 'grid', label: t('view.cards') },
              ]}
            />
          </div>
          {banner}
          <div className="svc-body">
            {error && !all ? (
              <EmptyState icon="alert" hue="svc" title={t('error.title')} text={error.message} action={<Button onClick={() => { setError(null); void loadList(kind); void loadSummary(); }}>{t('retry')}</Button>} />
            ) : !all ? (
              <div className="svc-skel"><Skeleton lines={8} height={44} /></div>
            ) : view === 'table' ? (
              <Table
                columns={columns}
                rows={rows}
                rowKey={(u) => u.name}
                activeKey={selected ?? undefined}
                onRowClick={(u) => openUnit(u.name)}
                caption={t('title')}
                empty={<EmptyState icon="services" hue="svc" title={t('empty.title')} text={t('empty.text')} />}
              />
            ) : rows.length === 0 ? (
              <EmptyState icon="services" hue="svc" title={t('empty.title')} text={t('empty.text')} />
            ) : (
              <div className="svc-cards-wrap">
                {grouped.map((g) => (
                  <section key={g.p ?? 'all'}>
                    {g.p && <h3 className="svc-gt"><span className={`svc-dot svc-dot--${g.p}`} />{t(`purpose.${g.p}`)} <span className="svc-muted">{g.units.length}</span></h3>}
                    <div className="svc-cards">
                      {g.units.map((u) => (
                        <UnitCard key={u.name} u={u} bad={u.state === 'failed'} busy={busy.has(u.name)} active={selected === u.name} mem={u.memory} cpu={cpu[u.name]} t={t} boot={bootCell(u)} menu={menuFor(u)}
                          onOpen={() => openUnit(u.name)} onAct={(a) => run(u.name, a)} onLogs={win ? undefined : () => openUnit(u.name, 'logs')} />
                      ))}
                    </div>
                  </section>
                ))}
              </div>
            )}
          </div>
        </div>
        {selected && (
          <UnitPanel
            key={selected}
            name={selected}
            tab={panelTab}
            onTab={setPanelTab}
            onClose={closePanel}
            inline={wide}
            rev={panelRev}
            row={all?.find((u) => u.name === selected)}
            run={run}
            busy={busy.has(selected)}
            onOpenUnit={(n) => openUnit(n)}
            onChanged={() => refreshAfter(selected, 'enable')}
          />
        )}
      </div>
      {dialog}
    </Page>
  );
}

function FailedBanner({ failed, win, t, onLog, onRestart, onDisable, onShow }: { failed: FailedUnit[]; win: boolean; t: ReturnType<typeof useT>; onLog(n: string): void; onRestart(n: string): void; onDisable(n: string): void; onShow(): void }) {
  const one = failed.length === 1 ? failed[0] : null;
  let text: string;
  if (one) {
    const reason = one.result === 'exit-code' ? t('banner.exitCode', { name: short(one.name), code: one.exitCode }) : one.result ? t('banner.result', { name: short(one.name), result: one.result }) : t('banner.failed', { name: short(one.name) });
    const hint = one.hintId ? t(`hints.${one.hintId}`) || one.hint : one.hint;
    text = hint ? `${reason} ${hint}` : reason;
  } else {
    const names = failed.slice(0, 3).map((f) => short(f.name)).join(', ');
    text = failed.length > 3 ? t('banner.many', { names, more: failed.length - 3 }) : names;
  }
  return (
    <div className="svc-alert" role="alert">
      <Icon name="alert" />
      <div className="svc-alert-tx">
        <b>{t('banner.title', { count: failed.length })}</b> <span>{text}</span>
      </div>
      <div className="svc-alert-act">
        {one ? (
          <>
            {!win && <Button size="sm" onClick={() => onLog(one.name)}>{t('banner.log')}</Button>}
            <Button size="sm" onClick={() => onRestart(one.name)}>{t('actions.restart')}</Button>
            {!win && <Button size="sm" onClick={() => onDisable(one.name)}>{t('actions.disable')}</Button>}
          </>
        ) : (
          <Button size="sm" onClick={onShow}>{t('banner.show')}</Button>
        )}
      </div>
    </div>
  );
}

function UnitCard({ u, bad, busy, active, mem, cpu, t, boot, menu, onOpen, onAct, onLogs }: { u: Unit; bad: boolean; busy: boolean; active: boolean; mem: number | null; cpu?: number; t: ReturnType<typeof useT>; boot: React.ReactNode; menu: MenuItem[]; onOpen(): void; onAct(a: Action): void; onLogs?: () => void }) {
  const running = u.state === 'running' || u.state === 'finished';
  return (
    <div className={`svc-card${bad ? ' svc-card--bad' : ''}${active ? ' svc-card--on' : ''}`}>
      <button className="svc-card-hd" type="button" onClick={onOpen}>
        <span className={`svc-tile svc-tile--${bad ? 'bad' : running ? 'ok' : 'off'}`}><Icon name="services" /></span>
        <span className="svc-card-tx">
          <b>{u.name}</b>
          <small>{u.description}</small>
        </span>
      </button>
      <div className="svc-card-m">
        <Badge tone={stateTone(u)}>{t(`state.${stateKey(u)}`)}</Badge>
        <span className="svc-muted svc-mono">{mem != null ? formatBytes(mem) : ''}{mem != null && cpu != null && u.state === 'running' ? ` · ${cpu.toFixed(1)}%` : ''}</span>
      </div>
      <div className="svc-card-f">
        <label className="svc-boot">{boot}<span>{t('boot.short')}</span></label>
        <div className="svc-q">
          <IconButton icon="refresh" label={t('actions.restart')} disabled={busy} onClick={() => onAct('restart')} />
          <IconButton icon={running ? 'power' : 'play'} label={running ? t('actions.stop') : t('actions.start')} disabled={busy} onClick={() => onAct(running ? 'stop' : 'start')} />
          {onLogs && <IconButton icon="logs" label={t('rowMenu.logs')} onClick={onLogs} />}
          <DropdownMenu items={menu} trigger={(p) => <IconButton icon="more" label={t('more')} {...p} />} />
        </div>
      </div>
    </div>
  );
}

