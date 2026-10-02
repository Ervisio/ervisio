import { useState } from 'react';
import { NavLink, useNavigate } from 'react-router-dom';
import { useSession } from '../api';
import { useT } from '../i18n';
import { useTheme } from '../theme';
import { Button, Icon, Sheet } from '../ui';
import { NavIcon } from './NavIcon';
import { useNav, type NavEntry } from './useNav';

const DOCK_IDS = ['overview', 'terminal', 'files', 'services'];

export function Dock() {
  const t = useT('shell');
  const { main } = useNav();
  const [more, setMore] = useState(false);
  const items = DOCK_IDS.map((id) => main.find((m) => m.key === id)!).filter(Boolean);
  return (
    <>
      <nav className="dock" aria-label={t('nav.main')}>
        {items.map((e) => (
          <NavLink key={e.key} to={e.to} end={e.end} className={({ isActive }) => `${e.hueClass}${isActive ? ' active' : ''}`}>
            <span className={`pi${e.logo ? ' has-logo' : ''}`}><NavIcon e={e} /></span>
            {e.label}
            {e.badge && <span className="n">{e.badge.count}</span>}
          </NavLink>
        ))}
        <button type="button" onClick={() => setMore(true)} aria-haspopup="dialog">
          <span className="pi"><Icon name="more" /></span>
          {t('nav.more')}
        </button>
      </nav>
      <MoreSheet open={more} onClose={() => setMore(false)} />
    </>
  );
}

export function MoreSheet({ open, onClose }: { open: boolean; onClose(): void }) {
  const t = useT('shell');
  const nav = useNavigate();
  const { host, session, signOut } = useSession();
  const { isDark, toggleDark } = useTheme();
  const { main, settings, plugins } = useNav();
  const tiles: NavEntry[] = [...main, ...plugins, settings];
  return (
    <Sheet open={open} onClose={onClose} title={t('nav.more')}>
      <button
        type="button"
        className="more-hosts"
        onClick={() => {
          onClose();
          nav('/settings#hosts');
        }}
      >
        <i />
        <span style={{ flex: 1 }}>
          <b>{host?.hostname ?? t('host.thisMachine')}</b>
          <small>{t('host.thisMachine')}{host?.ip ? `, ${host.ip}` : ''}</small>
        </span>
        <Icon name="chevron" />
      </button>
      <div className="more-grid">
        {tiles.map((e) => (
          <NavLink key={e.key} to={e.to} end={e.end} onClick={onClose} className={({ isActive }) => `more-tile ${e.hueClass}${isActive ? ' active' : ''}`}>
            <span className={`t${e.logo ? ' has-logo' : ''}`}><NavIcon e={e} /></span>
            <span>{e.label}</span>
            {e.badge && <span className="n">{e.badge.count}</span>}
          </NavLink>
        ))}
      </div>
      <div className="more-row">
        <span className="grow">{session?.user}</span>
        <Button icon={isDark ? 'moon' : 'sun'} onClick={toggleDark}>{isDark ? t('theme.toLight') : t('theme.toDark')}</Button>
        <Button variant="danger" icon="logout" onClick={() => void signOut()}>{t('user.signOut')}</Button>
      </div>
    </Sheet>
  );
}
