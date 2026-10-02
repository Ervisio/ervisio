import { useEffect, useState, type ReactNode } from 'react';
import { t } from '../../i18n';
import { Button, Dialog, Input } from '../../kit';

/**
 * Confirmation dialog with an optional type-to-confirm field. It does not use the kit's ConfirmDialog: that one
 * confirms through a form submit, and plugin frames are sandboxed without allow-forms, so a submit never fires there.
 * This one confirms through plain click and Enter handlers.
 */
export function Confirm({ open, onClose, onConfirm, title, description, confirmLabel, danger = true, confirmText, icon, children }: {
  open: boolean;
  onClose(): void;
  onConfirm(): unknown;
  title: ReactNode;
  description?: ReactNode;
  confirmLabel: string;
  danger?: boolean;
  confirmText?: string;
  icon?: string;
  children?: ReactNode;
}) {
  const [typed, setTyped] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (open) {
      setTyped('');
      setBusy(false);
    }
  }, [open]);
  const ok = !confirmText || typed === confirmText;
  const run = async () => {
    if (!ok || busy) return;
    setBusy(true);
    try {
      await onConfirm();
    } finally {
      setBusy(false);
      onClose();
    }
  };
  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      title={title}
      description={description}
      icon={icon ?? (danger ? 'trash' : 'alert')}
      tone={danger ? 'err' : 'acc'}
      role="alertdialog"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>{t('common.cancel')}</Button>
          <Button variant={danger ? 'danger-solid' : 'primary'} disabled={!ok} loading={busy} onClick={() => void run()}>{confirmLabel}</Button>
        </>
      }
    >
      {children}
      {confirmText && (
        <Input
          label={t('stacks.typeToConfirm', { text: confirmText })}
          value={typed}
          mono
          autoFocus
          autoComplete="off"
          onChange={(e) => setTyped(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void run(); } }}
        />
      )}
    </Dialog>
  );
}
