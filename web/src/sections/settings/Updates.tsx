import { useCallback, useEffect, useState } from 'react';
import { Name } from '../../brand';
import { useI18n, useT } from '../../i18n';
import { formatBytes, formatDate, formatDateTime, relativeTime } from '../../lib/format';
import { Badge, Button, ConfirmDialog, Icon, Progress, type IconName } from '../../ui';
import { ReleaseNotes } from './ReleaseNotes';
import {
  checkUpdates, clearRun, startRollback, startUpdate, updateStatus, useRun,
  type CheckResult, type RunPhase, type RunState, type UpdateStatus,
} from './updates';

/**
 * Settings › About: current version, check for updates, release notes, Update now (confirm → progress →
 * wait for the restarted daemon → reload), rollback and the last result. The automatic-update settings are
 * ordinary config rows in index.tsx.
 */
export function UpdatesBlock({ isAdmin }: { isAdmin: boolean }) {
  const t = useT('settings');
  const { lang } = useI18n();
  const run = useRun();
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [check, setCheck] = useState<CheckResult | null>(null);
  const [checking, setChecking] = useState(false);
  const [checkErr, setCheckErr] = useState('');
  const [confirm, setConfirm] = useState<null | 'update' | 'rollback'>(null);

  const loadStatus = useCallback(async (): Promise<UpdateStatus | null> => {
    try {
      const s = await updateStatus();
      setStatus(s);
      return s;
    } catch {
      setStatus(null);
      return null;
    }
  }, []);
  const doCheck = useCallback(async (force: boolean) => {
    setChecking(true);
    setCheckErr('');
    try {
      setCheck(await checkUpdates(force));
    } catch (e) {
      setCheckErr(e instanceof Error ? e.message : String(e));
    } finally {
      setChecking(false);
    }
  }, []);

  // On open: status, then a check when automatic checks are on (the server caches GitHub's answer for an hour).
  useEffect(() => {
    void loadStatus().then((s) => {
      if (s?.settings?.autoCheck !== false) void doCheck(false);
    });
  }, [loadStatus, doCheck]);

  const current = status?.current ?? check?.current ?? '';
  const latest = check?.latest ?? null;
  const newer = !!check?.newer && !!latest;
  const managedBy = status?.managedBy || check?.managedBy || '';
  const canApply = isAdmin && !managedBy && !!status?.canUpdate && newer && !!latest && latest.size > 0 && !status?.running;
  const managedNote = managedBy ? (
    <div className="st-upd-note">
      <Icon name="info" />
      <div>
        <b>{managedBy === 'unknown' ? t('updates.managedUnknown') : t('updates.managed', { manager: managedBy })}</b>
        <small className="st-upd-note-sub">{t('updates.managedDesc', { name: Name })}</small>
      </div>
    </div>
  ) : null;

  return (
    <div className="st-upd">
      <div className="st-upd-hd">
        <span className="st-upd-logo"><Icon name="zap" size={22} /></span>
        <div className="grow">
          <b>{Name} {current || '…'}</b>
          <small>
            {t('about.versionDesc')}
            {check?.checkedAt ? ` · ${t('updates.checked', { when: relativeTime(check.checkedAt, lang) })}` : ''}
          </small>
        </div>
        <Button icon="refresh" loading={checking} onClick={() => void doCheck(true)} disabled={!!run && !isFinal(run.phase)}>
          {t('updates.check')}
        </Button>
      </div>

      {run ? (
        <RunView run={run} onClose={() => { clearRun(); void loadStatus(); void doCheck(false); }} />
      ) : (
        <>
          {checkErr && <div className="st-warn"><Icon name="alert" /><div>{t('updates.checkFailed', { error: checkErr })}</div></div>}
          {!checkErr && check && !latest && <div className="st-upd-note"><Icon name="info" />{t('updates.noRelease')}</div>}
          {!checkErr && check && latest && !newer && (
            <div className="st-upd-note st-upd-note--ok"><Icon name="check" />{t('updates.upToDate', { name: Name, version: current })}</div>
          )}
          {!newer && managedNote}
          {newer && latest && (
            <div className="st-upd-card">
              <div className="st-upd-card-hd">
                <div className="grow">
                  <b>{t('updates.available', { version: latest.version })}</b>
                  <small>
                    {t('updates.published', { date: formatDate(latest.publishedAt, { dateStyle: 'medium' }) })}
                    {latest.size > 0 ? ` · ${formatBytes(latest.size)}` : ''}
                  </small>
                </div>
                {latest.prerelease && <Badge tone="warn">{t('updates.prerelease')}</Badge>}
                {isAdmin && !managedBy && (
                  <Button variant="primary" icon="download" disabled={!canApply} onClick={() => setConfirm('update')}>
                    {t('updates.updateNow')}
                  </Button>
                )}
              </div>
              {managedNote}
              <ReleaseNotes source={latest.notes} empty={t('updates.noNotes')} />
              {!managedBy && latest.size === 0 && <div className="st-warn"><Icon name="alert" /><div>{t('updates.noBuild', { arch: check?.arch ?? '' })}</div></div>}
              {isAdmin && !managedBy && status && !status.canUpdate && status.reason && (
                <div className="st-warn"><Icon name="info" /><div>{t('updates.cannot', { reason: status.reason })}</div></div>
              )}
              {isAdmin && status?.packageBusy && <div className="st-warn"><Icon name="alert" /><div>{t('updates.packageBusy')}</div></div>}
              {!isAdmin && !managedBy && <div className="st-upd-note"><Icon name="lock" />{t('updates.adminOnly')}</div>}
            </div>
          )}
        </>
      )}

      {status?.last && !run && <LastLine last={status.last} />}

      {isAdmin && !managedBy && status?.previous && !run && (
        <div className="st-upd-row">
          <div className="grow">
            <b>{t('updates.rollback.title')}</b>
            <small>{t('updates.rollback.desc', { version: status.previous })}</small>
          </div>
          <Button icon="undo" disabled={!status.canUpdate || status.running} onClick={() => setConfirm('rollback')}>
            {t('updates.rollback.action', { version: status.previous })}
          </Button>
        </div>
      )}

      <ConfirmDialog
        open={confirm === 'update' && !!latest}
        onClose={() => setConfirm(null)}
        danger={false}
        icon="download"
        title={t('updates.confirm.title', { version: latest?.version ?? '' })}
        description={t('updates.confirm.text', { name: Name, version: latest?.version ?? '', size: latest ? formatBytes(latest.size) : '' })}
        confirmLabel={t('updates.confirm.action')}
        onConfirm={() => startUpdate(latest!.version, current)}
      >
        <ul className="st-upd-list">
          <li>{t('updates.confirm.signin', { name: Name })}</li>
          <li>{t('updates.confirm.interrupt')}</li>
          <li>{t('updates.confirm.rollback')}</li>
        </ul>
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === 'rollback' && !!status?.previous}
        onClose={() => setConfirm(null)}
        danger={false}
        icon="undo"
        title={t('updates.rollback.confirmTitle', { version: status?.previous ?? '' })}
        description={t('updates.rollback.confirmText', { name: Name, version: status?.previous ?? '', current })}
        confirmLabel={t('updates.rollback.confirmAction')}
        onConfirm={() => startRollback(status!.previous, current)}
      >
        <ul className="st-upd-list">
          <li>{t('updates.confirm.signin', { name: Name })}</li>
          <li>{t('updates.confirm.interrupt')}</li>
        </ul>
      </ConfirmDialog>
    </div>
  );
}

