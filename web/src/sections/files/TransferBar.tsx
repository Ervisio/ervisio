import { useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import { Button, Icon, IconButton, Progress } from '../../ui';
import { transferActions, useTransfers, type Transfer } from './transfers';

export function TransferBar() {
  const t = useT('files');
  const list = useTransfers();
  if (list.length === 0) return null;
  const running = list.filter((x) => x.status === 'running' || x.status === 'queued').length;
  const done = list.filter((x) => x.status === 'done').length;
  const failed = list.filter((x) => x.status === 'error').length;
  const finished = list.length - running;
  return (
    <section className="files-xfer" aria-label={t('xfer.title')}>
      <div className="files-xfer-hd">
        <b>{t('xfer.title')}</b>
        <span className="files-muted">{[running ? t('xfer.running', { count: running }) : '', done ? t('xfer.done', { count: done }) : '', failed ? t('xfer.failed', { count: failed }) : ''].filter(Boolean).join(', ')}</span>
        {finished > 0 && (
          <Button size="sm" variant="ghost" className="files-xfer-clear" onClick={transferActions.clearFinished}>
            {t('xfer.clear')}
          </Button>
        )}
      </div>
      <div className="files-xfer-list">
        {list.map((x) => (
          <Row key={x.id} x={x} t={t} />
        ))}
      </div>
    </section>
  );
}

function Row({ x, t }: { x: Transfer; t: ReturnType<typeof useT> }) {
  const active = x.status === 'running' || x.status === 'queued';
  const pct = x.total > 0 ? Math.min(100, (Math.max(0, x.done) / x.total) * 100) : x.status === 'done' ? 100 : undefined;
  let state: string;
  if (x.status === 'queued') state = t('xfer.waiting');
  else if (x.status === 'running') state = x.total > 0 ? `${formatBytes(Math.max(0, x.done))} / ${formatBytes(x.total)}${x.speed ? ` · ${formatBytes(x.speed)}/s` : ''}` : t('xfer.working');
  else if (x.status === 'done') state = t('xfer.finished');
  else if (x.status === 'cancelled') state = t('xfer.cancelled');
  else state = x.error ?? t('xfer.error');
  const verb = x.kind === 'upload' ? t('xfer.upload') : x.kind === 'move' ? t('xfer.move') : t('xfer.copy');
  return (
    <div className={`files-xr is-${x.status}`}>
      <span className="files-ic files-ic--sm">
        <Icon name={x.kind === 'upload' ? 'upload' : 'copy'} />
      </span>
      <span className="files-xr-n" title={x.name}>
        <b>{x.name}</b>
        <small title={x.dest}>
          {verb} → {x.dest.split('/').filter(Boolean).pop() ?? '/'}
        </small>
      </span>
      <span className="files-xr-bar">
        <Progress value={pct} tone={x.status === 'error' ? 'err' : x.status === 'done' ? 'ok' : undefined} hue="file" label={x.name} />
      </span>
      <span className={`files-xr-st${x.status === 'error' ? ' is-err' : ''}`}>{state}</span>
      {x.status === 'error' && x.errorCode === 'conflict' && (
        <Button size="sm" onClick={() => transferActions.replace(x.id)}>
          {t('xfer.replace')}
        </Button>
      )}
      {x.status === 'error' && x.errorCode !== 'conflict' && (
        <Button size="sm" onClick={() => transferActions.retry(x.id)}>
          {t('xfer.retry')}
        </Button>
      )}
      {active ? <IconButton icon="close" label={t('xfer.cancel')} onClick={() => transferActions.cancel(x.id)} /> : <IconButton icon="close" label={t('xfer.dismiss')} onClick={() => transferActions.remove(x.id)} />}
    </div>
  );
}
