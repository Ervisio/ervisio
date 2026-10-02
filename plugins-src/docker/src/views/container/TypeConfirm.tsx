import { useEffect, useState, type ReactNode } from 'react';
import { errorText } from '../../api/engine';
import { t } from '../../i18n';
import { Button, Dialog, Input } from '../../kit';

/**
 * Type-to-confirm dialog without a <form>. The plugin frame is sandboxed without allow-forms, so the kit's
 * ConfirmDialog (which submits a form) cannot confirm there. Enter in the field confirms instead.
 */
export function TypeConfirm({ open, onClose, onConfirm, title, description, confirmLabel, confirmText, children }: {
  open: boolean;
  onClose(): void;
  onConfirm(): Promise<unknown>;
  title: ReactNode;
  description?: ReactNode;
  confirmLabel: string;
  confirmText: string;
  children?: ReactNode;
}) {
  const [typed, setTyped] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  useEffect(() => {
    if (open) {
      setTyped('');
      setErr('');
      setBusy(false);
    }
  }, [open]);
  const ok = typed === confirmText;
  const run = async () => {
    if (!ok || busy) return;
    setBusy(true);
    setErr('');
    try {
      await onConfirm();
      onClose();
    } catch (e) {
      setErr(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      title={title}
      description={description}
      icon="trash"
      tone="err"
      role="alertdialog"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>{t('common.cancel')}</Button>
          <Button variant="danger-solid" disabled={!ok} loading={busy} onClick={() => void run()}>{confirmLabel}</Button>
        </>
      }
    >
      <Input
        label={t('container.confirmType', { text: confirmText })}
        mono
        autoFocus
        value={typed}
        onChange={(e) => setTyped(e.target.value)}
        onKeyDown={(e) => { if (e.key === 'Enter') void run(); }}
        error={err || undefined}
        autoComplete="off"
        spellCheck={false}
      />
      {children}
    </Dialog>
  );
}
