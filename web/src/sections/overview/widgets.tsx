import { formatTime } from '../../lib/format';
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { ApiError, call, useSession } from '../../api';
import { useI18n, useT, type TFn } from '../../i18n';
import { Badge, Button, Card, EmptyState, Icon, Skeleton, StatCard, hueClass, type HueId, type IconName } from '../../ui';
import { PluginFrame, usePlugins } from '../../plugins';
import { useRunning, type ActionResult } from './actions';
import { usePolling, useRefreshInterval } from '../../lib/refresh';
import { physicalNet, useAlerts, useHost, useMetrics, type AlertItem } from './data';
import { bytes, bytesStr, duration, num, rate } from './fmt';
import { asMetric, asRange, isAction, METRIC_META, RANGES, type DashAction, type Widget } from './model';
import { TimeChart } from './TimeChart';

export type { Widget };

export interface WidgetCtx {
  /** Dashboard is in Personalize mode. */
  editing: boolean;
  /** Ask the page to confirm and run an action. */
  run(a: DashAction): void;
  /** Open the settings dialog of this widget. */
  configure(id: string): void;
}

export const widgetTitle = (w: Widget, t: TFn) => (typeof w.settings?.title === 'string' && w.settings.title.trim() ? w.settings.title.trim() : t(`widgets.${w.type}.title`));

export function useGo() {
  const nav = useNavigate();
  return useCallback(
    (section: string, params?: Record<string, string>) => {
      const qs = params && Object.keys(params).length ? `?${new URLSearchParams(params).toString()}` : '';
      nav(`${section === 'overview' ? '/' : `/${section}`}${qs}`);
    },
    [nav],
  );
}

/* ---------- helpers ---------- */
function SetUp({ ctx, w, text }: { ctx: WidgetCtx; w: Widget; text: string }) {
  const t = useT('overview');
  return (
    <div className="ov-setup">
      <small>{text}</small>
      <Button size="sm" icon="cog" onClick={() => ctx.configure(w.id)}>{t('setUp')}</Button>
    </div>
  );
}

function useTick(ms: number, on = true) {
  const [n, set] = useState(0);
  useEffect(() => {
    if (!on) return;
    const id = window.setInterval(() => set((x) => x + 1), ms);
    return () => window.clearInterval(id);
  }, [ms, on]);
  return n;
}

/** Calls a method now and every `every` ms while visible; never opens the unlock dialog by itself. */
function usePoll<T>(method: string, params: unknown, every: number, admin = false, enabled = true) {
  const [data, setData] = useState<T | null>(null);
  const [err, setErr] = useState<ApiError | null>(null);
  const key = JSON.stringify(params) + admin;
  const paramsRef = useRef(params);
  paramsRef.current = params;
  const load = useCallback(
    async (interactive = false) => {
      try {
        const r = await call<T>(method, paramsRef.current, { admin, noUnlock: !interactive });
        setData(r);
        setErr(null);
      } catch (e) {
        setErr(e instanceof ApiError ? e : new ApiError('internal', String(e)));
      }
    },
    [method, admin],
  );
  useEffect(() => {
    if (!enabled) return;
    void load();
  }, [load, key, enabled]);
  usePolling(() => void load(), every, enabled);
  return { data, err, reload: () => load(true) };
}

function ErrorLine({ err, onUnlock }: { err: ApiError; onUnlock?: () => void }) {
  const t = useT('overview');
  return (
    <div className="ov-err" role="alert">
      <Icon name="alert" />
      <span>{err.code === 'needs_admin' ? t('needsAdmin') : err.message}</span>
      {err.code === 'needs_admin' && onUnlock && <Button size="sm" icon="unlock" onClick={onUnlock}>{t('unlock')}</Button>}
    </div>
  );
}

