import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, call, stream, usePrefs, useSession } from '../../api';
import { useT } from '../../i18n';
import {
  Button,
  Chip,
  Icon,
  Input,
  Page,
  Segmented,
  Tabs,
  useIsMobile,
  useMediaQuery,
} from '../../ui';
import { useRailBadge } from '../index';
import { short } from './helpers';
import { UnitPanel } from './UnitPanel';
import { publishRunning, publishSummary } from './store';
import type { Action, FailedUnit, Summary, Unit, UnitKind } from './types';
import { PURPOSES } from './types';
import { useActions } from './useActions';
import { ServicesBody, type View } from './ServicesBody';
import { isRunning, useServiceCells } from './useServiceCells';
import { useServicesRoute, type Tab } from './useServicesRoute';
import { usePolling, useRefreshInterval } from '../../lib/refresh';
import './services.css';

type Purpose = 'all' | (typeof PURPOSES)[number];
const PURPOSE_HUE = { web: 'term', containers: 'plg', system: 'ov' } as const;
type T = ReturnType<typeof useT>;
type CpuSample = { ns: number; at: number };
type WatchEvent = { units: Unit[]; removed: string[] };

const kindOf = (tab: Tab): UnitKind =>
  tab === 'timers' ? 'timer' : tab === 'sockets' ? 'socket' : 'service';

/** CPU % per unit since its previous sample; records the new samples in prev. */
function sampleCpu(
  units: Unit[],
  prev: Record<string, CpuSample>,
  now: number,
): Record<string, number> {
  const pct: Record<string, number> = {};
  for (const u of units) {
    if (u.cpuNs == null) continue;
    const p = prev[u.name];
    if (p && now > p.at && u.cpuNs >= p.ns)
      pct[u.name] = ((u.cpuNs - p.ns) / ((now - p.at) * 1e6)) * 100;
    prev[u.name] = { ns: u.cpuNs, at: now };
  }
  return pct;
}

function mergeWatchEvent(cur: Unit[], ev: WatchEvent): Unit[] {
  const map = new Map(cur.map((u) => [u.name, u]));
  for (const n of ev.removed ?? []) {
    const old = map.get(n);
    // Gone from systemd's loaded set: still installed units stay as stopped.
    if (old)
      map.set(n, {
        ...old,
        load: 'not-loaded',
        active: 'inactive',
        sub: 'dead',
        state: 'stopped',
        pid: 0,
        memory: null,
        cpuNs: null,
      });
  }
  for (const u of ev.units ?? [])
    map.set(u.name, {
      ...map.get(u.name),
      ...u,
      enabled: u.enabled || map.get(u.name)?.enabled || '',
    });
  return [...map.values()].sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
}

function matchesTab(u: Unit, tab: Tab): boolean {
  if (tab === 'running') return isRunning(u);
  if (tab === 'failed') return u.state === 'failed';
  if (tab === 'stopped') return u.state === 'stopped';
  return true;
}

function filterUnits(all: Unit[], kind: UnitKind, tab: Tab, purpose: Purpose, q: string): Unit[] {
  const needle = q.trim().toLowerCase();
  return all.filter((u) => {
    if (kind === 'service' && !matchesTab(u, tab)) return false;
    if (purpose !== 'all' && u.purpose !== purpose) return false;
    return (
      !needle ||
      u.name.toLowerCase().includes(needle) ||
      u.description.toLowerCase().includes(needle)
    );
  });
}

