import { useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import { Badge, DropdownMenu, Button, Icon, Progress, toast, type IconName, type MenuItem } from '../../ui';
import type { Disk, Places as PlacesData, Remote } from './types';
import { basename } from './util';

export interface PlacesProps {
  data: PlacesData | null;
  home: string;
  loc: string;
  bookmarks: string[];
  remotes: Remote[];
  onGo(loc: string): void;
  onRemoveBookmark(path: string): void;
  onNew: MenuItem[];
  /** Called after an item was chosen (closes the phone sheet). */
  onPicked?(): void;
}

export function PlacesList(p: PlacesProps) {
  const t = useT('files');
  const go = (l: string) => {
    p.onGo(l);
    p.onPicked?.();
  };
  const item = (loc: string, icon: IconName, label: string, extra?: React.ReactNode) => (
    <a
      key={loc}
      href="#"
      className={p.loc === loc ? 'is-on' : ''}
      aria-current={p.loc === loc ? 'page' : undefined}
      onClick={(e) => {
        e.preventDefault();
        go(loc);
      }}
    >
      <Icon name={icon} />
      <span className="files-pl-n">{label}</span>
      {extra}
    </a>
  );
  const root = (p.data?.disks ?? []).find((d) => d.mount === '/') ?? p.data?.disks[0];
  return (
    <>
      <DropdownMenu items={p.onNew} aria-label={t('new.label')} trigger={(tp) => (
        <Button variant="primary" icon="plus" className="files-newb" {...tp}>
          {t('new.label')}
        </Button>
      )} />
      <div className="files-pg">
        {item(p.home, 'home', t('place.home'))}
        {item('/', 'server', t('place.root'), <span className="files-muted mono">/</span>)}
        {item('recent:', 'clock', t('place.recent'))}
        {item('starred:', 'star', t('place.starred'))}
        {item('trash:', 'trash', t('place.trash'), p.data && p.data.trashCount > 0 ? <Badge>{p.data.trashCount}</Badge> : undefined)}
      </div>
      <div className="files-plt">{t('bookmarks')}</div>
      <div className="files-pg">
        {p.bookmarks.length === 0 && <p className="files-muted files-pl-empty">{t('bookmarksEmpty')}</p>}
        {p.bookmarks.map((b) => (
          <a
            key={b}
            href="#"
            className={p.loc === b ? 'is-on' : ''}
            onClick={(e) => {
              e.preventDefault();
              go(b);
            }}
            title={b}
          >
            <Icon name="files" />
            <span className="files-pl-n mono">{b.length > 24 ? basename(b) : b}</span>
            <button
              type="button"
              className="files-pl-x"
              aria-label={t('removeBookmark', { name: basename(b) })}
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                p.onRemoveBookmark(b);
              }}
            >
              <Icon name="close" />
            </button>
          </a>
        ))}
      </div>
      <div className="files-plt">{t('remote.title')}</div>
      <div className="files-pg">
        {p.remotes.map((r) => (
          <a
            key={r.name}
            href="#"
            onClick={(e) => {
              e.preventDefault();
              toast.info(t('remote.soon'), t('remote.soonHint'));
            }}
          >
            <Icon name="server" />
            <span className="files-pl-n">{r.name}</span>
            <span className="files-sftp">SFTP</span>
          </a>
        ))}
        <a
          href="#"
          onClick={(e) => {
            e.preventDefault();
            toast.info(t('remote.soon'), t('remote.soonHint'));
          }}
        >
          <Icon name="plus" />
          <span className="files-pl-n">{t('remote.add')}</span>
        </a>
      </div>
      <div className="files-disks">
        {(p.data?.disks ?? []).map((d) => (
          <DiskBox key={d.device} d={d} main={d === root} onGo={go} t={t} />
        ))}
      </div>
    </>
  );
}

function DiskBox({ d, onGo, t }: { d: Disk; main: boolean; onGo(l: string): void; t: ReturnType<typeof useT> }) {
  const pct = d.total ? (d.used / d.total) * 100 : 0;
  return (
    <button type="button" className="files-store" onClick={() => onGo(d.mount)} title={d.device}>
      <span className="files-store-l">
        <b>{t('disk', { mount: d.mount })}</b>
        <span>{t('diskUsage', { used: formatBytes(d.used, 0), total: formatBytes(d.total, 0) })}</span>
      </span>
      <Progress value={pct} hue="file" tone={pct > 90 ? 'err' : pct > 80 ? 'warn' : undefined} label={t('disk', { mount: d.mount })} />
    </button>
  );
}
