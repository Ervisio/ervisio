import type { DragEvent, MouseEvent, MutableRefObject, ReactNode } from 'react';
import { InlineName } from './InlineName';
import type { TFn } from '../../i18n';
import { formatBytes } from '../../lib/format';
import { Icon } from '../../ui';
import { KIND_ICON, kindOf } from './kinds';
import { isHidden, nextSort } from './paneUtil';
import { Thumb } from './Thumb';
import { NativeFileIcon } from './NativeFileIcon';
import { isDirLike, type FEntry, type SortKey, type SortState } from './types';
import type { PickKeys } from './usePaneSelection';
import { dirname, formatWhen } from './util';

/** Per-item wiring shared by the grid and list views. */
export interface ItemKit {
  name(e: FEntry): ReactNode;
  native?: boolean;
  wire(e: FEntry): {
    'data-path': string;
    draggable: boolean;
    onDragStart(ev: DragEvent): void;
    onClick(ev: MouseEvent): void;
    onDoubleClick(): void;
    onContextMenu(ev: MouseEvent): void;
  };
  folderDrop(e: FEntry): {
    onDragOver?(ev: DragEvent): void;
    onDragLeave?(): void;
    onDrop?(ev: DragEvent): void;
  };
  moreButton(e: FEntry): JSX.Element;
  subtitle(e: FEntry): string;
  isSelected(e: FEntry): boolean;
  dropFolder: string | null;
  trash: boolean;
  lang: string;
  t: TFn;
}

interface ItemKitArgs {
  rename?: { path: string; submit(name: string): Promise<void>; cancel(): void };
  native?: boolean;
  trash: boolean;
  lang: string;
  t: TFn;
  selected: ReadonlySet<string>;
  onSelected(s: Set<string>): void;
  onFocus(): void;
  onOpen(e: FEntry): void;
  onContext(e: FEntry | null, x: number, y: number): void;
  anchor: MutableRefObject<string | null>;
  pick(e: FEntry, ev: PickKeys): void;
  dragStart(ev: DragEvent, e: FEntry): void;
  folderDrop: ItemKit['folderDrop'];
  dropFolder: string | null;
}

export function buildItemKit(a: ItemKitArgs): ItemKit {
  const { trash, lang, t } = a;
  const wire = (e: FEntry) => ({
    'data-path': e.path,
    draggable: !trash,
    onDragStart: (ev: DragEvent) => a.dragStart(ev, e),
    onClick: (ev: MouseEvent) => a.pick(e, ev),
    onDoubleClick: () => a.onOpen(e),
    onContextMenu: (ev: MouseEvent) => {
      ev.preventDefault();
      ev.stopPropagation();
      a.onFocus();
      if (!a.selected.has(e.path)) {
        a.onSelected(new Set([e.path]));
        a.anchor.current = e.path;
      }
      a.onContext(e, ev.clientX, ev.clientY);
    },
  });
  const moreButton = (e: FEntry) => (
    <button
      type="button"
      className="files-mo"
      aria-label={t('more')}
      onClick={(ev) => {
        ev.stopPropagation();
        const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
        if (!a.selected.has(e.path)) a.onSelected(new Set([e.path]));
        a.onFocus();
        a.onContext(e, r.left, r.bottom + 4);
      }}
    >
      <Icon name="more" />
    </button>
  );
  const subtitle = (e: FEntry) =>
    trash && e.deletedAt
      ? t('deletedAt', { when: formatWhen(e.deletedAt, lang) })
      : `${isDirLike(e) ? '' : formatBytes(e.size) + ', '}${formatWhen(e.mtime, lang)}`;
  return {
    name: (e) =>
      a.rename?.path === e.path ? (
        <InlineName
          key={e.path}
          name={e.name}
          folder={isDirLike(e)}
          onSubmit={a.rename.submit}
          onCancel={a.rename.cancel}
        />
      ) : (
        e.name
      ),
    native: a.native,
    wire,
    folderDrop: a.folderDrop,
    moreButton,
    subtitle,
    isSelected: (e) => a.selected.has(e.path),
    dropFolder: a.dropFolder,
    trash,
    lang,
    t,
  };
}

