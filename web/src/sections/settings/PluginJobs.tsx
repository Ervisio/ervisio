import { useEffect, useMemo, useState } from 'react';
import { useI18n, useT } from '../../i18n';
import { formatDateTime, formatDuration, relativeTime } from '../../lib/format';
import { Badge, Button, ConfirmDialog, Dialog, Icon, IconButton, Input, Switch, toast, type Tone } from '../../ui';
import {
  approveJob, createWebhook, deleteJob, jobHistory, listJobs, regenerateWebhook, revokeWebhook, runJob, setJobEnabled,
  type JobInstance, type NewWebhook, type RunLog, type Schedule,
} from './jobsApi';
import { LockedPanel, useAdminLoad } from './useAdminLoad';

const STATUS_TONE: Record<string, Tone> = { ok: 'ok', failed: 'err', timeout: 'err', running: 'info', queued: 'info', cancelled: 'neutral' };

/** "Every 5 min", "Every day at 03:30", "Mon, Fri at 08:00". */
export function describeSchedule(t: (k: string, v?: Record<string, string | number>) => string, lang: string, s?: Schedule): string {
  if (!s) return t('jobs.onDemand');
  if (s.every) {
    const e = s.every;
    if (e % 86400 === 0) return t('jobs.everyDays', { n: e / 86400 });
    if (e % 3600 === 0) return t('jobs.everyHours', { n: e / 3600 });
    if (e % 60 === 0) return t('jobs.everyMinutes', { n: e / 60 });
    return t('jobs.everySeconds', { n: e });
  }
  const times = (s.at ?? []).join(', ');
  if (!s.days?.length) return t('jobs.daily', { times });
  // 2024-01-07 was a Sunday.
  const days = s.days.map((d) => new Date(2024, 0, 7 + d).toLocaleDateString(lang, { weekday: 'short' })).join(', ');
  return t('jobs.weekly', { days, times });
}

const hookUrl = (path: string) => `${window.location.origin}${path}`;

