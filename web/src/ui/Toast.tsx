import { useEffect, useSyncExternalStore } from 'react';
import { useT } from '../i18n';
import { Icon, type IconName } from './Icon';
import { Progress } from './Display';

export interface ToastInput {
  title: string;
  detail?: string;
  tone?: 'ok' | 'err' | 'info' | 'run';
  icon?: IconName;
  action?: { label: string; onClick(): void };
  /** ms; 0 keeps it until dismissed. Default 4000 (errors 8000, run 0). */
  duration?: number;
  /** 0-100 for tone 'run'; undefined = indeterminate. */
  progress?: number;
}
interface ToastItem extends ToastInput {
  id: number;
}

let items: ToastItem[] = [];
let seq = 1;
const subs = new Set<() => void>();
const timers = new Map<number, number>();
const emit = () => {
  items = [...items];
  subs.forEach((s) => s());
};

function schedule(it: ToastItem) {
  window.clearTimeout(timers.get(it.id));
  const d = it.duration ?? (it.tone === 'err' ? 8000 : it.tone === 'run' ? 0 : 4000);
  if (d > 0) timers.set(it.id, window.setTimeout(() => toast.dismiss(it.id), d));
}

export const toast = {
  show(t: ToastInput): number {
    const it: ToastItem = { ...t, id: seq++ };
    items = [...items.slice(-4), it];
    schedule(it);
    emit();
    return it.id;
  },
  ok: (title: string, detail?: string) => toast.show({ title, detail, tone: 'ok' }),
  err: (title: string, detail?: string) => toast.show({ title, detail, tone: 'err' }),
  info: (title: string, detail?: string) => toast.show({ title, detail, tone: 'info' }),
  /** "Saved" toast with an Undo action. `undoLabel` comes from your own i18n. */
  undo(title: string, undoLabel: string, onUndo: () => void): number {
    const id = toast.show({ title, tone: 'ok', duration: 7000, action: { label: undoLabel, onClick: onUndo } });
    return id;
  },
  update(id: number, patch: Partial<ToastInput>) {
    const it = items.find((x) => x.id === id);
    if (!it) return;
    Object.assign(it, patch);
    schedule(it);
    emit();
  },
  dismiss(id: number) {
    window.clearTimeout(timers.get(id));
    timers.delete(id);
    items = items.filter((x) => x.id !== id);
    emit();
  },
};

/** Hook form: `const toast = useToast()` gives the same object. */
export const useToast = () => toast;

const ICONS: Record<string, IconName> = { ok: 'check', err: 'alert', info: 'info', run: 'download' };

export function ToastViewport() {
  const t = useT('ui');
  const list = useSyncExternalStore(
    (cb) => (subs.add(cb), () => void subs.delete(cb)),
    () => items,
  );
  useEffect(() => () => timers.forEach((x) => window.clearTimeout(x)), []);
  return (
    <div className="ui-toasts" role="region" aria-label={t('notifications')} aria-live="polite">
      {list.map((it) => (
        <div key={it.id} className={`ui-toast ui-toast--${it.tone ?? 'ok'}`} role={it.tone === 'err' ? 'alert' : 'status'}>
          <span className="ui-toast-ic"><Icon name={it.icon ?? ICONS[it.tone ?? 'ok']} /></span>
          <div className="ui-toast-tp">
            {it.title}
            {it.detail && <small>{it.detail}</small>}
            {it.tone === 'run' && <Progress value={it.progress} />}
          </div>
          {it.action && (
            <button
              type="button"
              className="ui-toast-act"
              onClick={() => {
                it.action!.onClick();
                toast.dismiss(it.id);
              }}
            >
              {it.action.label}
            </button>
          )}
          <button type="button" className="ui-toast-x" aria-label={t('dismiss')} onClick={() => toast.dismiss(it.id)}>
            <Icon name="close" />
          </button>
        </div>
      ))}
    </div>
  );
}
