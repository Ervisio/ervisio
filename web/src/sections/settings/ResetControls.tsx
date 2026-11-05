import { useEffect, useRef, useState } from 'react';
import { apiUrl, call, errorMessage, usePrefs, useSession } from '../../api';
import { useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import { Button, Checkbox, ConfirmDialog, Dialog, Icon, toast } from '../../ui';
import { resetBrowserPreferences } from './resetBrowser';
import { trackResetProgress } from './ResetProgressPage';
import './reset.css';

export interface ResetPreview {
  token: string;
  expiresAt: number;
  paths: { path: string; files: number; bytes: number }[];
  preserved: string[];
  files: number;
  bytes: number;
  backupRoot: string;
  nextURL: string;
}
export interface ResetStatus {
  id: string;
  state: 'running' | 'done' | 'failed' | 'rolled-back';
  phase: string;
  backup?: string;
  error?: string;
  nextURL?: string;
  restartRequired?: boolean;
  statusToken?: string;
  expectedBackupPath?: string;
}

export function ResetControls({ administrative }: { administrative: boolean }) {
  const t = useT('settings');
  const { session } = useSession();
  const { reset } = usePrefs();
  const [personal, setPersonal] = useState(false);
  const [clearBrowser, setClearBrowser] = useState(true);
  const [preserveAccess, setPreserveAccess] = useState(true);
  const [loading, setLoading] = useState(false);
  const [preview, setPreview] = useState<ResetPreview | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [job, setJob] = useState<ResetStatus | null>(null);
  const [failure, setFailure] = useState('');
  const jobId = job?.id;
  const live = useRef(true);
  useEffect(
    () => () => {
      live.current = false;
    },
    [],
  );
  useEffect(() => {
    if (job?.state === 'done') {
      try {
        resetBrowserPreferences(localStorage);
      } catch {
        /* unavailable */
      }
    }
  }, [job?.state]);
  useEffect(() => {
    if (!jobId || job?.state !== 'running') return;
    let disposed = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        let status: ResetStatus;
        if (job.statusToken) {
          const response = await fetch(
            apiUrl(
              `/api/maintenance/reset/status?id=${encodeURIComponent(jobId)}&token=${encodeURIComponent(job.statusToken)}`,
            ),
            { cache: 'no-store', credentials: 'same-origin' },
          );
          if (!response.ok) throw new Error(t('reset.statusUnavailable'));
          const body = await response.json();
          status = body.result ?? body;
        } else
          status = await call<ResetStatus>(
            'maintenance.reset.status',
            { id: jobId },
            { noUnlock: true },
          );
        if (!disposed) {
          setJob((previous) => ({ ...previous, ...status, id: jobId }));
          setFailure('');
        }
      } catch (error) {
        if (!disposed) setFailure(errorMessage(error));
      }
      if (!disposed) timer = setTimeout(() => void poll(), 1500);
    };
    void poll();
    return () => {
      disposed = true;
      clearTimeout(timer);
    };
  }, [jobId, job?.state, job?.statusToken, t]);
  const inspect = async () => {
    setLoading(true);
    setFailure('');
    try {
      setPreview(await call<ResetPreview>('maintenance.reset.preview', { preserveAccess }));
    } catch (error) {
      setFailure(errorMessage(error));
    } finally {
      setLoading(false);
    }
  };
  return (
    <div className="set-reset">
      <section className="set-reset-scope">
        <div>
          <h3>{t('reset.personalTitle')}</h3>
          <p>{t('reset.personalDescription')}</p>
        </div>
        <ul>
          <li>{t('reset.personalItems')}</li>
          <li>{t('reset.personalPreserved')}</li>
        </ul>
        <Checkbox checked={clearBrowser} onChange={setClearBrowser} label={t('reset.browser')} />
        <Button icon="undo" onClick={() => setPersonal(true)}>
          {t('reset.personalAction')}
        </Button>
      </section>
      {administrative && (
        <section className="set-reset-scope set-reset-factory">
          <div>
            <h3>{t('reset.factoryTitle')}</h3>
            <p>{t('reset.factoryDescription')}</p>
          </div>
          <p>{t('reset.factoryPreserved')}</p>
          <Checkbox
            checked={preserveAccess}
            onChange={(value) => {
              setPreserveAccess(value);
              setPreview(null);
            }}
            label={t('reset.keepAccess')}
          />
          <p>{t('reset.keepAccessHint')}</p>
          <Button
            variant="danger"
            icon="alert"
            loading={loading}
            disabled={job?.state === 'running'}
            onClick={() => void inspect()}
          >
            {t('reset.previewAction')}
          </Button>
        </section>
      )}
      {failure && (
        <p className="set-reset-error" role="alert">
          {failure}
        </p>
      )}
      <ConfirmDialog
        open={personal}
        onClose={() => setPersonal(false)}
        title={t('reset.personalTitle')}
        description={t('reset.personalConfirm')}
        confirmLabel={t('reset.personalAction')}
        confirmText={session?.user}
        onConfirm={async () => {
          await reset();
          if (clearBrowser) {
            try {
              resetBrowserPreferences(localStorage);
            } catch {
              /* Server preferences were reset even when browser storage is blocked. */
            }
          }
          toast.ok(t('reset.personalDone'));
          window.location.assign('/');
        }}
      />
      {preview && !confirm && (
        <Dialog
          open
          title={t('reset.previewTitle')}
          icon="alert"
          onClose={() => !loading && setPreview(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setPreview(null)}>
                {t('cancel')}
              </Button>
              <Button variant="danger" onClick={() => setConfirm(true)}>
                {t('reset.continue')}
              </Button>
            </>
          }
        >
          <div className="set-reset-preview">
            <p>
              {t('reset.previewSummary', {
                count: preview.files,
                size: formatBytes(preview.bytes),
              })}
            </p>
            <h4>{t('reset.backup')}</h4>
            <code>{preview.backupRoot}</code>
            <h4>{t('reset.included')}</h4>
            <ul className="set-reset-paths">
              {preview.paths.map((entry) => (
                <li key={entry.path}>
                  <code>{entry.path}</code>
                  <span>{formatBytes(entry.bytes)}</span>
                </li>
              ))}
            </ul>
            <h4>{t('reset.preserved')}</h4>
            <ul>
              {preview.preserved.map((path) => (
                <li key={path}>{path}</li>
              ))}
            </ul>
            <h4>{t('reset.nextURL')}</h4>
            <code>{preview.nextURL}</code>
            <p>{t('reset.backupExplanation')}</p>
          </div>
        </Dialog>
      )}
      <ConfirmDialog
        open={confirm && !!preview}
        onClose={() => setConfirm(false)}
        title={t('reset.factoryConfirmTitle')}
        description={t('reset.factoryConfirm')}
        confirmLabel={t('reset.factoryAction')}
        confirmText="RESET ERVISIO"
        onConfirm={async () => {
          if (!preview) return;
          const status = await call<ResetStatus>('maintenance.reset.apply', {
            token: preview.token,
            confirm: 'RESET ERVISIO',
          });
          const next = { ...status, nextURL: status.nextURL || preview.nextURL };
          if (!trackResetProgress(next)) setJob(next);
          setPreview(null);
        }}
      />
      {job && (
        <section className="set-reset-job" role="status">
          <Icon
            name={job.state === 'done' ? 'check' : job.state === 'running' ? 'refresh' : 'alert'}
          />
          <div>
            <h3>{t(`reset.status.${job.state}`)}</h3>
            <p>{t(`reset.phases.${job.phase}`)}</p>
            {job.backup && (
              <>
                <p>{t('reset.savedBackup')}</p>
                <code>{job.backup}</code>
              </>
            )}
            {!job.backup && job.expectedBackupPath && (
              <>
                <p>{t('reset.plannedBackup')}</p>
                <code>{job.expectedBackupPath}</code>
              </>
            )}
            {job.nextURL && (
              <p>
                <a href={job.nextURL}>{t('reset.openConsole')}</a>
              </p>
            )}
            {job.error && <p className="set-reset-error">{job.error}</p>}
          </div>
          {job.state === 'done' && job.restartRequired && (
            <Button
              onClick={async () => {
                try {
                  await call('maintenance.reset.restart', { id: job.id }, { noUnlock: true });
                  try {
                    resetBrowserPreferences(localStorage);
                  } catch {
                    /* unavailable */
                  }
                  setTimeout(() => window.location.assign(job.nextURL || '/'), 4000);
                } catch (error) {
                  if (live.current) setFailure(errorMessage(error));
                }
              }}
            >
              {t('reset.restart')}
            </Button>
          )}
        </section>
      )}
    </div>
  );
}