/** Live state changes pushed by the daemon, merged into the list of the given kind. */
function useUnitWatch(
  kind: UnitKind,
  setLists: React.Dispatch<React.SetStateAction<Partial<Record<UnitKind, Unit[]>>>>,
  loadSummary: () => Promise<void>,
  setPanelRev: React.Dispatch<React.SetStateAction<number>>,
) {
  useEffect(() => {
    let timer: number | undefined;
    const s = stream<WatchEvent>(
      'services.watch',
      { type: kind },
      {
        reopen: true,
        onData: (ev) => {
          setLists((l) => {
            const cur = l[kind];
            if (!cur) return l;
            const next = mergeWatchEvent(cur, ev);
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
      },
    );
    return () => {
      window.clearTimeout(timer);
      s.close();
    };
  }, [kind, loadSummary, setLists, setPanelRev]);
}

function useUnitData(kind: UnitKind) {
  const [lists, setLists] = useState<Partial<Record<UnitKind, Unit[]>>>({});
  const [summary, setSummary] = useState<Summary | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [cpu, setCpu] = useState<Record<string, number>>({});
  const cpuPrev = useRef<Record<string, CpuSample>>({});
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
      const pct = sampleCpu(r.units, cpuPrev.current, Date.now());
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

  useUnitWatch(kind, setLists, loadSummary, setPanelRev);

  const refreshAfter = useCallback(
    (_name: string, action: Action) => {
      void loadList(
        kind,
        action === 'enable' || action === 'disable' || action === 'mask' || action === 'unmask',
      );
      void loadSummary();
      setPanelRev((n) => n + 1);
    },
    [kind, loadList, loadSummary, setPanelRev],
  );
  const retry = () => {
    setError(null);
    void loadList(kind);
    void loadSummary();
  };

  return { lists, summary, error, cpu, panelRev, refreshAfter, retry };
}

function tabItems(t: T, summary: Summary | null, win: boolean) {
  const allTabs = [
    { id: 'all' as const, label: t('tabs.all'), count: summary?.total },
    { id: 'running' as const, label: t('tabs.running'), count: summary?.running },
    {
      id: 'failed' as const,
      label: t('tabs.failed'),
      count: summary ? (summary.failed ?? []).length : undefined,
    },
    { id: 'stopped' as const, label: t('tabs.stopped'), count: summary?.stopped },
    { id: 'timers' as const, label: t('tabs.timers'), count: summary?.timers },
    { id: 'sockets' as const, label: t('tabs.sockets'), count: summary?.sockets },
  ];
  return win ? allTabs.filter((x) => x.id !== 'timers' && x.id !== 'sockets') : allTabs;
}

export default function ServicesPage() {
  const t = useT('services');
  const mobile = useIsMobile();
  const wide = useMediaQuery('(min-width: 1241px)');
  const { prefs, set: setPref } = usePrefs();
  const win = useSession().session?.os === 'windows';
  const view: View = prefs['services.view'] === 'cards' ? 'cards' : 'table';
  const { selected, panelTab, tab, setTab, openUnit, closePanel, setPanelTab } =
    useServicesRoute(win);
  const [purpose, setPurpose] = useState<Purpose>('all');
  const [q, setQ] = useState('');
  const kind = kindOf(tab);

  const { lists, summary, error, cpu, panelRev, refreshAfter, retry } = useUnitData(kind);
  const failed = summary?.failed ?? [];
  useRailBadge('services', failed.length);
  const { run, busy, dialog } = useActions(refreshAfter);

  const all = lists[kind];
  const rows = useMemo(
    () => (all ? filterUnits(all, kind, tab, purpose, q) : []),
    [all, kind, tab, purpose, q],
  );
  const compact = mobile || (!!selected && wide);
  const { columns, renderCard } = useServiceCells({
    kind,
    compact,
    selected,
    cpu,
    win,
    t,
    run,
    busy,
    openUnit,
  });

  return (
    <Page flush hue="svc">
      <div className={`svc-root ${selected && wide ? 'svc-has-panel' : ''}`}>
        <div className="svc-main">
          <ServicesToolbar
            t={t}
            tabs={tabItems(t, summary, win)}
            tab={tab}
            onTab={setTab}
            showPurpose={kind === 'service' && !win}
            purpose={purpose}
            onPurpose={setPurpose}
            q={q}
            onQ={setQ}
            view={view}
            onView={(v) => void setPref('services.view', v)}
          />
          {failed.length > 0 && kind === 'service' && (
            <FailedBanner
              failed={failed}
              t={t}
              win={win}
              onLog={(n) => openUnit(n, 'logs')}
              onRestart={(n) => run(n, 'restart')}
              onDisable={(n) => run(n, 'disable')}
              onShow={() => setTab('failed')}
            />
          )}
          <div className="svc-body">
            <ServicesBody
              t={t}
              win={win}
              error={error}
              all={all}
              rows={rows}
              view={view}
              columns={columns}
              selected={selected}
              renderCard={renderCard}
              onOpen={(n) => openUnit(n)}
              onRetry={retry}
            />
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

type ToolbarProps = {
  t: T;
  tabs: { id: Tab; label: string; count?: number }[];
  tab: Tab;
  onTab: (tab: Tab) => void;
  showPurpose: boolean;
  purpose: Purpose;
  onPurpose: (p: Purpose) => void;
  q: string;
  onQ: (q: string) => void;
  view: View;
  onView: (v: View) => void;
};

function ServicesToolbar({
  t,
  tabs,
  tab,
  onTab,
  showPurpose,
  purpose,
  onPurpose,
  q,
  onQ,
  view,
  onView,
}: ToolbarProps) {
  return (
    <div className="svc-bar">
      <Tabs
        variant="pill"
        hue="svc"
        items={tabs}
        value={tab}
        onChange={(id) => onTab(id)}
        aria-label={t('title')}
      />
      <div className="svc-sp" />
      {showPurpose && (
        <div className="svc-chips" role="group" aria-label={t('purpose.label')}>
          <Chip hue="svc" pressed={purpose === 'all'} onClick={() => onPurpose('all')}>
            {t('purpose.all')}
          </Chip>
          {PURPOSES.map((p) => (
            <Chip key={p} hue={PURPOSE_HUE[p]} pressed={purpose === p} onClick={() => onPurpose(p)}>
              <span className={`svc-dot svc-dot--${p}`} />
              {t(`purpose.${p}`)}
            </Chip>
          ))}
        </div>
      )}
      <div className="svc-filter">
        <Input
          icon="search"
          value={q}
          onChange={(e) => onQ(e.target.value)}
          placeholder={t('filter')}
          aria-label={t('filter')}
        />
      </div>
      <Segmented
        value={view}
        onChange={onView}
        aria-label={t('view.label')}
        options={[
          { value: 'table', icon: 'list', label: t('view.table') },
          { value: 'cards', icon: 'grid', label: t('view.cards') },
        ]}
      />
    </div>
  );
}

const WIN_HINTS = new Set(['start-limit', 'env-setup']);

function FailedBanner({
  failed,
  win,
  t,
  onLog,
  onRestart,
  onDisable,
  onShow,
}: {
  failed: FailedUnit[];
  win: boolean;
  t: ReturnType<typeof useT>;
  onLog(n: string): void;
  onRestart(n: string): void;
  onDisable(n: string): void;
  onShow(): void;
}) {
  const one = failed.length === 1 ? failed[0] : null;
  let text: string;
  if (one) {
    const reason =
      one.result === 'exit-code'
        ? t('banner.exitCode', { name: short(one.name), code: one.exitCode })
        : one.result
          ? t('banner.result', { name: short(one.name), result: one.result })
          : t('banner.failed', { name: short(one.name) });
    const hint = one.hintId
      ? t(win && WIN_HINTS.has(one.hintId) ? `win.hints.${one.hintId}` : `hints.${one.hintId}`) ||
        one.hint
      : one.hint;
    text = hint ? `${reason} ${hint}` : reason;
  } else {
    const names = failed
      .slice(0, 3)
      .map((f) => short(f.name))
      .join(', ');
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
            {!win && (
              <Button size="sm" onClick={() => onLog(one.name)}>
                {t('banner.log')}
              </Button>
            )}
            <Button size="sm" onClick={() => onRestart(one.name)}>
              {t('actions.restart')}
            </Button>
            {!win && (
              <Button size="sm" onClick={() => onDisable(one.name)}>
                {t('actions.disable')}
              </Button>
            )}
          </>
        ) : (
          <Button size="sm" onClick={onShow}>
            {t('banner.show')}
          </Button>
        )}
      </div>
    </div>
  );
}
