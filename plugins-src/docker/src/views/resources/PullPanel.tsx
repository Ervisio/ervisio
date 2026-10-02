import { useEffect, useRef, useState } from 'react';
import { registryHost } from '../../api/registries';
import { shortId } from '../../api/format';
import { t } from '../../i18n';
import { Button, Icon, Input } from '../../kit';
import { isValidRef } from './imageRef';
import { pullImage, type PullState } from './pull';

/**
 * Inline "Pull image" form with live progress: one bar per layer, fed by the JSON lines of /images/create.
 * `initial` + `autostart` is the "Pull again" row action.
 */
export function PullPanel({ initial = '', autostart = false, onClose, onDone }: {
  initial?: string;
  autostart?: boolean;
  onClose(): void;
  onDone?(ref: string): void;
}) {
  const [ref, setRef] = useState(initial);
  const [state, setState] = useState<PullState | null>(null);
  const handle = useRef<{ close(): void } | null>(null);
  const started = useRef(false);
  const running = !!state && !state.finished && !state.error;
  const valid = isValidRef(ref);

  const start = (r = ref) => {
    handle.current?.close();
    setState({ layers: [], message: '', finished: false });
    handle.current = pullImage(r.trim(), (s) => {
      setState(s);
      if (s.finished && !s.error) onDone?.(r.trim());
    });
  };

  useEffect(() => {
    if (autostart && initial && !started.current) {
      started.current = true;
      start(initial);
    }
    return () => handle.current?.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const layers = state?.layers ?? [];
  const doneCount = layers.filter((l) => l.done).length;
  const host = valid ? registryHost(ref) : '';
  return (
    <section className="dk-card dk-pull" aria-label={t('res.pull.title')}>
      <div className="dk-card-h">
        <h3>{t('res.pull.title')}</h3>
        <Button size="sm" variant="ghost" icon="close" onClick={() => { handle.current?.close(); onClose(); }}>{running ? t('res.pull.cancel') : t('common.close')}</Button>
      </div>
      <div className="dk-pull-f" onKeyDown={(e) => { if (e.key === 'Enter' && valid && !running) { e.preventDefault(); start(); } }}>
        <Input
          fieldClassName="dk-grow"
          mono
          placeholder={t('res.pull.placeholder')}
          value={ref}
          onChange={(e) => setRef(e.target.value)}
          aria-label={t('res.pull.title')}
          disabled={running}
          autoFocus
          hint={host ? t('res.pull.from', { host }) : t('res.pull.hint')}
        />
        <Button variant="primary" icon="download" disabled={!valid || running} loading={running} onClick={() => start()}>{t('res.pull.go')}</Button>
      </div>
      {state && (
        <div className="dk-layers" aria-live="polite">
          {layers.length > 0 && <small className="dk-muted">{t('res.pull.layers', { done: doneCount, total: layers.length })}</small>}
          {layers.map((l) => (
            <div key={l.id} className={`dk-layer${l.done ? ' dk-layer--done' : ''}`}>
              <code>{shortId(l.id)}</code>
              <span className="dk-bar" role="progressbar" aria-valuenow={Math.round(l.pct * 100)} aria-valuemin={0} aria-valuemax={100}><i style={{ width: `${Math.round(l.pct * 100)}%` }} /></span>
              <small>{l.status}</small>
            </div>
          ))}
          {state.error && <p className="dk-fail"><Icon name="alert" />{state.error}</p>}
          {!state.error && state.finished && <p className="dk-okmsg"><Icon name="check" />{state.message || t('res.pull.done')}</p>}
          {!state.error && !state.finished && state.message && <small className="dk-muted">{state.message}</small>}
        </div>
      )}
    </section>
  );
}
