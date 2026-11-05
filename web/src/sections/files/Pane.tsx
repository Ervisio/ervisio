import { useMemo, type MouseEvent } from 'react';
import { useI18n, useT } from '../../i18n';
import { Icon } from '../../ui';
import type { PaneData } from './data';
import { PaneBar } from './PaneBar';
import { PaneBody } from './PaneBody';
import { buildItemKit } from './PaneItems';
import type { FEntry, PaneState, SortState, View } from './types';
import { usePaneDrop } from './usePaneDrop';
import { usePaneKeys } from './usePaneKeys';
import { usePaneSelection } from './usePaneSelection';
import { isVirtual, sortEntries } from './util';

export type Shortcut =
  | 'delete'
  | 'permDelete'
  | 'rename'
  | 'copy'
  | 'cut'
  | 'paste'
  | 'open'
  | 'up'
  | 'newFolder'
  | 'refresh'
  | 'details';

export interface PaneProps {
  /** "Left" / "Right" chip, only in split view. */
  label?: string;
  active: boolean;
  state: PaneState;
  data: PaneData;
  query: string;
  onQuery(q: string): void;
  sort: SortState;
  onSort(s: SortState): void;
  selected: ReadonlySet<string>;
  onSelected(s: Set<string>): void;
  starred: ReadonlySet<string>;
  onFocus(): void;
  onNavigate(loc: string): void;
  onView(v: View): void;
  onOpen(e: FEntry): void;
  onContext(e: FEntry | null, x: number, y: number): void;
  onDropFiles(dt: DataTransfer, destDir: string): void;
  onDropMove(paths: string[], destDir: string, copy: boolean): void;
  onShortcut(s: Shortcut): void;
  onLeaveAdmin(): void;
  onRetryAdmin(): void;
  /** Items the app can hand out when dragging (selection or the dragged item). */
  dragPaths(e: FEntry): string[];
}

const isBackgroundClick = (ev: MouseEvent) => {
  const cls = (ev.target as HTMLElement).classList;
  return (
    ev.target === ev.currentTarget || cls.contains('files-grid') || cls.contains('files-folds')
  );
};

export function Pane(p: PaneProps) {
  const t = useT('files');
  const { lang } = useI18n();
  const { state, data } = p;
  const loc = state.loc;
  const rows = useMemo(() => sortEntries(data.entries, p.sort), [data.entries, p.sort]);
  const virtual = isVirtual(loc);
  const trash = loc === 'trash:';

  const { anchor, root, pick, moveSelection } = usePaneSelection({ rows, ...p });
  const onKeyDown = usePaneKeys({ rows, moveSelection, ...p });
  const dnd = usePaneDrop({ loc, virtual, trash, ...p });
  const kit = buildItemKit({ trash, lang, t, anchor, pick, ...dnd, ...p });

  const onContentContextMenu = (ev: MouseEvent) => {
    ev.preventDefault();
    p.onFocus();
    p.onSelected(new Set());
    p.onContext(null, ev.clientX, ev.clientY);
  };

  return (
    <div
      ref={root}
      className={`files-pn${p.active ? ' is-act' : ''}${dnd.drop ? ' is-drop' : ''}`}
      tabIndex={0}
      onKeyDown={onKeyDown}
      onMouseDown={() => p.onFocus()}
      onDragOver={dnd.onDragOver}
      onDragLeave={dnd.onDragLeave}
      onDrop={dnd.onDrop}
    >
      <PaneBar {...p} t={t} onUp={() => p.onShortcut('up')} />
      <div
        className="files-content"
        onClick={(ev) => isBackgroundClick(ev) && p.onSelected(new Set())}
        onContextMenu={onContentContextMenu}
      >
        {data.searching && rows.length > 0 && <div className="files-note">{t('searching')}</div>}
        {data.truncated && (
          <div className="files-note">{p.query ? t('searchTruncated') : t('listTruncated')}</div>
        )}
        <PaneBody {...p} rows={rows} kit={kit} pick={pick} />
        {dnd.drop && !virtual && (
          <div className="files-dropmsg">
            <Icon name="upload" />
            <b>{t('dropHere')}</b>
            <small>{loc}</small>
          </div>
        )}
      </div>
    </div>
  );
}
