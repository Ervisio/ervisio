import { useId, type KeyboardEvent, type ReactNode } from 'react';
import { Icon, type IconName } from './Icon';
import { hueClass, type HueId } from './Display';

export interface TabItem<T extends string = string> {
  id: T;
  label: ReactNode;
  icon?: IconName;
  count?: number | string;
}

/** Tab list. Render your own panels; use `panelId(id)` on the content for aria-controls if you want it. */
export function Tabs<T extends string>({ items, value, onChange, variant = 'underline', hue, 'aria-label': ariaLabel, className }: {
  items: TabItem<T>[];
  value: T;
  onChange(id: T): void;
  variant?: 'pill' | 'underline';
  hue?: HueId;
  'aria-label'?: string;
  className?: string;
}) {
  const uid = useId();
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const i = items.findIndex((t) => t.id === value);
    let n = -1;
    if (e.key === 'ArrowRight') n = (i + 1) % items.length;
    else if (e.key === 'ArrowLeft') n = (i - 1 + items.length) % items.length;
    else if (e.key === 'Home') n = 0;
    else if (e.key === 'End') n = items.length - 1;
    if (n >= 0) {
      e.preventDefault();
      onChange(items[n].id);
      (e.currentTarget.children[n] as HTMLElement | undefined)?.focus();
    }
  };
  return (
    <div className={`ui-tabs ui-tabs--${variant} ${hueClass(hue)} ${className ?? ''}`} role="tablist" aria-label={ariaLabel} onKeyDown={onKey}>
      {items.map((t) => (
        <button
          key={t.id}
          type="button"
          role="tab"
          id={`${uid}-${t.id}`}
          aria-selected={t.id === value}
          tabIndex={t.id === value ? 0 : -1}
          className="ui-tab"
          onClick={() => onChange(t.id)}
        >
          {t.icon && <Icon name={t.icon} />}
          {t.label}
          {t.count !== undefined && <span className="ui-tab-n">{t.count}</span>}
        </button>
      ))}
    </div>
  );
}
