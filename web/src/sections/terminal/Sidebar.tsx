import { useState } from 'react';
import { useSession } from '../../api';
import { useT } from '../../i18n';
import { formatDuration } from '../../lib/format';
import { Button, DropdownMenu, Icon, IconButton, Menu, useContextMenu, type MenuAnchor, type MenuItem } from '../../ui';
import type { Host, SessionInfo } from './types';

const KIND_ICON = { local: 'terminal', root: 'lock', ssh: 'server' } as const;

interface Props {
  open: SessionInfo[];
  detached: SessionInfo[];
  hosts: Host[];
  /** session id -> pane number (1 or 2) when shown in split view */
  paneOf: Record<string, number>;
  activeId: string | null;
  onNew(): void;
  onNewRoot(): void;
  onNewSsh(): void;
  onOpen(s: SessionInfo): void;
  onDetach(s: SessionInfo): void;
  onRename(s: SessionInfo): void;
  onKill(s: SessionInfo): void;
  onConnect(h: Host): void;
  onEditHost(h: Host | null): void;
  onDeleteHost(h: Host): void;
  /** hosts that have an open ssh session */
  connectedHosts: Set<string>;
}

function SessionRow({ s, active, st, small, detached, onClick, items }: { s: SessionInfo; active?: boolean; st?: string; small: string; detached?: boolean; onClick(): void; items: MenuItem[] }) {
  const t = useT('terminal');
  const cm = useContextMenu(() => items);
  const [more, setMore] = useState<MenuAnchor | null>(null);
  return (
    <>
      <div
        className={`terminal-ss terminal-k-${s.kind}${active ? ' on' : ''}${detached ? ' det' : ''}`}
        role="button"
        tabIndex={0}
        onClick={onClick}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onClick();
          }
        }}
        {...cm.bind}
      >
        <span className="terminal-ic"><Icon name={detached ? 'clock' : KIND_ICON[s.kind]} /></span>
        <div className="terminal-ss-txt">
          <b>{s.name}</b>
          <small>{small}</small>
        </div>
        {st && <span className="terminal-st">{st}</span>}
        <span className="terminal-more">
          <IconButton
            icon="more"
            label={t('sessionMenu')}
            size="sm"
            variant="ghost"
            onClick={(e) => {
              e.stopPropagation();
              setMore({ rect: e.currentTarget.getBoundingClientRect() });
            }}
          />
        </span>
      </div>
      {cm.menu}
      {more && <Menu items={items} anchor={more} onClose={() => setMore(null)} />}
    </>
  );
}

function HostRow({ h, connected, onConnect, onEdit, onDelete }: { h: Host; connected: boolean; onConnect(): void; onEdit(): void; onDelete(): void }) {
  const t = useT('terminal');
  const items: MenuItem[] = [
    { id: 'c', label: t('connect'), icon: 'play', onSelect: onConnect },
    { id: 'e', label: t('editHost'), icon: 'edit', onSelect: onEdit },
    { id: 'd', label: t('deleteHost'), icon: 'trash', danger: true, onSelect: onDelete },
  ];
  const cm = useContextMenu(() => items);
  const addr = `${h.user ? h.user + '@' : ''}${h.host}${h.port ? ':' + h.port : ''}`;
  return (
    <>
      <div className="terminal-ss terminal-k-ssh" role="button" tabIndex={0} onClick={onConnect} onKeyDown={(e) => (e.key === 'Enter' ? onConnect() : undefined)} {...cm.bind}>
        <span className="terminal-ic"><Icon name="server" /></span>
        <div className="terminal-ss-txt">
          <b>{h.name}</b>
          <small>{connected ? t('sshConnected', { addr }) : t('sshNotConnected', { addr })}</small>
        </div>
        <span className="terminal-more">
          <DropdownMenu items={items} aria-label={t('hostMenu')} trigger={(p) => <IconButton icon="more" label={t('hostMenu')} size="sm" variant="ghost" {...p} onClick={(e) => { e.stopPropagation(); p.onClick(e); }} />} />
        </span>
      </div>
      {cm.menu}
    </>
  );
}

export default function Sidebar(p: Props) {
  const t = useT('terminal');
  const { session } = useSession();
  const canRoot = !!session && (session.isRoot || session.canSudo);
  const newItems: MenuItem[] = [
    { id: 'l', label: t('newShell'), icon: 'terminal', onSelect: p.onNew },
    { id: 'r', label: t('newRoot'), icon: 'lock', disabled: !canRoot, onSelect: p.onNewRoot },
    { id: 's', label: t('newSsh'), icon: 'server', onSelect: p.onNewSsh },
  ];
  const menu = (s: SessionInfo, isOpen: boolean): MenuItem[] => [
    ...(isOpen ? [{ id: 'd', label: t('detach'), icon: 'eyeoff', onSelect: () => p.onDetach(s) } as MenuItem] : [{ id: 'o', label: t('openSession'), icon: 'eye', onSelect: () => p.onOpen(s) } as MenuItem]),
    { id: 'r', label: t('rename'), icon: 'edit', onSelect: () => p.onRename(s) },
    { type: 'separator' },
    { id: 'k', label: t('kill'), icon: 'trash', danger: true, onSelect: () => p.onKill(s) },
  ];
  const small = (s: SessionInfo) => {
    if (s.kind === 'root') return t('rootShell');
    if (s.kind === 'ssh') return t('sshSmall', { host: s.title && s.title !== 'ssh' ? s.title : s.name });
    const raw = s.title || s.name;
    const home = session?.home;
    return home && home !== '/' ? raw.split(home).join('~') : raw;
  };
  return (
    <aside className="terminal-sess" aria-label={t('sessions')}>
      <div className="terminal-newrow">
        <Button variant="primary" icon="plus" className="terminal-newb" onClick={p.onNew}>
          {t('newSession')}
        </Button>
        <DropdownMenu items={newItems} aria-label={t('newSessionMenu')} trigger={(tp) => <Button variant="primary" iconOnly icon="chevron" aria-label={t('newSessionMenu')} {...tp} />} />
      </div>
      <div className="terminal-grp">{t('open')}</div>
      {p.open.length === 0 && <div className="terminal-none">{t('noneOpen')}</div>}
      {p.open.map((s) => (
        <SessionRow key={s.id} s={s} active={s.id === p.activeId} st={p.paneOf[s.id] ? t('paneN', { n: p.paneOf[s.id] }) : undefined} small={small(s)} onClick={() => p.onOpen(s)} items={menu(s, true)} />
      ))}
      {p.detached.length > 0 && <div className="terminal-grp">{t('detached')}</div>}
      {p.detached.map((s) => (
        <SessionRow key={s.id} s={s} detached st={formatDuration((Date.now() - s.createdAt) / 1000).split(' ')[0]} small={s.attached ? t('inUse') : t('detachedSmall')} onClick={() => p.onOpen(s)} items={menu(s, false)} />
      ))}
      <div className="terminal-grp">{t('savedHosts')}</div>
      {p.hosts.map((h) => (
        <HostRow key={h.id} h={h} connected={p.connectedHosts.has(h.name)} onConnect={() => p.onConnect(h)} onEdit={() => p.onEditHost(h)} onDelete={() => p.onDeleteHost(h)} />
      ))}
      <button type="button" className="terminal-addhost" onClick={() => p.onEditHost(null)}>
        <Icon name="plus" />
        {t('addHost')}
      </button>
    </aside>
  );
}
