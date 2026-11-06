import { useState, type MouseEvent } from 'react';
import { useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import { Button, Dialog, Icon, Menu, Progress, Skeleton, type MenuItem } from '../../ui';
import type { Disk, Places, Remote, RemoteCapacity } from './types';
import { basename } from './util';
import { networkAddress } from './networkPath';
import './computer-view.css';

interface Props {
  places: Places | null;
  home: string;
  bookmarks: string[];
  remotes: Remote[];
  capacity: Record<string, RemoteCapacity>;
  onGo(path: string): void;
  onRemote(remote: Remote): void;
  onAddRemote(): void;
  onEditRemote(remote: Remote): void;
  onForgetRemote(remote: Remote): void;
  onFormat(disk: Disk): void;
  onRefresh(): void;
  onActivate(): void;
}

type Selected = { kind: 'disk'; disk: Disk } | { kind: 'remote'; remote: Remote };

/** This PC is shared by both shells; every device and byte count comes from the bridge. */
export function ComputerView(p: Props) {
  const t = useT('files');
  const [selected, setSelected] = useState('');
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null);
  const [properties, setProperties] = useState<Selected | null>(null);
  const showMenu = (event: MouseEvent, items: MenuItem[]) => {
    event.preventDefault();
    event.stopPropagation();
    setMenu({ x: event.clientX, y: event.clientY, items });
  };
  const remoteItems = (remote: Remote): MenuItem[] => [
    { id: 'open', label: t('act.open'), icon: 'files', onSelect: () => p.onRemote(remote) },
    ...(remote.kind === 'sftp' || remote.kind === 'smb'
      ? [
          {
            id: 'edit',
            label: t('remote.edit'),
            icon: 'edit' as const,
            onSelect: () => p.onEditRemote(remote),
          },
        ]
      : []),
    {
      id: 'forget',
      label: t('remote.forget'),
      icon: 'close',
      onSelect: () => p.onForgetRemote(remote),
    },
    {
      id: 'properties',
      label: t('act.properties'),
      icon: 'info',
      onSelect: () => setProperties({ kind: 'remote', remote }),
    },
  ];
  const diskItems = (disk: Disk): MenuItem[] => [
    { id: 'open', label: t('act.open'), icon: 'files', onSelect: () => p.onGo(disk.mount) },
    { id: 'format', label: t('computer.format'), icon: 'server', onSelect: () => p.onFormat(disk) },
    {
      id: 'properties',
      label: t('act.properties'),
      icon: 'info',
      onSelect: () => setProperties({ kind: 'disk', disk }),
    },
  ];
  const capacity = properties?.kind === 'remote' ? p.capacity[properties.remote.id] : undefined;
  return (
    <section
      className="files-native-computer files-computer"
      aria-label={t('native.thisPC')}
      onPointerDown={p.onActivate}
      onFocusCapture={p.onActivate}
      onContextMenu={(event) =>
        showMenu(event, [
          { id: 'network', label: t('remote.add'), icon: 'plus', onSelect: p.onAddRemote },
          { id: 'refresh', label: t('act.refresh'), icon: 'refresh', onSelect: p.onRefresh },
        ])
      }
    >
      <header>
        <Icon name="server" />
        <div>
          <h2>{t('native.thisPC')}</h2>
          <p>{t('native.hostFiles')}</p>
        </div>
        <Button icon="plus" onClick={p.onAddRemote}>
          {t('remote.add')}
        </Button>
      </header>
      <h3>{t('folders')}</h3>
      <div className="files-native-quickfolders">
        {[...new Set([p.home, ...p.bookmarks])].map((path) => (
          <button key={path} type="button" onClick={() => p.onGo(path)} title={path}>
            <span className="files-native-folder" aria-hidden="true" />
            <span>
              <b>{path === p.home ? t('place.home') : basename(path)}</b>
              <small>{path}</small>
            </span>
          </button>
        ))}
      </div>
      <h3>{t('native.devices')}</h3>
      {!p.places ? (
        <Skeleton lines={3} />
      ) : (
        <div className="files-native-drives">
          {p.places.disks.map((disk) => (
            <button
              key={`${disk.device}:${disk.mount}`}
              type="button"
              data-computer-card={disk.mount}
              className={selected === disk.mount ? 'is-selected' : ''}
              onClick={(event) => {
                setSelected(disk.mount);
                if (event.detail === 0) p.onGo(disk.mount);
              }}
              onDoubleClick={() => p.onGo(disk.mount)}
              onContextMenu={(event) => {
                setSelected(disk.mount);
                showMenu(event, diskItems(disk));
              }}
            >
              <span className="files-native-drive">
                <Icon name="server" />
              </span>
              <span>
                <b>{t('disk', { mount: disk.mount })}</b>
                <Progress value={disk.total ? (disk.used / disk.total) * 100 : 0} hue="file" />
                <small>
                  {t('native.freeSpace', {
                    free: formatBytes(disk.avail),
                    total: formatBytes(disk.total),
                  })}
                </small>
                <small className="mono">
                  {disk.device} · {disk.fstype}
                </small>
              </span>
            </button>
          ))}
          {p.places.disks.length === 0 && <p className="files-muted">{t('native.noDisks')}</p>}
        </div>
      )}
      <h3>{t('computer.networkFolders')}</h3>
      <div className="files-native-drives files-network-drives">
        {p.remotes.map((remote) => {
          const space = p.capacity[remote.id];
          return (
            <button
              key={remote.id}
              type="button"
              data-remote-drive={remote.id}
              className={selected === remote.id ? 'is-selected' : ''}
              onClick={(event) => {
                setSelected(remote.id);
                if (event.detail === 0) p.onRemote(remote);
              }}
              onDoubleClick={() => p.onRemote(remote)}
              onContextMenu={(event) => {
                setSelected(remote.id);
                showMenu(event, remoteItems(remote));
              }}
            >
              <span className="files-native-drive files-network-drive">
                <Icon name="globe" />
                <small>{remote.kind.toUpperCase()}</small>
              </span>
              <span>
                <b>{remote.name}</b>
                {space?.supported && space.total ? (
                  <>
                    <Progress value={((space.used ?? 0) / space.total) * 100} hue="file" />
                    <small>
                      {t('native.freeSpace', {
                        free: formatBytes(space.avail ?? 0),
                        total: formatBytes(space.total),
                      })}
                    </small>
                  </>
                ) : (
                  <small className="files-drive-status" title={space?.error}>
                    {!space
                      ? t('computer.capacityLoading')
                      : space.error
                        ? t('computer.connectionUnavailable')
                        : t('computer.capacityUnknown')}
                  </small>
                )}
                <small className="mono">
                  {remote.kind === 'smb'
                    ? networkAddress(
                        remote.host ?? '',
                        remote.share ?? '',
                        remote.path,
                        remote.port,
                      )
                    : `${remote.user ? remote.user + '@' : ''}${remote.host ?? remote.name}${remote.path ?? ''}`}
                </small>
              </span>
            </button>
          );
        })}
        {!p.remotes.length && <p className="files-muted">{t('computer.noNetworkFolders')}</p>}
      </div>
      {menu && (
        <Menu
          items={menu.items}
          anchor={{ x: menu.x, y: menu.y }}
          aria-label={t('menu')}
          onClose={() => setMenu(null)}
        />
      )}
      {properties && (
        <Dialog
          open
          title={t('propertiesFor', {
            name: properties.kind === 'disk' ? properties.disk.mount : properties.remote.name,
          })}
          icon="server"
          onClose={() => setProperties(null)}
          className="files-drive-properties"
          footer={<Button onClick={() => setProperties(null)}>{t('edit.close')}</Button>}
        >
          <dl className="files-drive-details">
            {properties.kind === 'disk' ? (
              <>
                <dt>{t('computer.device')}</dt>
                <dd>{properties.disk.device}</dd>
                <dt>{t('computer.mount')}</dt>
                <dd>{properties.disk.mount}</dd>
                <dt>{t('computer.filesystem')}</dt>
                <dd>{properties.disk.fstype}</dd>
                <dt>{t('computer.total')}</dt>
                <dd>{formatBytes(properties.disk.total)}</dd>
                <dt>{t('computer.used')}</dt>
                <dd>{formatBytes(properties.disk.used)}</dd>
                <dt>{t('computer.available')}</dt>
                <dd>{formatBytes(properties.disk.avail)}</dd>
              </>
            ) : (
              <>
                <dt>{t('remote.protocol')}</dt>
                <dd>{properties.remote.kind.toUpperCase()}</dd>
                <dt>{t('remote.host')}</dt>
                <dd>{properties.remote.host}</dd>
                <dt>{t('remote.user')}</dt>
                <dd>{properties.remote.user || '—'}</dd>
                {properties.remote.kind === 'smb' && (
                  <>
                    <dt>{t('computer.share')}</dt>
                    <dd>{properties.remote.share}</dd>
                  </>
                )}
                <dt>{t('remote.startPath')}</dt>
                <dd>{properties.remote.path || '/'}</dd>
                {capacity?.supported ? (
                  <>
                    <dt>{t('computer.total')}</dt>
                    <dd>{formatBytes(capacity.total ?? 0)}</dd>
                    <dt>{t('computer.used')}</dt>
                    <dd>{formatBytes(capacity.used ?? 0)}</dd>
                    <dt>{t('computer.available')}</dt>
                    <dd>{formatBytes(capacity.avail ?? 0)}</dd>
                  </>
                ) : (
                  <>
                    <dt>{t('computer.total')}</dt>
                    <dd>{t('computer.capacityUnknown')}</dd>
                  </>
                )}
                {properties.remote.fingerprint && (
                  <>
                    <dt>{t('remote.trustedKey')}</dt>
                    <dd>{properties.remote.fingerprint}</dd>
                  </>
                )}
              </>
            )}
          </dl>
        </Dialog>
      )}
    </section>
  );
}
