import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { applyAutoUpdate, findWatchtower, parseSessions, readLogs, runOnce, validCron, withTag, type ApplyStep } from '../api/autoupdate';
import { errorText } from '../api/engine';
import { containers } from '../api/resources';
import { t } from '../i18n';
import { Badge, Button, Icon, Skeleton, Switch, toast } from '../kit';
import type { RouteProps } from '../router';
import { setFile, useFile, type AutoUpdateConfig } from '../settings';
import { PageHeader } from '../ui/PageHeader';
import { AvailableCard, HistoryCard, RunCard, type RunState } from './autoupdate/Activity';
import { OptionsCard, ScheduleCard, ScopeCard, scheduleSummary } from './autoupdate/Settings';

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/** Route { view: 'autoupdate' }: Watchtower settings, status, and the checks it has done. */
export function AutoUpdatePage(_props: RouteProps<'autoupdate'>) {
  const cs = containers.use();
  const [file, , fileLoaded] = useFile('autoupdate');
  const [settings] = useFile('settings');
  const wt = findWatchtower(cs.data);
  const [draft, setDraft] = useState<AutoUpdateConfig | null>(null);
  const [applying, setApplying] = useState<ApplyStep | null>(null);
  const [run, setRun] = useState<RunState | null>(null);
  const [confirmUpdate, setConfirmUpdate] = useState(false);
  const [logs, setLogs] = useState<string[]>([]);
  const running = run?.phase === 'pull' || run?.phase === 'run';

  // Start from what is applied (the container's label), else from the saved file.
  useEffect(() => {
    if (draft || !fileLoaded || !cs.data) return;
    const base = wt?.config ?? file.config;
    setDraft({ ...base, enabled: wt ? true : base.enabled, image: base.image || withTag(settings.watchtowerImage) });
  }, [draft, fileLoaded, cs.data, wt, file.config, settings.watchtowerImage]);

  const baseline = useMemo<AutoUpdateConfig | null>(() => {
    if (!draft) return null;
    if (wt?.config) return wt.config;
    return { ...file.config, enabled: false, image: file.config.image || withTag(settings.watchtowerImage) };
  }, [draft, wt?.config, file.config, settings.watchtowerImage]);
  const dirty = !!draft && !!baseline && !same(draft, baseline);
  const wtId = wt?.container.Id;
  const wtName = wt?.name;
  const wtState = wt?.container.State;

  const loadLogs = useCallback(async () => {
    if (!wtId || !wtName) {
      setLogs([]);
      return;
    }
    try {
      setLogs(await readLogs(wtName));
    } catch {
      /* the container may be starting */
    }
  }, [wtId, wtName]);
  useEffect(() => {
    void loadLogs();
    if (!wtId) return;
    const timer = setInterval(() => { if (!document.hidden) void loadLogs(); }, 30000);
    return () => clearInterval(timer);
  }, [loadLogs, wtId, wtState]);

  const parsed = useMemo(() => parseSessions(logs), [logs]);
  const manual = useMemo(() => (run?.phase === 'done' ? parseSessions(run.lines).sessions[0] : undefined), [run]);
  const latest = useMemo(() => {
    // Whichever is newer: the scheduled log or the check the user just ran.
    const sched = parsed.sessions[0];
    return manual && (!sched || manual.at >= sched.at) ? manual : sched;
  }, [parsed, manual]);

  const set = (p: Partial<AutoUpdateConfig>) => setDraft((d) => (d ? { ...d, ...p } : d));
  const bad = !!draft && ((draft.mode === 'choose' && draft.containers.length === 0) || (draft.schedule.type === 'custom' && !validCron(draft.schedule.cron)) || !draft.image.trim());

  const apply = async () => {
    if (!draft || bad) return;
    const cfg = { ...draft, image: withTag(draft.image) };
    try {
      await applyAutoUpdate(cfg, setApplying);
      await setFile('autoupdate', { config: cfg });
      setDraft(cfg);
      toast.ok(t(cfg.enabled ? 'autoupdate.applied' : 'autoupdate.applied.off'));
    } catch (e) {
      toast.err(t('autoupdate.applyFailed'), errorText(e));
    } finally {
      setApplying(null);
      await containers.refresh();
    }
  };

  const abort = useRef(false);
  const start = async (mode: 'look' | 'update') => {
    if (!draft) return;
    abort.current = false;
    const state: RunState = { mode, phase: 'pull', lines: [] };
    setRun(state);
    try {
      const code = await runOnce({ ...draft, image: withTag(draft.image), enabled: true }, {
        lookOnly: mode === 'look',
        onPhase: (p) => setRun((r) => (r ? { ...r, phase: p } : r)),
        onLine: (l) => setRun((r) => (r ? { ...r, lines: [...r.lines, l] } : r)),
      });
      setRun((r) => (r ? { ...r, phase: code === 0 ? 'done' : 'error', error: code === 0 ? undefined : `exit ${code}` } : r));
    } catch (e) {
      setRun((r) => (r ? { ...r, phase: 'error', error: errorText(e) } : r));
    } finally {
      await containers.refresh();
    }
  };

  const statusKey = !wt ? 'missing' : wtState === 'running' ? 'running' : wtState === 'restarting' ? 'restarting' : 'stopped';
  const tone = statusKey === 'running' ? 'ok' : statusKey === 'missing' ? 'neutral' : 'warn';

  if (!draft) {
    return (
      <div className="dk-au">
        <PageHeader icon="refresh" hue="sw" title={t('autoupdate.title')} subtitle={t('autoupdate.sub')} />
        <Skeleton lines={5} />
      </div>
    );
  }

  return (
    <div className="dk-au">
      <PageHeader
        icon="refresh"
        hue="sw"
        title={t('autoupdate.title')}
        subtitle={t('autoupdate.sub')}
        actions={
          <>
            <Button icon="search" loading={running && run?.mode === 'look'} disabled={running || !!applying} onClick={() => void start('look')}>{t('autoupdate.checkNow')}</Button>
            {!draft.monitorOnly && <Button icon="download" disabled={running || !!applying} onClick={() => setConfirmUpdate(true)}>{t('autoupdate.updateNow')}</Button>}
          </>
        }
      />

      {confirmUpdate && (
        <div className="dk-au-note dk-au-note--warn" role="alert">
          <span className="dk-au-note-ic"><Icon name="alert" /></span>
          <div>
            <b>{t('autoupdate.updateNow.title')}</b>
            <p>{t('autoupdate.updateNow.text')}</p>
            <div className="dk-au-chips">
              <Button variant="primary" size="sm" onClick={() => { setConfirmUpdate(false); void start('update'); }}>{t('autoupdate.updateNow.confirm')}</Button>
              <Button variant="ghost" size="sm" onClick={() => setConfirmUpdate(false)}>{t('common.cancel')}</Button>
            </div>
          </div>
        </div>
      )}

      <div className="dk-au-note">
        <span className="dk-au-note-ic"><Icon name="info" /></span>
        <div>
          <b>{t('autoupdate.explain.title')}</b>
          <p>{t('autoupdate.explain.1')}</p>
          <p>{t('autoupdate.explain.2')}</p>
          <p>{t('autoupdate.explain.3')}</p>
        </div>
      </div>

      <section className={`dk-au-master ${draft.enabled ? 'dk-au-master--on' : ''}`}>
        <div className="dk-au-master-t">
          <b>{t('autoupdate.master')}</b>
          <span>{t(draft.enabled ? 'autoupdate.master.on' : 'autoupdate.master.off')}</span>
        </div>
        <Switch checked={draft.enabled} aria-label={t('autoupdate.master')} onChange={(v) => set({ enabled: v })} />
        <dl className="dk-au-facts">
          <div><dt>{t('autoupdate.status.container')}</dt><dd><Badge tone={tone} dot>{t(`autoupdate.status.${statusKey}`)}</Badge></dd></div>
          <div><dt>{t('autoupdate.status.image')}</dt><dd className="dk-mono">{wt?.container.Image ?? withTag(draft.image)}</dd></div>
          <div><dt>{t('autoupdate.status.next')}</dt><dd>{draft.enabled && wt ? scheduleSummary(draft) : '–'}</dd></div>
          <div><dt>{t('autoupdate.status.last')}</dt><dd>{parsed.sessions[0]?.at ? new Date(parsed.sessions[0].at).toLocaleString() : t('autoupdate.status.lastNone')}</dd></div>
        </dl>
      </section>

      {run && <RunCard run={run} session={manual} onClose={() => setRun(null)} />}

      <div className="dk-au-grid">
        <ScheduleCard c={draft} set={set} />
        <ScopeCard c={draft} set={set} all={cs.data ?? []} />
        <OptionsCard c={draft} set={set} />
        <AvailableCard session={latest} />
      </div>
      <HistoryCard sessions={parsed.sessions} raw={logs} />

      {(dirty || applying) && (
        <div className="dk-au-bar" role="region" aria-label={t('autoupdate.apply.title')}>
          <Icon name="alert" />
          <div>
            <b>{applying ? t(`autoupdate.applying.${applying}`) : t('autoupdate.apply.title')}</b>
            {!applying && <span>{t(draft.enabled ? 'autoupdate.apply.text' : wt ? 'autoupdate.apply.textOff' : 'autoupdate.apply.textSave')}</span>}
          </div>
          <button type="button" className="dk-au-bar-b" disabled={!!applying} onClick={() => baseline && setDraft({ ...baseline, enabled: !!wt })}>{t('autoupdate.discard')}</button>
          <button type="button" className="dk-au-bar-b dk-au-bar-go" disabled={!!applying || bad} onClick={() => void apply()}>{t('autoupdate.apply')}</button>
        </div>
      )}

    </div>
  );
}