/* ---------- stat cards ---------- */
function Stat({ w }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const { lang } = useI18n();
  const { metrics: m } = useMetrics();
  const host = useHost();
  const metric = asMetric(w.settings?.metric);
  const meta = METRIC_META[metric];
  const label = w.settings?.title?.trim() || t(`metrics.${metric}`);
  const common = { hue: meta.hue, icon: meta.icon as IconName, label };
  if (!m) return <StatCard {...common} value="–" sub={t('loading')} />;
  if (metric === 'cpu') {
    return <StatCard {...common} value={num(m.cpu.percent, lang, 0)} unit="%" sub={host ? t('stat.threads', { count: host.cpu.threads }) : m.load?.length ? t('stat.load', { v: num(m.load[0], lang, 2) }) : ''} percent={m.cpu.percent} />;
  }
  if (metric === 'memory') {
    const used = bytes(m.memory.used, lang);
    return <StatCard {...common} value={used.value} unit={used.unit} percent={m.memory.percent} sub={t('stat.ofTotal', { total: bytesStr(m.memory.total, lang) })} />;
  }
  if (metric === 'disk') {
    const want = w.settings?.mount as string | undefined;
    const d = (want && m.disks.find((x) => x.mount === want)) || m.disks.find((x) => x.mount === '/') || m.disks[0];
    if (!d) return <StatCard {...common} value="–" sub={t('stat.noDisk')} />;
    const free = bytes(d.free, lang);
    return <StatCard {...common} value={free.value} unit={t('stat.free', { unit: free.unit })} percent={d.percent} sub={t('stat.usedOf', { used: bytesStr(d.used, lang), total: bytesStr(d.total, lang), mount: d.mount })} />;
  }
  const want = w.settings?.iface as string | undefined;
  const list = physicalNet(m);
  const ifs = want && want !== 'auto' ? m.net.filter((n) => n.iface === want) : list;
  const busiest = [...ifs].sort((a, b) => b.rxRate + b.txRate - (a.rxRate + a.txRate))[0] ?? ifs[0];
  const rx = ifs.reduce((a, n) => a + n.rxRate, 0);
  const tx = ifs.reduce((a, n) => a + n.txRate, 0);
  const total = rate(rx + tx, lang);
  return <StatCard {...common} value={total.value} unit={total.unit} sub={busiest ? t('stat.netSub', { iface: busiest.iface, rx: rate(rx, lang).value + ' ' + rate(rx, lang).unit, tx: rate(tx, lang).value + ' ' + rate(tx, lang).unit }) : t('stat.noNet')} />;
}

/* ---------- charts ---------- */
const NET_SCALE_MIN = 100 * 1024;

function Activity({ w }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const { history } = useMetrics();
  const range = asRange(w.settings?.range);
  const peak = Math.max(NET_SCALE_MIN, ...history.map((p) => p.net));
  const series = useMemo(
    () => [
      { color: 'var(--h-ov)', fill: true, get: (p: { cpu: number }) => p.cpu },
      { color: 'var(--h-file)', get: (p: { net: number }) => (p.net / peak) * 100 },
    ],
    [peak],
  );
  return (
    <Card title={widgetTitle(w, t)} action={t(`range.${range}`)}>
      <div className="ov-leg">
        <span><i style={{ background: 'var(--h-ov)' }} />{t('metrics.cpu')}</span>
        <span><i style={{ background: 'var(--h-file)' }} />{t('metrics.network')}</span>
      </div>
      <TimeChart points={history} windowMs={RANGES[range]} series={series} label={t('activity.label')} />
      {history.length < 3 && <small className="ov-muted">{t('activity.collecting')}</small>}
    </Card>
  );
}

