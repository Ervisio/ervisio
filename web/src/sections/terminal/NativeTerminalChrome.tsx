import { useContext, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useSession } from '../../api';
import { WindowTitlebarSlot } from '../../desktop/AppWindow';
import { useT } from '../../i18n';
import { DropdownMenu, Icon, Menu, type MenuAnchor, type MenuItem } from '../../ui';
import type { Host, SessionInfo } from './types';

interface Props {
  sessions: SessionInfo[];
  detached: SessionInfo[];
  hosts: Host[];
  activeId: string | null;
  split: boolean;
  canSplit: boolean;
  snippets: boolean;
  fontSize: number;
  onShow(id: string): void;
  onNew(): void;
  onRoot(): void;
  onSsh(): void;
  onHost(host: Host): void;
  onDuplicate(session: SessionInfo): void;
  onDetach(id: string): void;
  onRename(session: SessionInfo): void;
  onEnd(session: SessionInfo): void;
  onSessions(): void;
  onSplit(): void;
  onPalette(): void;
  onSnippets(): void;
  onFont(delta: number): void;
  onCopy(): void;
  onPaste(): void;
  onFocus(): void;
  onAppearance(): void;
}

const icons = { local: 'terminal', root: 'shield', ssh: 'server' } as const;

/** Native window chrome over the same bridge-owned PTY sessions as the classic terminal. */
export function NativeTerminalChrome(p: Props) {
  const t = useT('terminal');
  const { session } = useSession();
  const titlebarSlot = useContext(WindowTitlebarSlot);
  const [context, setContext] = useState<{ session: SessionInfo; anchor: MenuAnchor } | null>(null);
  const tabs = useRef<HTMLDivElement>(null);
  useEffect(() => {
    tabs.current
      ?.querySelector('[aria-selected="true"]')
      ?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [p.activeId, titlebarSlot]);
  const canRoot = !!session && (session.isRoot || session.canSudo);
  const newItems: MenuItem[] = [
    { type: 'heading', label: t('native.profiles') },
    {
      id: 'local',
      label: t(session?.os === 'windows' ? 'native.powershell' : 'newShell'),
      icon: 'terminal',
      kbd: 'Ctrl+Shift+T',
      onSelect: p.onNew,
    },
    { id: 'root', label: t('newRoot'), icon: 'shield', disabled: !canRoot, onSelect: p.onRoot },
    { id: 'ssh', label: t('newSsh'), icon: 'server', onSelect: p.onSsh },
    ...(p.hosts.length
      ? [
          { type: 'separator' } as MenuItem,
          { type: 'heading', label: t('savedHosts') } as MenuItem,
          ...p.hosts.map((host): MenuItem => ({
            id: `host:${host.id}`,
            label: host.name,
            icon: 'server',
            onSelect: () => p.onHost(host),
          })),
        ]
      : []),
    ...(p.detached.length
      ? [
          { type: 'separator' } as MenuItem,
          { type: 'heading', label: t('detached') } as MenuItem,
          ...p.detached.map((item): MenuItem => ({
            id: `session:${item.id}`,
            label: item.name,
            icon: icons[item.kind],
            onSelect: () => p.onShow(item.id),
          })),
        ]
      : []),
    { type: 'separator' },
    { id: 'sessions', label: t('sessions'), icon: 'menu', onSelect: p.onSessions },
    {
      id: 'palette',
      label: t('palette.title'),
      icon: 'code',
      kbd: 'Ctrl+Shift+P',
      onSelect: p.onPalette,
    },
  ];
  const actions: MenuItem[] = [
    { id: 'appearance', label: t('native.appearance'), icon: 'palette', onSelect: p.onAppearance },
    { type: 'separator' },
    { id: 'copy', label: t('native.copy'), icon: 'copy', onSelect: p.onCopy },
    { id: 'paste', label: t('native.paste'), icon: 'file', onSelect: p.onPaste },
    { type: 'separator' },
    {
      id: 'snippets',
      label: t(p.snippets ? 'hideSnippets' : 'showSnippets'),
      icon: 'code',
      onSelect: p.onSnippets,
    },
    { id: 'larger', label: t('native.larger'), icon: 'plus', onSelect: () => p.onFont(1) },
    { id: 'smaller', label: t('native.smaller'), icon: 'minus', onSelect: () => p.onFont(-1) },
    { id: 'focus', label: t('focus'), icon: 'overview', onSelect: p.onFocus },
  ];
  const sessionItems = (item: SessionInfo): MenuItem[] => [
    {
      id: 'duplicate',
      label: t('native.duplicate'),
      icon: 'copy',
      disabled: item.kind === 'ssh' && !p.hosts.some((host) => host.name === item.name),
      onSelect: () => p.onDuplicate(item),
    },
    { id: 'rename', label: t('rename'), icon: 'edit', onSelect: () => p.onRename(item) },
    { id: 'detach', label: t('detach'), icon: 'eyeoff', onSelect: () => p.onDetach(item.id) },
    { type: 'separator' },
    { id: 'end', label: t('kill'), icon: 'close', danger: true, onSelect: () => p.onEnd(item) },
  ];
  const tabstrip = (
    <div className="terminal-native-tabs">
      <div ref={tabs} className="terminal-native-tablist" role="tablist" aria-label={t('sessions')}>
        {p.sessions.map((item) => (
          <div
            key={item.id}
            className={`terminal-native-tab terminal-k-${item.kind}${item.id === p.activeId ? ' is-active' : ''}`}
          >
            <button
              type="button"
              role="tab"
              aria-selected={item.id === p.activeId}
              tabIndex={item.id === p.activeId ? 0 : -1}
              className="terminal-native-tab-open"
              onClick={() => p.onShow(item.id)}
              onKeyDown={(event) => {
                const index = p.sessions.findIndex((session) => session.id === item.id);
                const target =
                  event.key === 'ArrowRight'
                    ? (index + 1) % p.sessions.length
                    : event.key === 'ArrowLeft'
                      ? (index + p.sessions.length - 1) % p.sessions.length
                      : event.key === 'Home'
                        ? 0
                        : event.key === 'End'
                          ? p.sessions.length - 1
                          : -1;
                if (target < 0) return;
                event.preventDefault();
                p.onShow(p.sessions[target].id);
                tabs.current?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[target]?.focus();
              }}
              onContextMenu={(event) => {
                event.preventDefault();
                setContext({ session: item, anchor: { x: event.clientX, y: event.clientY } });
              }}
              title={item.title || item.name}
            >
              <Icon name={icons[item.kind]} />
              <span>{item.name}</span>
            </button>
            <DropdownMenu
              items={sessionItems(item)}
              aria-label={t('sessionMenu')}
              trigger={(props) => (
                <button
                  type="button"
                  className="terminal-native-tab-menu"
                  aria-label={t('sessionMenu')}
                  {...props}
                >
                  <Icon name="chevron" />
                </button>
              )}
            />
            <button
              type="button"
              className="terminal-native-tab-close"
              onClick={() => p.onEnd(item)}
              aria-label={t('native.closeTab', { name: item.name })}
              title={t('kill')}
            >
              <Icon name="close" />
            </button>
          </div>
        ))}
      </div>
      <div className="terminal-native-new">
        <button
          type="button"
          onClick={p.onNew}
          aria-label={t('newSession')}
          title={t('newSession')}
        >
          <Icon name="plus" />
        </button>
        <DropdownMenu
          items={newItems}
          aria-label={t('newSessionMenu')}
          trigger={(props) => (
            <button type="button" aria-label={t('newSessionMenu')} {...props}>
              <Icon name="chevron" />
            </button>
          )}
        />
      </div>
    </div>
  );
  return (
    <>
      {titlebarSlot ? createPortal(tabstrip, titlebarSlot) : tabstrip}
      <div className="terminal-native-toolbar">
        <button type="button" onClick={p.onSessions} title={t('sessions')}>
          <Icon name="menu" />
          <span>{t('sessions')}</span>
          {p.detached.length > 0 && <b>{p.detached.length}</b>}
        </button>
        {p.canSplit && (
          <button type="button" aria-pressed={p.split} onClick={p.onSplit} title={t('splitTip')}>
            <Icon name="columns" />
            <span>{t('split')}</span>
          </button>
        )}
        <button type="button" onClick={p.onPalette} title={t('paletteTip')}>
          <Icon name="code" />
          <span>{t('palette.title')}</span>
        </button>
        <span className="terminal-native-spacer" />
        <span className="terminal-native-font">{p.fontSize}px</span>
        <button
          type="button"
          onClick={p.onAppearance}
          aria-label={t('native.appearance')}
          title={t('native.appearance')}
        >
          <Icon name="palette" />
        </button>
        <DropdownMenu
          items={actions}
          aria-label={t('native.options')}
          trigger={(props) => (
            <button
              type="button"
              aria-label={t('native.options')}
              title={t('native.options')}
              {...props}
            >
              <Icon name="more" />
            </button>
          )}
        />
      </div>
      {context && (
        <Menu
          items={sessionItems(context.session)}
          anchor={context.anchor}
          onClose={() => setContext(null)}
          aria-label={t('sessionMenu')}
        />
      )}
    </>
  );
}
