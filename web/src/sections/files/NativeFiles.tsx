import { useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import {
  Button,
  DropdownMenu,
  Icon,
  IconButton,
  Progress,
  registerIcon,
  Skeleton,
  type MenuItem,
} from '../../ui';
import type { Shortcut } from './Pane';
import type { FEntry, Places } from './types';
import { basename } from './util';

registerIcon(
  'files-cut',
  '<circle cx="6" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M8.5 7.5L21 21M8.5 16.5L21 3M11 12l3 3"/>',
);
registerIcon(
  'files-paste',
  '<rect x="5" y="5" width="14" height="16" rx="2"/><rect x="8" y="3" width="8" height="4" rx="1"/><path d="M9 12h6M9 16h6"/>',
);

interface CommandBarProps {
  targets: FEntry[];
  writable: boolean;
  trash: boolean;
  clipboard: boolean;
  computer: boolean;
  newItems: MenuItem[];
  onShortcut(key: Shortcut): void;
  onRestore(): void;
  onEmptyTrash(): void;
  onUpload(): void;
}

/** Existing file actions, presented as an Explorer command bar. */
export function NativeFilesCommandBar(p: CommandBarProps) {
  const t = useT('files');
  const selected = p.targets.length > 0 && !p.computer;
  return (
    <div className="files-native-commands" role="toolbar" aria-label={t('menu')}>
      <DropdownMenu
        items={p.newItems}
        aria-label={t('new.label')}
        trigger={(tp) => (
          <Button icon="plus" disabled={!p.writable} {...tp}>
            {t('new.label')}
          </Button>
        )}
      />
      <span className="files-native-sep" />
      {p.trash ? (
        <>
          <Button icon="undo" disabled={!selected} onClick={p.onRestore}>
            {t('act.restore')}
          </Button>
          <Button icon="trash" disabled={p.computer} onClick={p.onEmptyTrash}>
            {t('act.emptyTrash')}
          </Button>
        </>
      ) : (
        <>
          <IconButton
            icon="files-cut"
            label={t('act.cut')}
            disabled={!selected}
            onClick={() => p.onShortcut('cut')}
          />
          <IconButton
            icon="copy"
            label={t('act.copy')}
            disabled={!selected}
            onClick={() => p.onShortcut('copy')}
          />
          <IconButton
            icon="files-paste"
            label={t('act.paste')}
            disabled={!p.writable || !p.clipboard}
            onClick={() => p.onShortcut('paste')}
          />
          <IconButton
            icon="edit"
            label={t('act.rename')}
            disabled={!selected || p.targets.length !== 1}
            onClick={() => p.onShortcut('rename')}
          />
        </>
      )}
      <IconButton
        icon="trash"
        label={t(p.trash ? 'act.deleteForever' : 'act.trash')}
        disabled={!selected}
        onClick={() => p.onShortcut('delete')}
      />
      <span className="files-native-sep" />
      <Button icon="upload" disabled={!p.writable} onClick={p.onUpload}>
        {t('new.upload')}
      </Button>
      <IconButton
        icon="refresh"
        label={t('native.refresh')}
        onClick={() => p.onShortcut('refresh')}
      />
      <span className="files-sp" />
      <IconButton
        icon="info"
        label={t('act.details')}
        disabled={!selected}
        onClick={() => p.onShortcut('details')}
      />
    </div>
  );
}

/** Actual bridge-reported folders and disks; never invents Windows drives or user directories. */
export function NativeComputer({
  places,
  home,
  bookmarks,
  onGo,
}: {
  places: Places | null;
  home: string;
  bookmarks: string[];
  onGo(path: string): void;
}) {
  const t = useT('files');
  const folders = [...new Set([home, ...bookmarks])];
  return (
    <section className="files-native-computer" aria-label={t('native.thisPC')}>
      <header>
        <Icon name="server" />
        <div>
          <h2>{t('native.thisPC')}</h2>
          <p>{t('native.hostFiles')}</p>
        </div>
      </header>
      <h3>{t('folders')}</h3>
      <div className="files-native-quickfolders">
        {folders.map((path) => (
          <button key={path} type="button" onClick={() => onGo(path)} title={path}>
            <span className="files-native-folder" aria-hidden="true" />
            <span>
              <b>{path === home ? t('place.home') : basename(path)}</b>
              <small>{path}</small>
            </span>
          </button>
        ))}
      </div>
      <h3>{t('native.devices')}</h3>
      {!places ? (
        <Skeleton lines={3} />
      ) : (
        <div className="files-native-drives">
          {places.disks.map((disk) => (
            <button
              key={`${disk.device}:${disk.mount}`}
              type="button"
              onClick={() => onGo(disk.mount)}
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
          {places.disks.length === 0 && <p className="files-muted">{t('native.noDisks')}</p>}
        </div>
      )}
    </section>
  );
}
