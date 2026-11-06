import { useT } from '../../i18n';
import { Badge, EmptyState, Progress } from '../../ui';
import type { TxState } from './tx';

/** A stable place to return to after dismissing the transient side panel. */
export function TxActivity({tx}:{tx:TxState}) {
  const t=useT('software');
  if(tx.phase==='idle') return <EmptyState icon="download" hue="sw" title={t('activity.empty')} text={t('activity.hint')} />;
  const title=tx.title?t(`tx.title.${tx.title.key}`,tx.title.vars):t('tx.working');
  return <section className="sw-task-activity" aria-label={t('tab.activity')}>
    <header><div><h3>{title}</h3><p>{t('activity.background')}</p></div><Badge tone={tx.phase==='failed'?'err':tx.phase==='done'?'ok':'info'}>{t(`activity.${tx.phase}`)}</Badge></header>
    <Progress label={title} value={tx.phase==='done'?100:tx.phase==='running'?tx.percent:undefined} tone={tx.phase==='failed'?'err':tx.phase==='done'?'ok':undefined} hue="sw" />
    {tx.current&&tx.phase==='running'&&<p className="sw-mono">{tx.current}</p>}
    <pre className="sw-log" tabIndex={0} aria-label={t('tx.log')}>{tx.log.join('\n')}</pre>
    {tx.message&&<p role={tx.phase==='failed'?'alert':undefined} className={tx.phase==='failed'?'sw-read-error':'sw-muted'}>{tx.message}</p>}
  </section>;
}