/** Settings > Plugin jobs (administrators): every job instance plugins created, with owner, schedule, last run, webhooks. */
export function PluginJobsBlock() {
  const t = useT('settings');
  const { lang } = useI18n();
  const { data, state, error, reload } = useAdminLoad(listJobs, 5000);
  const [logs, setLogs] = useState<JobInstance | null>(null);
  const [removing, setRemoving] = useState<JobInstance | null>(null);
  const [hook, setHook] = useState<{ job: JobInstance; created: NewWebhook } | null>(null);
  const [naming, setNaming] = useState<JobInstance | null>(null);
  const [revoking, setRevoking] = useState<{ job: JobInstance; id: string; label: string } | null>(null);
  const [busy, setBusy] = useState('');
  const [approving, setApproving] = useState<JobInstance | null>(null);

  if (state === 'locked') return <LockedPanel title={t('jobs.lockedTitle')} text={t('jobs.lockedText')} onUnlocked={() => void reload()} />;
  if (state === 'failed') return <div className="st-warn" role="alert"><Icon name="alert" /><div>{error}</div></div>;
  const all = data?.instances ?? [];
  const byPlugin = new Map<string, JobInstance[]>();
  for (const j of all) byPlugin.set(j.plugin, [...(byPlugin.get(j.plugin) ?? []), j]);

  const fail = (name: string, e: unknown) => toast.err(t('saveFailed', { name }), e instanceof Error ? e.message : undefined);
  const label = (j: JobInstance) => j.name || j.job;

  return (
    <>
      <p className="st-note">{t('jobs.intro')}</p>
      {all.length === 0 && state === 'ready' && <div className="st-tx" style={{ padding: '6px 0' }}><small>{t('jobs.empty')}</small></div>}
      {[...byPlugin.entries()].map(([plugin, list]) => (
        <div key={plugin}>
          <div className="st-gl2">{plugin}</div>
          {list.map((j) => {
            const last = j.last;
            return (
              <div className="st-job" key={j.id}>
                <div className="st-job-hd">
                  <div className="grow">
                    <b>{label(j)}</b>
                    <small>{t('jobs.owner', { user: j.owner })} · {describeSchedule(t, lang, j.schedule)}{j.name ? ` · ${j.job}` : ''}</small>
                  </div>
                  {j.running ? <Badge tone="info">{t('jobs.status.running')}</Badge> : !j.enabled ? <Badge>{t('jobs.status.disabled')}</Badge> : j.awaitingApproval ? <Badge tone="warn">{t('jobs.status.awaiting')}</Badge> : last ? <Badge tone={STATUS_TONE[last.status] ?? 'neutral'}>{t(`jobs.status.${last.status}`)}</Badge> : <Badge>{t('jobs.status.never')}</Badge>}
                  <Switch aria-label={t('jobs.enable', { name: label(j) })} checked={j.enabled} onChange={(on) => setJobEnabled(j.id, on).then(() => reload(), (e) => fail(label(j), e))} />
                </div>
                {j.disabledReason && !j.enabled && <div className="st-warn"><Icon name="alert" /><div>{j.disabledReason}</div></div>}
                {j.needsAdmin && (
                  <div className={`st-warn${j.approval?.valid ? ' st-warn--soft' : ''}`}>
                    <Icon name="shield" />
                    <div className="grow">{j.approval ? (j.approval.valid ? t('jobs.admin', { by: j.approval.by, when: formatDateTime(j.approval.at) }) : t('jobs.adminStale')) : t('jobs.adminMissing')}</div>
                    {j.awaitingApproval && <Button size="sm" icon="shield" onClick={() => setApproving(j)}>{t('jobs.approve.action')}</Button>}
                  </div>
                )}
                <div className="st-job-meta">
                  {last && <small>{t('jobs.lastRun', { when: relativeTime(last.started, lang) })}{last.error ? ` · ${last.error}` : ''}</small>}
                  {j.enabled && !j.awaitingApproval && j.nextRun ? <small>{t('jobs.nextRun', { when: relativeTime(j.nextRun, lang) })}</small> : null}
                </div>
                <div className="st-job-hooks">
                  <b>{t('jobs.webhooks.title')}</b>
                  {j.webhooks.length === 0 && <small>{t('jobs.webhooks.none')}</small>}
                  {j.webhooks.map((w) => (
                    <div className="st-host" key={w.id}>
                      <Icon name="link" size={15} />
                      <div className="grow">
                        <b>{w.label || t('jobs.webhooks.unnamed')}</b>
                        <small>{t('jobs.webhooks.created', { when: formatDateTime(w.created) })} · {w.lastUsed ? t('jobs.webhooks.used', { when: relativeTime(w.lastUsed, lang) }) : t('jobs.webhooks.unused')}</small>
                      </div>
                      <Button size="sm" icon="refresh" onClick={() => regenerateWebhook(j.id, w.id).then((created) => { setHook({ job: j, created }); void reload(); }, (e) => fail(label(j), e))}>{t('jobs.webhooks.regenerate')}</Button>
                      <IconButton icon="trash" label={t('jobs.webhooks.revokeLabel', { name: w.label || w.id })} onClick={() => setRevoking({ job: j, id: w.id, label: w.label || w.id })} />
                    </div>
                  ))}
                  <div><Button size="sm" icon="plus" disabled={j.webhooks.length >= 5} onClick={() => setNaming(j)}>{t('jobs.webhooks.add')}</Button></div>
                </div>
                <div className="st-job-act">
                  <Button icon="play" loading={busy === j.id} disabled={!j.enabled || j.running || !!j.awaitingApproval} onClick={async () => {
                    setBusy(j.id);
                    try { await runJob(j.id); toast.info(t('jobs.started', { name: label(j) })); void reload(); } catch (e) { fail(label(j), e); } finally { setBusy(''); }
                  }}>{t('jobs.runNow')}</Button>
                  <Button icon="logs" onClick={() => setLogs(j)}>{t('jobs.logs')}</Button>
                  <IconButton icon="trash" label={t('jobs.delete', { name: label(j) })} onClick={() => setRemoving(j)} />
                </div>
              </div>
            );
          })}
        </div>
      ))}

      {logs && <LogsDialog job={logs} onClose={() => setLogs(null)} />}
      {naming && <NameHookDialog onClose={() => setNaming(null)} onCreate={async (name) => {
        const j = naming;
        try { const created = await createWebhook(j.id, name); setNaming(null); setHook({ job: j, created }); void reload(); } catch (e) { fail(label(j), e); }
      }} />}
      {hook && (
        <Dialog open onClose={() => setHook(null)} title={t('jobs.webhooks.urlTitle')} description={t('jobs.webhooks.urlText')} icon="link"
          footer={<Button variant="primary" onClick={() => setHook(null)}>{t('jobs.webhooks.done')}</Button>}>
          <Input label={t('jobs.webhooks.url')} mono readOnly value={hookUrl(hook.created.path)} onFocus={(e) => e.currentTarget.select()}
            end={<IconButton icon="copy" label={t('jobs.webhooks.copy')} onClick={() => navigator.clipboard?.writeText(hookUrl(hook.created.path)).then(() => toast.ok(t('jobs.webhooks.copied')), () => undefined)} />} />
          <small className="st-note">{t('jobs.webhooks.curl')}</small>
          <code className="st-code">curl -X POST {hookUrl(hook.created.path)}</code>
        </Dialog>
      )}
      <ConfirmDialog open={!!revoking} onClose={() => setRevoking(null)} title={t('jobs.webhooks.revokeTitle')} description={t('jobs.webhooks.revokeText')} confirmLabel={t('jobs.webhooks.revoke')} cancelLabel={t('cancel')}
        onConfirm={async () => { if (revoking) { await revokeWebhook(revoking.job.id, revoking.id); void reload(); } }} />
      <ConfirmDialog open={!!approving} onClose={() => setApproving(null)} danger={false} icon="shield"
        title={t('jobs.approve.title', { name: approving ? label(approving) : '' })}
        description={t('jobs.approve.text', { plugin: approving?.plugin ?? '', user: approving?.owner ?? '' })}
        confirmLabel={t('jobs.approve.confirm')} cancelLabel={t('cancel')}
        onConfirm={async () => { if (approving) { await approveJob(approving.id); toast.ok(t('jobs.approve.done', { name: label(approving) })); void reload(); } }}>
        {approving && <ApprovalDetails job={approving} />}
      </ConfirmDialog>
      <ConfirmDialog open={!!removing} onClose={() => setRemoving(null)} title={t('jobs.deleteTitle')} description={t('jobs.deleteText', { name: removing ? label(removing) : '' })} confirmLabel={t('jobs.deleteAction')} cancelLabel={t('cancel')}
        onConfirm={async () => { if (removing) { await deleteJob(removing.id); void reload(); } }} />
    </>
  );
}

