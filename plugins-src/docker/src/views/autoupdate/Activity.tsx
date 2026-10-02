import { useState } from 'react';
import type { Session } from '../../api/autoupdate';
import { relativeTime } from '../../api/format';
import { t } from '../../i18n';
import { Badge, Button, Icon } from '../../kit';

export interface RunState {
  mode: 'look' | 'update';
  phase: 'pull' | 'run' | 'done' | 'error';
  lines: string[];
  error?: string;
}

/** Output of "Check now" / "Update now". */
export function RunCard({ run, session, onClose }: { run: RunState; session?: Session; onClose(): void }) {
  const busy = run.phase === 'pull' || run.phase === 'run';
  return (
    <section className="dk-card dk-au-card dk-au-run">
      <div className="dk-card-h">
        <h3><Icon name={busy ? 'refresh' : run.phase === 'error' ? 'alert' : 'check'} /> {t(run.mode === 'look' ? 'autoupdate.run.title.look' : 'autoupdate.run.title.update')}</h3>
        <Badge tone={run.phase === 'error' ? 'err' : run.phase === 'done' ? 'ok' : 'info'}>{t(run.phase === 'pull' ? 'autoupdate.run.pull' : run.phase === 'run' ? 'autoupdate.run.running' : run.phase === 'done' ? 'autoupdate.run.done' : 'autoupdate.run.failed')}</Badge>
        {!busy && <Button size="sm" variant="ghost" onClick={onClose}>{t('autoupdate.run.close')}</Button>}
      </div>
      {run.error && <p className="dk-au-warn">{t('autoupdate.run.failedToRun')}: {run.error}</p>}
      {run.phase === 'done' && session && (
        <div className="dk-au-chips">
          {session.found.length === 0 && session.updated === 0 && <Badge tone="ok">{t('autoupdate.run.none')}</Badge>}
          {session.found.length > 0 && run.mode === 'look' && <Badge tone="warn">{t('autoupdate.run.found', { n: session.found.length })}</Badge>}
          {session.updated > 0 && <Badge tone="ok">{t('autoupdate.run.updated', { n: session.updated })}</Badge>}
          {session.failed > 0 && <Badge tone="err">{t('autoupdate.run.failedN', { n: session.failed })}</Badge>}
        </div>
      )}
      <pre className="dk-au-log" aria-live="polite">{run.lines.length ? run.lines.map(prettyLine).join('\n') : '…'}</pre>
    </section>
  );
}

/** One log line without the logfmt noise. */
export function prettyLine(line: string): string {
  const m = /^time=(\S+) level=(\S+) msg=(?:"((?:[^"\\]|\\.)*)"|(\S+))(.*)$/.exec(line);
  if (!m) return line;
  const when = m[1].replace(/^.*T/, '').replace(/Z$/, '');
  return `${when}  ${m[3] ?? m[4]}${m[5]}`;
}

/** Containers that have a newer image, from the most recent check. */
export function AvailableCard({ session }: { session: Session | undefined }) {
  return (
    <section className="dk-card dk-au-card">
      <div className="dk-card-h"><h3><Icon name="download" /> {t('autoupdate.available')}</h3></div>
      {!session || session.found.length === 0 ? (
        <p className="dk-au-sub">{t('autoupdate.available.none')}</p>
      ) : (
        <div className="dk-au-list">
          {session.found.map((f) => (
            <div key={f.container + f.image} className="dk-au-row">
              <span className="dk-au-ic hue-log"><Icon name="download" /></span>
              <div className="dk-au-tx"><b>{f.container}</b><span className="dk-mono">{f.image}</span></div>
            </div>
          ))}
        </div>
      )}
      <p className="dk-au-sub">{t('autoupdate.available.hint')}</p>
    </section>
  );
}

export function HistoryCard({ sessions, raw }: { sessions: Session[]; raw: string[] }) {
  const [showLog, setShowLog] = useState(false);
  const lines = raw;
  return (
    <section className="dk-card dk-au-card">
      <div className="dk-card-h">
        <h3><Icon name="logs" /> {t('autoupdate.history')}</h3>
        {lines.length > 0 && <Button size="sm" variant="ghost" onClick={() => setShowLog((v) => !v)}>{t(showLog ? 'autoupdate.history.hideLog' : 'autoupdate.history.showLog')}</Button>}
      </div>
      {sessions.length === 0 ? (
        <p className="dk-au-sub">{t('autoupdate.history.none')}</p>
      ) : (
        <div className="dk-au-list">
          {sessions.slice(0, 12).map((s, i) => (
            <div key={i} className="dk-au-row dk-au-row--col">
              <div className="dk-au-line">
                <span className={`dk-au-ic ${s.failed || s.errors.length ? 'hue-svc' : s.updated ? 'hue-term' : 'hue-file'}`}><Icon name={s.failed || s.errors.length ? 'alert' : 'check'} /></span>
                <div className="dk-au-tx">
                  <b>{t('autoupdate.history.row', { scanned: s.scanned, updated: s.updated })}</b>
                  <span title={new Date(s.at).toLocaleString()}>{s.at ? `${new Date(s.at).toLocaleString()} · ${relativeTime(new Date(s.at).toISOString())}` : ''}</span>
                </div>
              </div>
              {(s.updatedNames.length > 0 || s.found.length > 0 || s.errors.length > 0) && (
                <div className="dk-au-chips">
                  {s.updatedNames.map((n) => <Badge key={`u${n}`} tone="ok">{t('autoupdate.history.updated')}: {n}</Badge>)}
                  {s.found.filter((f) => !s.updatedNames.includes(f.container)).map((f) => <Badge key={`f${f.container}`} tone="warn">{t('autoupdate.history.found')}: {f.container}</Badge>)}
                  {s.errors.map((e, k) => <Badge key={`e${k}`} tone="err">{e}</Badge>)}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
      {showLog && <pre className="dk-au-log">{lines.slice(-200).map(prettyLine).join('\n')}</pre>}
    </section>
  );
}
