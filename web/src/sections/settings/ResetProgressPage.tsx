import { useEffect, useState } from 'react';
import { API_BASE, apiUrl } from '../../api';
import { useT } from '../../i18n';
import { BrandMark, Button, Icon } from '../../ui';
import { resetBrowserPreferences } from './resetBrowser';
import type { ResetStatus } from './ResetControls';
import './reset.css';

const storageKey = `ervisio.maintenance.reset:${API_BASE}`;
const validCapability = (id: unknown, token: unknown) =>
  typeof id === 'string' &&
  /^[a-f0-9]{32}$/.test(id) &&
  typeof token === 'string' &&
  /^[a-f0-9]{64}$/.test(token);

/** A read-only job capability, held only by this tab while its reset is running. */
export function trackResetProgress(job: ResetStatus) {
  if (!validCapability(job.id, job.statusToken)) return false;
  try {
    sessionStorage.setItem(storageKey, JSON.stringify(job));
  } catch {
    /* The fragment is a tab-local fallback. */
  }
  const fragment = new URLSearchParams({ id: job.id, token: job.statusToken! });
  window.location.assign(`/maintenance/reset#${fragment}`);
  return true;
}

function initialJob(): ResetStatus | null {
  const fragment = new URLSearchParams(window.location.hash.slice(1));
  try {
    const saved = JSON.parse(sessionStorage.getItem(storageKey) || 'null') as ResetStatus | null;
    if (
      saved &&
      validCapability(saved.id, saved.statusToken) &&
      (!fragment.get('id') || saved.id === fragment.get('id'))
    )
      return saved;
  } catch {
    /* unavailable */
  }
  const id = fragment.get('id'),
    token = fragment.get('token');
  if (validCapability(id, token))
    return { id: id!, statusToken: token!, state: 'running', phase: 'backup' };
  return null;
}

/** Kept outside RequireAuth so restarting Ervisio cannot unmount the progress watcher. */
export function ResetProgressPage() {
  const t = useT('settings');
  const [job, setJob] = useState(initialJob);
  const [unavailable, setUnavailable] = useState(false);
  useEffect(() => {
    if (location.hash)
      history.replaceState(history.state, '', `${location.pathname}${location.search}`);
  }, []);
  useEffect(() => {
    if (!job?.statusToken || job.state !== 'running') return;
    const initial = job;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const controller = new AbortController();
    try {
      sessionStorage.setItem(storageKey, JSON.stringify(initial));
    } catch {
      /* unavailable */
    }
    const poll = async () => {
      try {
        const query = new URLSearchParams({ id: initial.id, token: initial.statusToken! });
        const response = await fetch(apiUrl(`/api/maintenance/reset/status?${query}`), {
          cache: 'no-store',
          credentials: 'omit',
          signal: controller.signal,
        });
        if (!response.ok) throw new Error('Reset status unavailable');
        const body = await response.json();
        const status = body.result ?? body;
        if (!stopped) {
          setJob((previous) => (previous ? { ...previous, ...status } : previous));
          setUnavailable(false);
        }
      } catch {
        if (!stopped) setUnavailable(true);
      }
      if (!stopped) timer = setTimeout(() => void poll(), 1500);
    };
    void poll();
    return () => {
      stopped = true;
      controller.abort();
      clearTimeout(timer);
    };
  }, [job?.id, job?.statusToken, job?.state]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (job && job.state !== 'running') {
      try {
        sessionStorage.removeItem(storageKey);
      } catch {
        /* unavailable */
      }
      if (job.state === 'done') {
        try {
          resetBrowserPreferences(localStorage);
        } catch {
          /* unavailable */
        }
      }
      setJob((previous) => (previous ? { ...previous, statusToken: undefined } : previous));
    }
  }, [job?.state]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <main className="set-reset-progress">
      <BrandMark size={42} />
      <h1>{t('reset.progressTitle')}</h1>
      {job ? (
        <>
          <section className="set-reset-job" role="status">
            <Icon
              name={job.state === 'done' ? 'check' : job.state === 'running' ? 'refresh' : 'alert'}
            />
            <div>
              <h2>{t(`reset.status.${job.state}`)}</h2>
              <p>{t(`reset.phases.${job.phase}`)}</p>
            </div>
          </section>
          {job.expectedBackupPath && (
            <>
              <p>{t(job.state === 'done' ? 'reset.savedBackup' : 'reset.plannedBackup')}</p>
              <code>{job.expectedBackupPath}</code>
            </>
          )}
          {unavailable && <p role="status">{t('reset.statusUnavailable')}</p>}
          <p>{t('reset.progressPrivacy')}</p>
          <a className="set-reset-console-link" href={job.nextURL || '/login'}>
            {t('reset.openConsole')}
          </a>
        </>
      ) : (
        <>
          <p>{t('reset.noActiveJob')}</p>
          <Button onClick={() => window.location.assign('/login')}>{t('reset.openConsole')}</Button>
        </>
      )}
    </main>
  );
}
export default ResetProgressPage;