/** What an approval covers: the values the job runs with and each step it runs as root. */
function ApprovalDetails({ job }: { job: JobInstance }) {
  const t = useT('settings');
  const params = Object.entries(job.params);
  return (
    <div className="st-approve">
      <b>{t('jobs.approve.steps')}</b>
      <ul>
        {(job.adminSteps ?? []).map((s) => (
          <li key={s.id}>
            <small>{t('jobs.approve.step', { id: s.id })}</small>
            <code className="st-code">{s.kind === 'command' ? (s.argv ?? []).join(' ') : s.http}</code>
          </li>
        ))}
      </ul>
      <b>{t('jobs.approve.values')}</b>
      {params.length === 0 ? <small>{t('jobs.approve.noValues')}</small> : (
        <dl>
          {params.map(([k, v]) => (<div key={k}><dt>{k}</dt><dd><code>{v}</code></dd></div>))}
        </dl>
      )}
      <small className="st-note">{t('jobs.approve.note')}</small>
    </div>
  );
}

function NameHookDialog({ onClose, onCreate }: { onClose(): void; onCreate(name: string): Promise<void> }) {
  const t = useT('settings');
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open onClose={onClose} title={t('jobs.webhooks.addTitle')} description={t('jobs.webhooks.addText')} icon="link"
      onSubmit={(e) => { e.preventDefault(); setBusy(true); void onCreate(name.trim()).finally(() => setBusy(false)); }}
      footer={<><Button variant="ghost" onClick={onClose}>{t('cancel')}</Button><Button type="submit" variant="primary" loading={busy}>{t('jobs.webhooks.create')}</Button></>}>
      <Input data-autofocus label={t('jobs.webhooks.label')} value={name} onChange={(e) => setName(e.target.value)} placeholder="GitHub Actions" maxLength={60} />
    </Dialog>
  );
}

