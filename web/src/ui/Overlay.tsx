import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useT } from '../i18n';
import { ApiError } from '../api/types';
import { Button, IconButton } from './Button';
import { hueClass, type HueId } from './Display';
import { Input } from './Form';
import { Icon, type IconName } from './Icon';
import { useFocusTrap, useIsMobile, useScrollLock } from './hooks';

/* ---------- Dialog ---------- */
export interface DialogProps {
  open: boolean;
  onClose(): void;
  title: ReactNode;
  description?: ReactNode;
  icon?: IconName;
  tone?: 'acc' | 'err' | 'warn';
  children?: ReactNode;
  footer?: ReactNode;
  size?: 'md' | 'lg';
  /** Esc / click outside close the dialog. Default true. */
  dismissable?: boolean;
  role?: 'dialog' | 'alertdialog';
  /** Wrap content in a <form> so Enter submits. */
  onSubmit?: (e: FormEvent<HTMLFormElement>) => void;
}

export function Dialog({ open, onClose, title, description, icon, tone = 'acc', children, footer, size = 'md', dismissable = true, role = 'dialog', onSubmit }: DialogProps) {
  const ref = useRef<HTMLDivElement>(null);
  const tid = useId();
  const did = useId();
  useScrollLock(open);
  useFocusTrap(ref, open, dismissable ? onClose : undefined);
  if (!open) return null;
  const inner = (
    <>
      <div className="ui-dlg-hd">
        {icon && <span className={`ui-dlg-ic ui-dlg-ic--${tone}`}><Icon name={icon} /></span>}
        <div style={{ minWidth: 0 }}>
          <h3 id={tid}>{title}</h3>
          {description && <p id={did}>{description}</p>}
        </div>
      </div>
      {children}
      {footer && <div className="ui-dlg-ft">{footer}</div>}
    </>
  );
  return createPortal(
    <div
      className="ui-scrim"
      onMouseDown={(e) => {
        if (dismissable && e.target === e.currentTarget) onClose();
      }}
    >
      <div ref={ref} className={`ui-dlg${size === 'lg' ? ' ui-dlg--lg' : ''}`} role={role} aria-modal="true" aria-labelledby={tid} aria-describedby={description ? did : undefined}>
        {onSubmit ? (
          <form onSubmit={onSubmit} style={{ display: 'contents' }}>{inner}</form>
        ) : inner}
      </div>
    </div>,
    document.body,
  );
}

/* ---------- ConfirmDialog (type-to-confirm) ---------- */
export interface ConfirmDialogProps {
  open: boolean;
  onClose(): void;
  onConfirm(): unknown;
  title: ReactNode;
  description?: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  /** Red solid confirm button (default true for destructive use). */
  danger?: boolean;
  /** The user must type this text to enable the confirm button. */
  confirmText?: string;
  icon?: IconName;
  children?: ReactNode;
}

export function ConfirmDialog({ open, onClose, onConfirm, title, description, confirmLabel, cancelLabel, danger = true, confirmText, icon, children }: ConfirmDialogProps) {
  const t = useT('ui');
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
  const ok = !confirmText || typed === confirmText;
  const run = async (e?: FormEvent) => {
    e?.preventDefault();
    if (!ok || busy) return;
    setBusy(true);
    setErr('');
    try {
      await onConfirm();
      onClose();
    } catch (x) {
      setErr(x instanceof Error ? x.message : String(x));
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
      icon={icon ?? (danger ? 'trash' : 'alert')}
      tone={danger ? 'err' : 'acc'}
      role="alertdialog"
      onSubmit={run}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>{cancelLabel ?? t('cancel')}</Button>
          <Button type="submit" variant={danger ? 'danger-solid' : 'primary'} disabled={!ok} loading={busy}>{confirmLabel}</Button>
        </>
      }
    >
      {confirmText && (
        <Input
          data-autofocus
          label={<>{t('typeToConfirm.before')} <b>{confirmText}</b> {t('typeToConfirm.after')}</>}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          autoComplete="off"
          spellCheck={false}
        />
      )}
      {children}
      {err && <div className="ui-form-err" role="alert"><Icon name="alert" />{err}</div>}
    </Dialog>
  );
}

