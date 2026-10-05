import { useCallback, useState, type ReactNode } from 'react';
import { ApiError, call, useSession } from '../../api';
import { useT } from '../../i18n';
import { ConfirmDialog, toast } from '../../ui';
import { needsConfirm, short } from './helpers';
import type { Action } from './types';

/**
 * Runs unit actions with the confirmation rules (type-to-confirm for units that could
 * cut you off, a plain confirmation for mask) and the toasts.
 * `after` runs once the action succeeded, to refresh lists.
 */
export function useActions(after: (name: string, action: Action) => void): {
  run(name: string, action: Action): void;
  busy: ReadonlySet<string>;
  dialog: ReactNode;
} {
  const t = useT('services');
  const win = useSession().session?.os === 'windows';
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
  const [ask, setAsk] = useState<{ name: string; action: Action; mode: 'typed' | 'simple' } | null>(null);

  const exec = useCallback(
    async (name: string, action: Action) => {
      setBusy((b) => new Set(b).add(name));
      try {
        await call('services.action', { name, action });
        toast.ok(t(`toast.${action}`, { name: short(name) }));
        after(name, action);
      } catch (e) {
        toast.err(t('toast.failed', { name: short(name), action: t(`actions.${action}`).toLowerCase() }), e instanceof ApiError ? e.message : String(e));
        after(name, action);
      } finally {
        setBusy((b) => {
          const n = new Set(b);
          n.delete(name);
          return n;
        });
      }
    },
    [after, t],
  );

  const run = useCallback(
    (name: string, action: Action) => {
      const mode = needsConfirm(action, name);
      if (mode) setAsk({ name, action, mode });
      else void exec(name, action);
    },
    [exec],
  );

  const dialog = (
    <ConfirmDialog
      open={!!ask}
      onClose={() => setAsk(null)}
      onConfirm={() => ask && exec(ask.name, ask.action)}
      title={ask ? t(win && ask.action === 'mask' ? 'win.confirm.mask.title' : `confirm.${ask.action}.title`, { name: short(ask.name) }) : ''}
      description={ask ? t(win && ask.action === 'mask' ? 'win.confirm.mask.text' : `confirm.${ask.action}.text`, { name: short(ask.name) }) : ''}
      confirmLabel={ask ? (win && ask.action === 'mask' ? t('win.mask') : t(`actions.${ask.action}`)) : ''}
      confirmText={ask?.mode === 'typed' ? short(ask.name) : undefined}
      danger
    />
  );
  return { run, busy, dialog };
}