const isFinal = (p: RunPhase) => p === 'done' || p === 'rolled-back' || p === 'timeout' || p === 'error';

function LastLine({ last }: { last: NonNullable<UpdateStatus['last']> }) {
  const t = useT('settings');
  const when = last.finishedAt ?? last.startedAt;
  const tone = last.state === 'ok' ? 'ok' : last.state === 'running' ? 'info' : 'err';
  const icon: IconName = last.state === 'ok' ? 'check' : last.state === 'running' ? 'clock' : 'alert';
  const what = t(`updates.last.${last.kind}`, { from: last.from ?? '?', to: last.to || '?' });
  return (
    <div className={`st-upd-last st-upd-last--${tone}`}>
      <Icon name={icon} />
      <div className="grow">
        <b>{t(`updates.last.state.${last.state}`)}{last.auto ? ` · ${t('updates.last.auto')}` : ''}</b>
        <small>{what} · {formatDateTime(when)}</small>
        {last.error && <small className="st-upd-err">{last.error}</small>}
      </div>
    </div>
  );
}

const UPDATE_STEPS = ['download', 'verify', 'install', 'restart', 'waiting'] as const;
const ROLLBACK_STEPS = ['restart', 'waiting'] as const;
const stepOf = (p: RunPhase): string => (p === 'check' ? 'download' : p === 'extract' || p === 'test' ? 'install' : p);