/* ---------- UnlockDialog ---------- */
export function UnlockDialog({ open, user, minutes = 5, reason, onSubmit, onCancel }: {
  open: boolean;
  user?: string;
  minutes?: number;
  /** What needed admin rights, e.g. "Restarting docker". Optional. */
  reason?: string;
  onSubmit(password: string): Promise<void>;
  onCancel(): void;
}) {
  const t = useT('ui');
  const [pw, setPw] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  useEffect(() => {
    if (open) {
      setPw('');
      setErr('');
      setBusy(false);
    }
  }, [open]);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!pw || busy) return;
    setBusy(true);
    setErr('');
    try {
      await onSubmit(pw);
    } catch (x) {
      const code = x instanceof ApiError ? x.code : '';
      setErr(code === 'forbidden' ? t('unlock.forbidden') : code === 'unauthenticated' || code === 'invalid' ? t('unlock.wrong') : x instanceof Error ? x.message : String(x));
      setPw('');
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onCancel}
      title={t('unlock.title')}
      description={t(reason ? 'unlock.bodyFor' : 'unlock.body', { reason: reason ?? '', user: user ?? '', minutes })}
      icon="lock"
      onSubmit={submit}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>{t('cancel')}</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!pw}>{t('unlock.submit')}</Button>
        </>
      }
    >
      <Input data-autofocus icon="shield" type="password" autoComplete="current-password" aria-label={t('unlock.password')} placeholder={t('unlock.password')} value={pw} onChange={(e) => setPw(e.target.value)} />
      {err && <div className="ui-form-err" role="alert"><Icon name="alert" />{err}</div>}
    </Dialog>
  );
}

/* ---------- Sheet (bottom sheet) ---------- */
export function Sheet({ open, onClose, title, children }: { open: boolean; onClose(): void; title?: ReactNode; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const tid = useId();
  useScrollLock(open);
  useFocusTrap(ref, open, onClose);
  if (!open) return null;
  return createPortal(
    <>
      <div className="ui-sheet-wrap" onMouseDown={onClose} />
      <div ref={ref} className="ui-sheet" role="dialog" aria-modal="true" aria-labelledby={title ? tid : undefined}>
        <div className="ui-grab" aria-hidden="true" />
        {title && <h3 id={tid}>{title}</h3>}
        <div className="ui-sheet-body">{children}</div>
      </div>
    </>,
    document.body,
  );
}

/* ---------- Panel: right side panel on desktop, bottom sheet on phones ---------- */
export interface PanelProps {
  open: boolean;
  onClose(): void;
  title: ReactNode;
  subtitle?: ReactNode;
  icon?: IconName;
  hue?: HueId;
  /** A <Tabs/> element shown under the header. */
  tabs?: ReactNode;
  footer?: ReactNode;
  children: ReactNode;
  /** 640px instead of 420px. */
  wide?: boolean;
  /** Render in the normal flow (as a flex child) instead of floating over the page. */
  inline?: boolean;
}

export function Panel({ open, onClose, title, subtitle, icon, hue, tabs, footer, children, wide, inline }: PanelProps) {
  const t = useT('ui');
  const mobile = useIsMobile();
  const ref = useRef<HTMLElement>(null);
  const tid = useId();
  useEffect(() => {
    if (!open || mobile) return;
    const on = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !document.querySelector('.ui-scrim,.ui-menu')) onClose();
    };
    document.addEventListener('keydown', on);
    return () => document.removeEventListener('keydown', on);
  }, [open, mobile, onClose]);
  if (!open) return null;

  const header = (
    <div className="ui-panel-hd">
      {icon && <span className="ui-panel-ic"><Icon name={icon} /></span>}
      <div style={{ minWidth: 0 }}>
        <h3 id={tid}>{title}</h3>
        {subtitle && <div className="ui-panel-sub">{subtitle}</div>}
      </div>
      <span className="ui-panel-x"><IconButton icon="close" label={t('close')} onClick={onClose} /></span>
    </div>
  );
  const body = (
    <>
      {tabs && <div className="ui-panel-tabs">{tabs}</div>}
      <div className="ui-panel-body">{children}</div>
      {footer && <div className="ui-panel-ft">{footer}</div>}
    </>
  );
  if (mobile) {
    return (
      <Sheet open onClose={onClose}>
        <div className={hueClass(hue)} style={{ display: 'contents' }}>
          {header}
          {tabs && <div>{tabs}</div>}
          <div className="ui-panel-body" style={{ padding: 0 }}>{children}</div>
          {footer && <div className="ui-panel-ft" style={{ padding: 0 }}>{footer}</div>}
        </div>
      </Sheet>
    );
  }
  return (
    <aside ref={ref} className={`ui-panel ${inline ? '' : 'ui-panel--float'} ${wide ? 'ui-panel--lg' : ''} ${hueClass(hue)}`} aria-labelledby={tid}>
      {header}
      {body}
    </aside>
  );
}
