import type { ReactNode } from 'react';
import { usePrefs, useSession } from '../api';
import { Icon, type IconName } from './Icon';
import './native-app.css';

/** Native application navigation; the underlying pages/actions stay identical in both shells. */
export function NativeAppFrame<T extends string>({
  title,
  icon,
  items,
  active,
  onSelect,
  children,
}: {
  title: string;
  icon: IconName;
  items: { id: T; label: ReactNode; count?: number | string; icon?: IconName }[];
  active: T;
  onSelect(value: T): void;
  children: ReactNode;
}) {
  const { prefs } = usePrefs();
  const { host } = useSession();
  if ((prefs['shell.mode'] ?? host?.defaultShell) !== 'ervisios') return <>{children}</>;
  return (
    <div className="eos-native-frame">
      <aside className="eos-native-sidebar">
        <div className="eos-native-app-name">
          <Icon name={icon} size={28} />
          <strong>{title}</strong>
        </div>
        <nav aria-label={title}>
          {items.map((item) => (
            <button
              key={item.id}
              aria-label={typeof item.label === 'string' ? item.label : undefined}
              title={typeof item.label === 'string' ? item.label : undefined}
              aria-current={active === item.id ? 'page' : undefined}
              onClick={() => onSelect(item.id)}
            >
              <Icon name={item.icon ?? icon} size={18} />
              <span>{item.label}</span>
              {item.count !== undefined && <small>{item.count}</small>}
            </button>
          ))}
        </nav>
        <span className="eos-native-brand">ErvisiOS</span>
      </aside>
      <div className="eos-native-main">
        <header className="eos-native-heading">
          <Icon name={items.find((item) => item.id === active)?.icon ?? icon} />
          <div>
            <span>{title}</span>
            <h1>{items.find((item) => item.id === active)?.label}</h1>
          </div>
        </header>
        <div className="eos-native-content">{children}</div>
      </div>
    </div>
  );
}
