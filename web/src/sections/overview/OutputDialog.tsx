import { useT } from '../../i18n';
import { Badge, Button, Dialog, toast } from '../../ui';
import { showOutput, took, useOutputView } from './actions';

/** "Show output" dialog for the result of an action. Mounted once by the Overview page. */
export function OutputDialog() {
  const t = useT('overview');
  const v = useOutputView();
  const r = v?.result;
  const copy = () => {
    const text = r?.output ?? v?.error ?? '';
    navigator.clipboard?.writeText(text).then(() => toast.ok(t('output.copied')), () => toast.err(t('output.copyFailed')));
  };
  return (
    <Dialog
      open={!!v}
      onClose={() => showOutput(null)}
      size="lg"
      icon="terminal"
      title={v?.label ?? ''}
      description={v ? <span className="ov-mono">$ {v.command}{v.admin ? ` · ${t('action.asAdmin')}` : ''}</span> : undefined}
      footer={
        <>
          <Button variant="ghost" icon="copy" onClick={copy}>{t('output.copy')}</Button>
          <Button variant="primary" onClick={() => showOutput(null)}>{t('close')}</Button>
        </>
      }
    >
      {v && (
        <>
          <div className="ov-out-meta">
            {r ? (
              r.ok ? <Badge tone="ok">{t('output.success')}</Badge> : r.timedOut ? <Badge tone="err">{t('action.timedOutShort')}</Badge> : <Badge tone="err">{t('action.exit', { code: r.exitCode })}</Badge>
            ) : (
              <Badge tone="err">{t('output.notRun')}</Badge>
            )}
            {r && <small>{t('action.took', { time: took(r.durationMs) })}</small>}
            {r?.truncated && <small>{t('output.truncated')}</small>}
          </div>
          <pre className="ov-pre ov-pre--dlg" tabIndex={0}>{r ? r.output || t('output.none') : v.error}</pre>
        </>
      )}
    </Dialog>
  );
}
