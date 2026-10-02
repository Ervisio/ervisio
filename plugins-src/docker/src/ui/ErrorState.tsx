import type { ErrorInfo } from '../api/engine';
import { t } from '../i18n';
import { Button, EmptyState } from '../kit';

/** Clean empty state for a failed Docker connection or permission problem. */
export function ErrorState({ error, onRetry }: { error: ErrorInfo; onRetry?(): void }) {
  const k = error.kind === 'unknown' ? 'unknown' : error.kind;
  const text = k === 'engine' || k === 'unknown' ? t('error.detail', { message: error.message }) : `${t(`error.${k}.text`)}`;
  return (
    <EmptyState
      icon="alert"
      hue="svc"
      title={t(`error.${k}.title`)}
      text={text}
      action={onRetry && <Button variant="primary" icon="refresh" onClick={onRetry}>{t('common.retry')}</Button>}
    />
  );
}