function ChartWidget({ w }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const { lang } = useI18n();
  const { history, metrics: m } = useMetrics();
  const metric = asMetric(w.settings?.metric);
  const range = asRange(w.settings?.range, '15m');
  const meta = METRIC_META[metric];
  const peak = Math.max(NET_SCALE_MIN, ...history.map((p) => p.net));
  const mount = (w.settings?.mount as string | undefined) || '/';
  const get = useMemo(() => {
    if (metric === 'cpu') return (p: { cpu: number }) => p.cpu;
    if (metric === 'memory') return (p: { mem: number }) => p.mem;
    if (metric === 'network') return (p: { net: number }) => (p.net / peak) * 100;
    return (p: { disks: Record<string, number> }) => p.disks[mount] ?? Object.values(p.disks)[0] ?? 0;
  }, [metric, peak, mount]);
  const series = useMemo(() => [{ color: 'var(--h)', fill: true, get }], [get]);
  let value = '–';
  if (m) {
    if (metric === 'cpu') value = `${num(m.cpu.percent, lang, 0)}%`;
    else if (metric === 'memory') value = `${num(m.memory.percent, lang, 0)}%`;
    else if (metric === 'disk') value = `${num((m.disks.find((d) => d.mount === mount) ?? m.disks[0])?.percent ?? 0, lang, 0)}%`;
    else {
      const r = rate(physicalNet(m).reduce((a, n) => a + n.rxRate + n.txRate, 0), lang);
      value = `${r.value} ${r.unit}`;
    }
  }
  return (
    <Card title={widgetTitle({ ...w, type: 'chart', settings: { ...w.settings, title: w.settings?.title || t(`metrics.${metric}`) } }, t)} action={t(`range.${range}`)} hue={meta.hue}>
      <div className="ov-chart-val">{value}</div>
      <TimeChart points={history} windowMs={RANGES[range]} series={series} label={t(`metrics.${metric}`)} />
    </Card>
  );
}

/* ---------- actions ---------- */
function ActionTile({ a, ctx, big }: { a: DashAction; ctx: WidgetCtx; big?: boolean }) {
  const running = useRunning().has(a.id);
  return (
    <button type="button" className={`ov-tile ${hueClass(a.hue)}${big ? ' ov-tile--big' : ''}`} disabled={running} aria-busy={running || undefined} onClick={() => ctx.run(a)}>
      {running ? <span className="ui-spin" aria-hidden="true" /> : <Icon name={a.icon} />}
      <span>{a.label}</span>
    </button>
  );
}

function Actions({ w, ctx }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const list: DashAction[] = Array.isArray(w.settings?.actions) ? (w.settings!.actions as unknown[]).filter(isAction) : [];
  return (
    <Card title={widgetTitle(w, t)}>
      {list.length ? (
        <div className="ov-tiles">{list.map((a) => <ActionTile key={a.id} a={a} ctx={ctx} />)}</div>
      ) : (
        <SetUp ctx={ctx} w={w} text={t('actions.empty')} />
      )}
    </Card>
  );
}

function ActionOne({ w, ctx }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  if (!isAction(w.settings)) return <div className="ov-card-fill"><SetUp ctx={ctx} w={w} text={t('action.empty')} /></div>;
  return <ActionTile a={w.settings as DashAction} ctx={ctx} big />;
}

/* ---------- alerts ---------- */
const ALERT_KIND: Record<string, { icon: IconName; hue: HueId }> = {
  services: { icon: 'alert', hue: 'svc' },
  software: { icon: 'software', hue: 'sw' },
  logs: { icon: 'shield', hue: 'term' },
  files: { icon: 'disk', hue: 'file' },
  terminal: { icon: 'mem', hue: 'log' },
};

