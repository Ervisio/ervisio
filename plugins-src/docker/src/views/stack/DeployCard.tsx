import { useEffect, useRef } from 'react';
import { relativeTime } from '../../api/format';
import { t } from '../../i18n';
import { Icon } from '../../kit';
import { lineTone, type DeployState } from './deployLog';

/** Output pane: the last command run on the stack (deploy, pull, restart, down). */
export function DeployOutput({ state, deployedAt }: { state: DeployState | undefined; deployedAt?: number }) {
  const box = useRef<HTMLPreElement>(null);
  const n = state?.lines.length ?? 0;
  useEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [n]);
  if (!state) {
    return <p className="dk-muted">{deployedAt ? t('stacks.deploy.lastAt', { ago: relativeTime(deployedAt) }) : t('stacks.deploy.never')}</p>;
  }
  const done = !state.running && state.code === 0;
  const failed = !state.running && state.code !== 0;
  return (
    <>
      <div className={`dk-sk-result${done ? ' dk-sk-result--ok' : failed ? ' dk-sk-result--err' : ''}`}>
        {state.running ? <span className="dk-sk-spin" aria-hidden="true" /> : <Icon name={done ? 'check' : 'alert'} />}
        <b>{state.title}</b>
        <span>{state.running ? t('stacks.deploy.running') : done ? t('stacks.deploy.ok') : t('stacks.deploy.failed', { code: state.code ?? -1 })}</span>
      </div>
      <pre className="dk-sk-dep" ref={box} aria-live="polite">
        {state.lines.length === 0 && <span className="dk-t-c">{t('stacks.deploy.waiting')}</span>}
        {state.lines.map((l, i) => (
          <span key={i} className={`dk-sk-dl dk-sk-dl--${lineTone(l) || 'n'}`}>{l.text}{'\n'}</span>
        ))}
      </pre>
    </>
  );
}
