import { t } from '../../i18n';
import { Button, Icon, Progress } from '../../kit';
import type { PullState } from './pull';
import type { RunResult, Step } from './run';

/** Shows the steps of a create or recreate as they happen, and the outcome. */
export function RunView({ steps, pull, result, name, recreate, onBack, onOpen }: {
  steps: Step[];
  pull?: PullState;
  result?: RunResult;
  name: string;
  recreate: boolean;
  onBack(): void;
  onOpen(id: string): void;
}) {
  return (
    <section className="dk-cr-card dk-cr-runview" aria-live="polite">
      <h3>{recreate ? t('create.run.recreating', { name }) : t('create.run.creating')}</h3>
      <ol className="dk-cr-steps">
        {steps.filter((s) => s.status !== 'skip').map((s) => (
          <li key={s.id} className={`dk-cr-st dk-cr-st--${s.status}`}>
            <span className="dk-cr-stic">{s.status === 'done' ? <Icon name="check" size={14} /> : s.status === 'fail' ? <Icon name="close" size={14} /> : s.status === 'run' ? <i className="dk-cr-spin" /> : null}</span>
            <div>
              <b>{t(`create.step.${s.id}`)}</b>
              {s.detail && <span className="dk-muted dk-mono">{s.detail}</span>}
              {s.id === 'pull' && s.status === 'run' && pull && (
                <div className="dk-cr-pull">
                  <Progress value={pull.percent} label={t('create.pull.progress')} />
                  <span className="dk-muted">{pull.percent !== undefined ? `${pull.percent}% ` : ''}{pull.layers ? t('create.pull.layers', { done: pull.doneLayers, total: pull.layers }) : t('create.pull.starting')}</span>
                </div>
              )}
            </div>
          </li>
        ))}
      </ol>
      {result && !result.ok && (
        <div className="dk-cr-fail" role="alert">
          <b>{t('create.run.failed')}</b>
          <p>{result.error}</p>
          {result.rolledBack && <p>{t('create.run.rolledBack')}</p>}
          {result.rollbackError && <p>{t('create.run.rollbackFailed', { message: result.rollbackError })}</p>}
          <div className="dk-cr-row">
            <Button onClick={onBack}>{t('create.run.backToForm')}</Button>
            {result.id && <Button variant="primary" onClick={() => onOpen(result.id!)}>{t('create.run.openContainer')}</Button>}
          </div>
        </div>
      )}
    </section>
  );
}