function Alerts({ w }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const go = useGo();
  const { alerts, error, refresh } = useAlerts();
  const showOk = w.settings?.showOk !== false;
  const max = Math.max(3, Math.min(12, Number(w.settings?.max) || 6));
  const list = (alerts ?? []).filter((a) => showOk || a.severity !== 'ok').slice(0, max);
  const label = (a: AlertItem) => (a.titleKey && t(`alerts.${a.titleKey}`, (a.vars ?? {}) as Record<string, string | number>)) || a.title;
  return (
    <Card title={widgetTitle(w, t)} action={<Button size="sm" variant="ghost" iconOnly icon="refresh" aria-label={t('refresh')} onClick={() => void refresh()} />}>
      {alerts === null && !error ? (
        <Skeleton lines={3} height={36} />
      ) : error && alerts === null ? (
        <div className="ov-err" role="alert"><Icon name="alert" />{error}</div>
      ) : list.length === 0 ? (
        <EmptyState icon="check" hue="term" title={t('alerts.none')} />
      ) : (
        <div className="ov-rows">
          {list.map((a) => {
            const kind = ALERT_KIND[a.action?.section ?? ''] ?? { icon: a.severity === 'ok' ? 'check' : 'alert', hue: 'ov' as HueId };
            const tone = a.severity === 'err' ? 'err' : a.severity === 'ok' ? 'ok' : '';
            return (
              <div key={a.id} className={`ov-row ${hueClass(kind.hue)}${tone ? ` ov-row--${tone}` : ''}`}>
                <span className="ov-ic"><Icon name={a.severity === 'ok' ? 'shield' : kind.icon} /></span>
                <div className="ov-row-tx">
                  <b>{label(a)}</b>
                  {a.detail && <small>{a.detail}</small>}
                </div>
                {a.action && a.severity !== 'ok' && (
                  <button type="button" className="ov-row-btn" onClick={() => go(a.action!.section, a.action!.params)}>
                    {t(`alerts.open.${a.action.section in ALERT_KIND ? a.action.section : 'default'}`)}
                  </button>
                )}
              </div>
            );
          })}
        </div>
      )}
    </Card>
  );
}