export function GridView({ rows, admin, kit }: { rows: FEntry[]; admin: boolean; kit: ItemKit }) {
  const { t, lang } = kit;
  const folders = rows.filter(isDirLike);
  const files = rows.filter((r) => !isDirLike(r));
  return (
    <>
      {folders.length > 0 && (
        <>
          <div className="files-sec">
            {t('folders')} <span>{folders.length}</span>
          </div>
          <div className="files-folds">
            {folders.map((e) => (
              <div
                key={e.path}
                data-entry-type={e.type}
                {...kit.wire(e)}
                {...kit.folderDrop(e)}
                className={`files-fold files-t-dir${kit.isSelected(e) ? ' is-sel' : ''}${isHidden(e) ? ' is-hid' : ''}${kit.dropFolder === e.path ? ' is-drop' : ''}`}
              >
                {kit.native ? (
                  <NativeFileIcon entry={e} />
                ) : (
                  <span className="files-ic">
                    <Icon name={e.type === 'symlink' ? 'link' : 'files'} />
                  </span>
                )}
                <div className="files-fold-t">
                  <b>{kit.name(e)}</b>
                  <small>{kit.trash ? kit.subtitle(e) : formatWhen(e.mtime, lang)}</small>
                </div>
                {kit.moreButton(e)}
              </div>
            ))}
          </div>
        </>
      )}
      {files.length > 0 && (
        <>
          <div className="files-sec">
            {t('files')} <span>{files.length}</span>
          </div>
          <div className="files-grid">
            {files.map((e) => {
              const k = kindOf(e);
              return (
                <div
                  key={e.path}
                  {...kit.wire(e)}
                  className={`files-fc files-t-${k}${kit.isSelected(e) ? ' is-sel' : ''}${isHidden(e) ? ' is-hid' : ''}`}
                >
                  {kit.native ? (
                    <div className="files-os-preview">
                      <NativeFileIcon entry={e} />
                    </div>
                  ) : (
                    <Thumb e={e} admin={admin} />
                  )}
                  <div className="files-meta">
                    {!kit.native && (
                      <span className="files-ic files-ic--sm">
                        <Icon name={e.type === 'symlink' ? 'link' : KIND_ICON[k]} />
                      </span>
                    )}
                    <b title={e.name}>{kit.name(e)}</b>
                    {kit.moreButton(e)}
                  </div>
                  <div className="files-sub">{kit.subtitle(e)}</div>
                </div>
              );
            })}
          </div>
        </>
      )}
    </>
  );
}

interface ListViewProps {
  rows: FEntry[];
  kit: ItemKit;
  sort: SortState;
  onSort(s: SortState): void;
  onSelected(s: Set<string>): void;
  onToggle(e: FEntry, shiftKey: boolean): void;
  /** Show each row's parent folder (search results). */
  showDir: boolean;
}

export function ListView({
  rows,
  kit,
  sort,
  onSort,
  onSelected,
  onToggle,
  showDir,
}: ListViewProps) {
  const { t, lang, trash } = kit;
  const allOn = rows.length > 0 && rows.every((r) => kit.isSelected(r));
  const th = (key: SortKey, label: string, cls?: string) => (
    <th
      className={cls}
      aria-sort={sort.key === key ? (sort.dir === 'asc' ? 'ascending' : 'descending') : undefined}
    >
      <button type="button" onClick={() => onSort(nextSort(sort, key))}>
        {label}
        {sort.key === key && <Icon name={sort.dir === 'asc' ? 'chevronup' : 'chevron'} />}
      </button>
    </th>
  );
  return (
    <table className="ui-table files-lv">
      <thead>
        <tr>
          <th className="files-cb">
            <button
              type="button"
              className={`files-box${allOn ? ' on' : ''}`}
              aria-label={t('selectAll')}
              aria-pressed={allOn}
              onClick={() => onSelected(allOn ? new Set() : new Set(rows.map((r) => r.path)))}
            >
              {allOn && <Icon name="check" />}
            </button>
          </th>
          {th('name', t('col.name'))}
          {th('size', t('col.size'), 'num')}
          {th('owner', t('col.owner'), 'files-c-owner')}
          {th('perm', t('col.permissions'), 'files-c-perm')}
          {th('mtime', trash ? t('col.deleted') : t('col.modified'), 'files-c-mtime')}
        </tr>
      </thead>
      <tbody>
        {rows.map((e) => {
          const k = kindOf(e);
          const isSel = kit.isSelected(e);
          return (
            <tr
              key={e.path}
              {...kit.wire(e)}
              {...kit.folderDrop(e)}
              aria-selected={isSel || undefined}
              className={`files-t-${k}${isHidden(e) ? ' is-hid' : ''}${kit.dropFolder === e.path ? ' is-drop' : ''}`}
            >
              <td className="files-cb">
                <button
                  type="button"
                  className={`files-box${isSel ? ' on' : ''}`}
                  aria-label={t('selectRow', { name: e.name })}
                  aria-pressed={isSel}
                  onClick={(ev) => {
                    ev.stopPropagation();
                    onToggle(e, ev.shiftKey);
                  }}
                >
                  {isSel && <Icon name="check" />}
                </button>
              </td>
              <td>
                <div className="files-nm">
                  {kit.native ? (
                    <NativeFileIcon entry={e} compact />
                  ) : (
                    <span className="files-ic files-ic--sm">
                      <Icon name={e.type === 'symlink' ? 'link' : KIND_ICON[k]} />
                    </span>
                  )}
                  <span className="files-nm-t">{kit.name(e)}</span>
                  {e.type === 'symlink' && e.target && (
                    <small className="files-link">→ {e.target}</small>
                  )}
                  {showDir && e.path && <small className="files-link">{dirname(e.path)}</small>}
                </div>
              </td>
              <td className="num">{isDirLike(e) ? '' : formatBytes(e.size)}</td>
              <td className="files-c-owner">{e.owner}</td>
              <td className="files-c-perm mono">{e.perm}</td>
              <td className="files-c-mtime files-muted">
                {formatWhen(trash && e.deletedAt ? e.deletedAt : e.mtime, lang)}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