function LogsDialog({ job, onClose }: { job: JobInstance; onClose(): void }) {
  const t = useT('settings');
  const { lang } = useI18n();
  const { data, state, error } = useAdminLoad(() => jobHistory(job.id), 3000);
  const runs = useMemo(() => data?.runs ?? [], [data]);
  const [sel, setSel] = useState('');
  useEffect(() => {
    if (!sel && runs.length) setSel(runs[0].id);
  }, [runs, sel]);
  const run: RunLog | undefined = runs.find((r) => r.id === sel) ?? runs[0];
  return (
    <Dialog open onClose={onClose} size="lg" title={t('jobs.logsTitle', { name: job.name || job.job })} icon="logs" footer={<Button onClick={onClose}>{t('jobs.close')}</Button>}>
      {state === 'failed' && <div className="st-warn" role="alert"><Icon name="alert" /><div>{error}</div></div>}
      {state !== 'failed' && runs.length === 0 && <small>{t('jobs.noRuns')}</small>}
      {runs.length > 0 && (
        <div className="st-runs">
          <ul className="st-runlist" aria-label={t('jobs.logs')}>
            {runs.map((r) => (
              <li key={r.id}>
                <button type="button" aria-current={r.id === run?.id} onClick={() => setSel(r.id)}>
                  <Badge tone={STATUS_TONE[r.status] ?? 'neutral'}>{t(`jobs.status.${r.status}`)}</Badge>
                  <span>{formatDateTime(r.started)}</span>
                  <small>{r.trigger === 'manual' ? t('jobs.trigger.manual', { by: r.by ?? '' }) : t(`jobs.trigger.${r.trigger}`)}{r.ended ? ` · ${formatDuration((r.ended - r.started) / 1000)}` : ''}</small>
                </button>
              </li>
            ))}
          </ul>
          {run && (
            <div className="st-runbody" lang={lang}>
              {run.error && <div className="st-warn"><Icon name="alert" /><div>{run.error}</div></div>}
              {run.steps.length === 0 && <small>{t('jobs.noSteps')}</small>}
              {run.steps.map((s) => (
                <div className="st-step" key={s.id}>
                  <div className="st-step-hd">
                    <Badge tone={s.status === 'ok' ? 'ok' : s.status === 'failed' ? (s.handled ? 'warn' : 'err') : 'neutral'}>{s.handled ? t('jobs.step.handled') : t(`jobs.step.${s.status}`)}</Badge>
                    <b>{s.id}</b>
                    <small>
                      {s.exitCode !== undefined ? t('jobs.step.exit', { code: s.exitCode }) : ''}{s.httpStatus ? t('jobs.step.http', { status: s.httpStatus }) : ''}
                      {s.admin ? ` · ${t('jobs.step.admin')}` : ''}{s.durationMs ? ` · ${s.durationMs} ms` : ''}
                    </small>
                  </div>
                  {s.error && <small className="st-job-last--err">{s.error}</small>}
                  {s.stdout && <pre className="st-log" aria-label={t('jobs.step.stdout')}>{s.stdout}</pre>}
                  {s.stderr && <pre className="st-log st-log--err" aria-label={t('jobs.step.stderr')}>{s.stderr}</pre>}
                  {s.truncated && <small>{t('jobs.step.truncated')}</small>}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </Dialog>
  );
}