function RunView({ run, onClose }: { run: RunState; onClose(): void }) {
  const t = useT('settings');
  const steps: readonly string[] = run.kind === 'update' ? UPDATE_STEPS : ROLLBACK_STEPS;
  const final = isFinal(run.phase);
  const cur = stepOf(run.phase);
  const curIdx = run.phase === 'done' ? steps.length : steps.indexOf(cur);
  const failedIdx = final && run.phase !== 'done' ? Math.max(curIdx, 0) : -1;
  return (
    <div className="st-upd-run" role="status" aria-live="polite">
      <b className="st-upd-run-title">
        {run.kind === 'update' ? t('updates.run.title', { version: run.target }) : t('updates.run.rollbackTitle', { version: run.target })}
      </b>
      <ol className="st-upd-steps">
        {steps.map((s, i) => {
          const state = i === failedIdx ? 'err' : i < curIdx ? 'done' : i === curIdx && !final ? 'now' : 'todo';
          return (
            <li key={s} className={`st-upd-step st-upd-step--${state}`}>
              <span className="mk">{state === 'done' ? <Icon name="check" /> : state === 'err' ? <Icon name="x" /> : <i />}</span>
              <div className="grow">
                <span>{t(`updates.run.${s}`, { version: run.target })}</span>
                {s === 'download' && state === 'now' && run.phase === 'download' && run.total ? (
                  <div className="st-upd-prog">
                    <Progress value={run.percent ?? 0} label={t('updates.run.download')} />
                    <small>{formatBytes(run.done ?? 0)} / {formatBytes(run.total)} · {run.percent ?? 0}%</small>
                  </div>
                ) : null}
                {s === 'waiting' && state === 'now' && <div className="st-upd-prog"><Progress label={t('updates.run.waiting', { version: run.target })} /></div>}
              </div>
            </li>
          );
        })}
      </ol>
      {run.phase === 'done' && <div className="st-upd-note st-upd-note--ok"><Icon name="check" />{t('updates.run.done', { version: run.target })}</div>}
      {run.phase === 'rolled-back' && <div className="st-warn"><Icon name="alert" /><div>{t('updates.run.rolledBack', { version: run.target, from: run.from })}</div></div>}
      {run.phase === 'timeout' && <div className="st-warn"><Icon name="alert" /><div>{t('updates.run.timeout')}</div></div>}
      {run.phase === 'error' && <div className="st-warn"><Icon name="alert" /><div>{t('updates.run.error', { error: run.error ?? '' })}</div></div>}
      {(run.phase === 'rolled-back' || run.phase === 'timeout') && (
        <div className="st-upd-actions"><Button icon="refresh" onClick={() => window.location.reload()}>{t('updates.run.reload')}</Button></div>
      )}
      {run.phase === 'error' && <div className="st-upd-actions"><Button onClick={onClose}>{t('updates.run.close')}</Button></div>}
    </div>
  );
}