/* ---------- this machine ---------- */
function Machine({ w }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const { lang } = useI18n();
  const h = useHost();
  useTick(60_000);
  const rows: { icon: IconName; title: string; sub: string }[] = [];
  if (h) {
    const up = Math.max(0, Date.now() / 1000 - h.bootTime);
    const started = new Date(h.bootTime * 1000);
    const sameDay = started.toDateString() === new Date().toDateString();
    const clock = formatTime(started, { lang });
    rows.push(
      { icon: 'server', title: h.distro.prettyName || h.distro.name, sub: t('machine.kernel', { kernel: h.kernel, arch: h.arch }) },
      { icon: 'cpu', title: h.cpu.model.replace(/\((R|TM)\)/g, '').replace(/\s+/g, ' ').trim() || t('machine.cpu'), sub: t('machine.cores', { cores: h.cpu.cores, threads: h.cpu.threads, mem: bytesStr(h.memoryTotal, lang) }) },
      { icon: 'power', title: t('machine.uptime', { dur: duration(up, t) }), sub: sameDay ? t('machine.bootToday', { time: clock }) : t('machine.bootOn', { date: started.toLocaleDateString(lang, { day: 'numeric', month: 'short' }), time: clock }) },
    );
    const net = [h.hostname, h.ip].filter(Boolean).join(' · ');
    if (net) rows.push({ icon: 'globe', title: net, sub: [h.machine?.vendor, h.machine?.product].filter(Boolean).join(' ') || t('machine.network') });
  }
  return (
    <Card title={widgetTitle(w, t)}>
      {!h ? (
        <Skeleton lines={3} height={36} />
      ) : (
        <div className="ov-rows">
          {rows.map((r, i) => (
            <div key={i} className="ov-row hue-ov">
              <span className="ov-ic"><Icon name={r.icon} /></span>
              <div className="ov-row-tx"><b>{r.title}</b><small>{r.sub}</small></div>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

/* ---------- service status ---------- */
interface UnitStatus { unit: string; description: string; active: string; sub: string }
function Service({ w, ctx }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const go = useGo();
  const refresh = useRefreshInterval();
  const units: string[] = Array.isArray(w.settings?.units) ? w.settings!.units : [];
  const { data, err, reload } = usePoll<UnitStatus[]>('overview.unitStatus', { units }, Math.max(refresh, 5000), false, units.length > 0);
  return (
    <Card title={widgetTitle(w, t)}>
      {units.length === 0 ? (
        <SetUp ctx={ctx} w={w} text={t('service.empty')} />
      ) : err && !data ? (
        <ErrorLine err={err} onUnlock={() => void reload()} />
      ) : !data ? (
        <Skeleton lines={units.length > 3 ? 3 : units.length} height={34} />
      ) : (
        <div className="ov-rows">
          {data.map((u) => {
            const tone = u.active === 'active' ? 'ok' : u.active === 'failed' ? 'err' : u.active === 'not-found' ? 'warn' : 'neutral';
            return (
              <button key={u.unit} type="button" className="ov-row ov-row--btn" onClick={() => go('services', { unit: u.unit })}>
                <span className={`ov-dot ov-dot--${tone}`} aria-hidden="true" />
                <div className="ov-row-tx"><b>{u.unit.replace(/\.service$/, '')}</b><small>{u.description || t('service.notFound')}</small></div>
                <Badge tone={tone as 'ok' | 'err' | 'warn' | 'neutral'}>{t(`service.state.${['active', 'inactive', 'failed', 'activating', 'deactivating', 'not-found'].includes(u.active) ? u.active : 'inactive'}`)}</Badge>
              </button>
            );
          })}
        </div>
      )}
    </Card>
  );
}

/* ---------- live log ---------- */
function Log({ w, ctx }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const refresh = useRefreshInterval();
  const s = w.settings ?? {};
  const lines = Math.max(4, Math.min(100, Number(s.lines) || 12));
  const params = s.source === 'file' ? { file: s.file ?? '', lines } : { unit: s.source === 'unit' ? s.unit ?? '' : '', lines };
  const ready = s.source === 'file' ? !!s.file : s.source === 'unit' ? !!s.unit : true;
  const { data, err, reload } = usePoll<{ lines: string[] }>('overview.logTail', params, Math.max(refresh, 2000), false, ready);
  const box = useRef<HTMLPreElement>(null);
  useEffect(() => {
    if (box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [data]);
  const title = s.title?.trim() || (s.source === 'file' ? String(s.file).split('/').pop() : s.source === 'unit' ? s.unit : t('log.system'));
  return (
    <Card title={title || t('widgets.log.title')} action={<span className="ov-live"><i />{t('log.live')}</span>}>
      {!ready ? (
        <SetUp ctx={ctx} w={w} text={t('log.empty')} />
      ) : err && !data ? (
        <ErrorLine err={err} onUnlock={() => void reload()} />
      ) : (
        <pre ref={box} className="ov-pre" tabIndex={0} style={{ maxHeight: `${lines * 1.5 + 1}em` }}>{data?.lines.length ? data.lines.join('\n') : data ? t('log.nothing') : t('loading')}</pre>
      )}
    </Card>
  );
}

/* ---------- command output ---------- */
function Output({ w, ctx }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const s = w.settings ?? {};
  const argv: string[] = Array.isArray(s.argv) ? s.argv : [];
  const every = Math.max(2, Number(s.every) || 10);
  const admin = !!s.admin;
  const [res, setRes] = useState<ActionResult | null>(null);
  const [err, setErr] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const key = JSON.stringify(argv) + admin;
  const run = useCallback(
    async (interactive: boolean) => {
      if (busyRef.current || !argv.length) return;
      busyRef.current = true;
      setBusy(true);
      try {
        setRes(await call<ActionResult>('overview.actionRun', { argv, timeout: Math.max(5, Math.min(60, every)) }, { admin, noUnlock: !interactive }));
        setErr(null);
      } catch (e) {
        setErr(e instanceof ApiError ? e : new ApiError('internal', String(e)));
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [key, every], // eslint-disable-line react-hooks/exhaustive-deps
  );
  useEffect(() => {
    if (!argv.length) return;
    void run(false);
    const id = window.setInterval(() => {
      if (!document.hidden) void run(false);
    }, every * 1000);
    return () => window.clearInterval(id);
  }, [run, every, key]); // eslint-disable-line react-hooks/exhaustive-deps
  const cmd = argv.join(' ');
  return (
    <Card title={s.title?.trim() || cmd || t('widgets.output.title')} action={<span className="ov-live">{t('output.every', { s: every })}</span>}>
      {!argv.length ? (
        <SetUp ctx={ctx} w={w} text={t('output.empty')} />
      ) : err && !res ? (
        <ErrorLine err={err} onUnlock={() => void run(true)} />
      ) : (
        <>
          <pre className="ov-pre" tabIndex={0} aria-busy={busy}>{res ? res.output || t('output.none') : t('loading')}</pre>
          {res && !res.ok && <small className="ov-pre-note">{res.timedOut ? t('action.timedOutShort') : t('action.exit', { code: res.exitCode })}</small>}
          {err && res && <ErrorLine err={err} onUnlock={() => void run(true)} />}
        </>
      )}
    </Card>
  );
}

/* ---------- favourite folder ---------- */
function Folder({ w, ctx }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const go = useGo();
  const { session } = useSession();
  const path = String(w.settings?.path ?? '');
  if (!path) return <div className="ov-card-fill"><SetUp ctx={ctx} w={w} text={t('folder.empty')} /></div>;
  const shown = session?.home && path.startsWith(session.home) ? `~${path.slice(session.home.length)}` : path;
  const name = w.settings?.label?.trim() || path.replace(/\/+$/, '').split('/').pop() || '/';
  return (
    <button type="button" className="ov-tile ov-tile--big hue-file" onClick={() => go('files', { path })}>
      <Icon name="files" />
      <span>{name}<small className="ov-mono">{shown}</small></span>
    </button>
  );
}

/* ---------- plugin widgets ---------- */
/** Plugin widgets run in a small sandboxed frame (web/src/plugins/PluginFrame), never in the app itself. */
function PluginWidget({ w }: { w: Widget; ctx: WidgetCtx }) {
  const t = useT('overview');
  const { widgets, plugins, loading } = usePlugins();
  const pid = String(w.settings?.plugin ?? '');
  const wid = String(w.settings?.widget ?? '');
  const def = widgets.find((x) => x.plugin === pid && x.id === wid);
  const manifest = plugins.find((p) => p.id === pid);
  if (!def || !manifest) {
    return (
      <Card title={widgetTitle(w, t)} hue="plg">
        {loading ? <Skeleton lines={2} /> : <EmptyState icon="plugins" hue="plg" title={t('plugin.missing')} text={t('plugin.missingText', { plugin: pid || '?' })} />}
      </Card>
    );
  }
  return (
    <Card title={w.settings?.title?.trim() || def.title} hue="plg">
      <PluginFrame plugin={manifest} view={{ kind: 'widget', id: def.id }} title={`${manifest.name}: ${def.title}`} />
    </Card>
  );
}

/* ---------- registry ---------- */
export function WidgetBody({ w, ctx }: { w: Widget; ctx: WidgetCtx }): ReactNode {
  switch (w.type) {
    case 'stat': return <Stat w={w} ctx={ctx} />;
    case 'activity': return <Activity w={w} ctx={ctx} />;
    case 'chart': return <ChartWidget w={w} ctx={ctx} />;
    case 'actions': return <Actions w={w} ctx={ctx} />;
    case 'action': return <ActionOne w={w} ctx={ctx} />;
    case 'alerts': return <Alerts w={w} ctx={ctx} />;
    case 'machine': return <Machine w={w} ctx={ctx} />;
    case 'service': return <Service w={w} ctx={ctx} />;
    case 'log': return <Log w={w} ctx={ctx} />;
    case 'output': return <Output w={w} ctx={ctx} />;
    case 'folder': return <Folder w={w} ctx={ctx} />;
    case 'plugin': return <PluginWidget w={w} ctx={ctx} />;
    default: return <UnknownWidget w={w} />;
  }
}

function UnknownWidget({ w }: { w: Widget }) {
  const t = useT('overview');
  return (
    <Card title={t('unknown.title')}>
      <small className="ov-muted">{t('unknown.text', { type: String(w.type) })}</small>
    </Card>
  );
}
