import { useState } from 'react';
import { Icon } from '../ui';
import type { NavEntry } from './useNav';

/** The glyph of a navigation entry: a plugin's own logo when it has one (and it loads), else its icon. */
export function NavIcon({ e }: { e: Pick<NavEntry, 'icon' | 'logo'> }) {
  const [failed, setFailed] = useState('');
  return e.logo && failed !== e.logo
    ? <img className="nav-logo" src={e.logo} alt="" draggable={false} onError={() => setFailed(e.logo!)} />
    : <Icon name={e.icon} />;
}
