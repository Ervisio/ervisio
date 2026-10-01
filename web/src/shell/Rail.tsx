import { useRef, useState } from 'react';
import { Link, NavLink, useNavigate } from 'react-router-dom';
import { requestUnlock, useSession } from '../api';
import { useT } from '../i18n';
import { Icon, Menu, type MenuItem } from '../ui';
import { formatClock } from '../lib/format';
import { DistroLogo } from './DistroLogo';
import { useNav, type NavEntry } from './useNav';

export function RailItem({ e }: { e: NavEntry }) {
  return (
    <NavLink to={e.to} end={e.end} className={({ isActive }) => `rail-it ${e.hueClass}${isActive ? ' active' : ''}`} aria-label={e.label}>
      <span className="pi"><Icon name={e.icon} /></span>
      <span className="rail-lb">{e.label}</span>
      {e.badge && <span className={`n${e.badge.tone === 'info' ? ' n--info' : ''}`} aria-label={String(e.badge.count)}>{e.badge.count > 99 ? '99+' : e.badge.count}</span>}
    </NavLink>
  );
}

export function Rail() {
  const t = useT('shell');
  const { host, session, isUnlocked, unlockLeft, lock, signOut } = useSession();
  const { main, settings, plugins } = useNav();
  const nav = useNavigate();
  const avRef = useRef<HTMLButtonElement>(null);
  const [anchor, setAnchor] = useState<DOMRect | null>(null);

  const items: MenuItem[] = [
    { type: 'heading', label: t('user.signedInAs', { user: session?.user ?? '' }) },
    isUnlocked
      ? { id: 'lock', label: t('sudo.lockNow', { time: formatClock(unlockLeft) }), icon: 'lock', onSelect: () => void lock() }
      : { id: 'unlock', label: t('sudo.unlock'), icon: 'unlock', disabled: session ? !session.canSudo && !session.isAdmin : false, onSelect: () => void requestUnlock().catch(() => undefined) },
    { id: 'settings', label: t('nav.settings'), icon: 'cog', onSelect: () => nav('/settings') },
    { type: 'separator' },
    { id: 'out', label: t('user.signOut'), icon: 'logout', danger: true, onSelect: () => void signOut() },
  ];

  return (
    <nav className="rail" aria-label={t('nav.main')}>
      <Link to="/" className="rail-distro" aria-label={host?.hostname ?? t('nav.overview')}>
        <span className="lg"><DistroLogo id={host?.distro.id} logo={host?.distro.logo} url={host?.distro.logoUrl} /></span>
        <span className="dn">{host?.hostname ?? ''}</span>
      </Link>
      <div className="rail-list">
        {main.map((e, i) => (
          <span key={e.key} style={{ display: 'contents' }}>
            {e.key === 'plugins' && <div className="rail-gap" />}
            <RailItem e={e} />
            {/* plugin pages live under the Plugins item */}
            {main[i].key === 'plugins' && plugins.map((p) => <RailItem key={p.key} e={p} />)}
          </span>
        ))}
      </div>
      <div className="rail-foot">
        <RailItem e={settings} />
        <button
          ref={avRef}
          type="button"
          className="rail-av"
          aria-label={t('user.menu')}
          aria-haspopup="menu"
          aria-expanded={!!anchor}
          onClick={() => setAnchor(anchor ? null : avRef.current!.getBoundingClientRect())}
        >
          {(session?.user ?? '?').slice(0, 1)}
          {isUnlocked && <span className="sudo-dot" />}
        </button>
      </div>
      {anchor && <AvatarMenu items={items} anchor={anchor} onClose={() => setAnchor(null)} />}
    </nav>
  );
}

function AvatarMenu({ items, anchor, onClose }: { items: MenuItem[]; anchor: DOMRect; onClose(): void }) {
  // Opens to the right of the rail, bottom aligned with the avatar.
  const rect = new DOMRect(anchor.right + 6, anchor.top - 8, 0, 0);
  return <Menu items={items} anchor={{ x: rect.x, y: rect.y }} onClose={onClose} />;
}
