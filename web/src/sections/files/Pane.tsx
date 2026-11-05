import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
  type KeyboardEvent,
  type MouseEvent,
} from 'react';
import { ApiError } from '../../api';
import { useI18n, useT, type TFn } from '../../i18n';
import { formatBytes } from '../../lib/format';
import {
  Badge,
  Button,
  DropdownMenu,
  EmptyState,
  Icon,
  IconButton,
  Segmented,
  Skeleton,
  type IconName,
  type MenuItem,
} from '../../ui';
import { DRAG_TYPE } from './dropfiles';
import type { PaneData } from './data';
import { KIND_ICON, kindOf } from './kinds';
import { Thumb } from './Thumb';
import {
  isDirLike,
  type FEntry,
  type PaneState,
  type SortKey,
  type SortState,
  type View,
} from './types';
import { basename, crumbs, dirname, formatWhen, isVirtual, sortEntries } from './util';

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

const VIRTUAL_ICON: Record<string, IconName> = {
  'trash:': 'trash',
  'recent:': 'clock',
  'starred:': 'star',
};
const ARROW_STEP: Record<string, number> = {
  ArrowDown: 1,
  ArrowRight: 1,
  ArrowUp: -1,
  ArrowLeft: -1,
};

type PickKeys = { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean };

/** Per-item wiring shared by the grid and list views. */
interface ItemKit {
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

const isHidden = (e: FEntry) => e.name.startsWith('.');
const hasFiles = (dt: DataTransfer) => Array.from(dt.types).includes('Files');
const hasInternal = (dt: DataTransfer) => Array.from(dt.types).includes(DRAG_TYPE);
const nextSort = (sort: SortState, key: SortKey): SortState => ({
  key,
  dir: sort.key === key && sort.dir === 'asc' ? 'desc' : 'asc',
});

const PLAIN_KEY_SHORTCUTS: Record<string, Shortcut> = {
  F2: 'rename',
  F5: 'refresh',
  Backspace: 'up',
};
const MOD_KEY_SHORTCUTS: Record<string, Shortcut> = { c: 'copy', x: 'cut', v: 'paste' };

function keyShortcut(ev: KeyboardEvent, mod: boolean): Shortcut | null {
  const k = ev.key;
  if (k === 'Delete') return ev.shiftKey ? 'permDelete' : 'delete';
  if (k === 'Enter') return mod ? null : 'open';
  if (ev.altKey && k === 'ArrowUp') return 'up';
  if (PLAIN_KEY_SHORTCUTS[k]) return PLAIN_KEY_SHORTCUTS[k];
  if (!mod) return null;
  const lower = k.toLowerCase();
  if (ev.shiftKey && lower === 'n') return 'newFolder';
  return MOD_KEY_SHORTCUTS[lower] ?? null;
}

function rangePaths(rows: FEntry[], fromPath: string, toPath: string): string[] | null {
  const a = rows.findIndex((r) => r.path === fromPath);
  const b = rows.findIndex((r) => r.path === toPath);
  if (a < 0 || b < 0) return null;
  return rows.slice(Math.min(a, b), Math.max(a, b) + 1).map((r) => r.path);
}

export function Pane(p: PaneProps) {
  const t = useT('files');
  const { lang } = useI18n();
  const { state, data } = p;
  const loc = state.loc;
  const rows = useMemo(() => sortEntries(data.entries, p.sort), [data.entries, p.sort]);
  const anchor = useRef<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const [drop, setDrop] = useState(false);
  const [dropFolder, setDropFolder] = useState<string | null>(null);
  const virtual = isVirtual(loc);
  const trash = loc === 'trash:';
  const canUp = !virtual && loc !== '/';

  const pick = (e: FEntry, ev: PickKeys) => {
    p.onFocus();
    const next = new Set(ev.ctrlKey || ev.metaKey ? p.selected : []);
    const range = ev.shiftKey && anchor.current ? rangePaths(rows, anchor.current, e.path) : null;
    if (range) {
      next.clear();
      range.forEach((path) => next.add(path));
    } else if (!(ev.shiftKey && anchor.current)) {
      if ((ev.ctrlKey || ev.metaKey) && next.has(e.path)) next.delete(e.path);
      else next.add(e.path);
      anchor.current = e.path;
    }
    p.onSelected(next);
  };

  const moveSelection = (step: number, ev: PickKeys) => {
    const cur = anchor.current ? rows.findIndex((r) => r.path === anchor.current) : -1;
    const n = Math.min(rows.length - 1, Math.max(0, cur < 0 ? 0 : cur + step));
    pick(rows[n], ev);
    root.current
      ?.querySelector(`[data-path="${CSS.escape(rows[n].path)}"]`)
      ?.scrollIntoView({ block: 'nearest' });
  };

  const onKeyDown = (ev: KeyboardEvent<HTMLDivElement>) => {
    const tag = (ev.target as HTMLElement).tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA') return;
    const mod = ev.ctrlKey || ev.metaKey;
    const k = ev.key;
    const shortcut = keyShortcut(ev, mod);
    if (shortcut) {
      ev.preventDefault();
      p.onShortcut(shortcut);
    } else if (mod && k.toLowerCase() === 'a') {
      ev.preventDefault();
      p.onSelected(new Set(rows.map((r) => r.path)));
    } else if (k === 'Escape') {
      if (p.selected.size) p.onSelected(new Set());
      else if (p.query) p.onQuery('');
    } else if (ARROW_STEP[k] && rows.length) {
      ev.preventDefault();
      moveSelection(ARROW_STEP[k], ev);
    }
  };

  const canDropHere = !virtual;
  const onDragOver = (ev: DragEvent) => {
    if (!canDropHere || !(hasFiles(ev.dataTransfer) || hasInternal(ev.dataTransfer))) return;
    ev.preventDefault();
    ev.dataTransfer.dropEffect =
      hasInternal(ev.dataTransfer) && !(ev.ctrlKey || ev.altKey) ? 'move' : 'copy';
    setDrop(true);
  };
  const doDrop = (ev: DragEvent, dest: string) => {
    ev.preventDefault();
    ev.stopPropagation();
    setDrop(false);
    setDropFolder(null);
    if (hasInternal(ev.dataTransfer)) {
      try {
        const paths: string[] = JSON.parse(ev.dataTransfer.getData(DRAG_TYPE));
        p.onDropMove(paths, dest, ev.ctrlKey || ev.altKey);
      } catch {
        /* ignore malformed drags */
      }
    } else if (hasFiles(ev.dataTransfer)) p.onDropFiles(ev.dataTransfer, dest);
  };
  const dragStart = (ev: DragEvent, e: FEntry) => {
    if (trash) return ev.preventDefault();
    ev.dataTransfer.setData(DRAG_TYPE, JSON.stringify(p.dragPaths(e)));
    ev.dataTransfer.effectAllowed = 'copyMove';
    if (!p.selected.has(e.path)) p.onSelected(new Set([e.path]));
  };
  const folderDrop = (e: FEntry) =>
    isDirLike(e) && !trash
      ? {
          onDragOver: (ev: DragEvent) => {
            if (hasInternal(ev.dataTransfer) || hasFiles(ev.dataTransfer)) {
              ev.preventDefault();
              ev.stopPropagation();
              setDropFolder(e.path);
            }
          },
          onDragLeave: () => setDropFolder((f) => (f === e.path ? null : f)),
          onDrop: (ev: DragEvent) => doDrop(ev, e.path),
        }
      : {};

  const wireItem = (e: FEntry) => ({
    'data-path': e.path,
    draggable: !trash,
    onDragStart: (ev: DragEvent) => dragStart(ev, e),
    onClick: (ev: MouseEvent) => pick(e, ev),
    onDoubleClick: () => p.onOpen(e),
    onContextMenu: (ev: MouseEvent) => {
      ev.preventDefault();
      ev.stopPropagation();
      p.onFocus();
      if (!p.selected.has(e.path)) {
        p.onSelected(new Set([e.path]));
        anchor.current = e.path;
      }
      p.onContext(e, ev.clientX, ev.clientY);
    },
  });
  const moreBtn = (e: FEntry) => (
    <button
      type="button"
      className="files-mo"
      aria-label={t('more')}
      onClick={(ev) => {
        ev.stopPropagation();
        const r = (ev.currentTarget as HTMLElement).getBoundingClientRect();
        if (!p.selected.has(e.path)) p.onSelected(new Set([e.path]));
        p.onFocus();
        p.onContext(e, r.left, r.bottom + 4);
      }}
    >
      <Icon name="more" />
    </button>
  );
  const sub = (e: FEntry) =>
    trash && e.deletedAt
      ? t('deletedAt', { when: formatWhen(e.deletedAt, lang) })
      : `${isDirLike(e) ? '' : formatBytes(e.size) + ', '}${formatWhen(e.mtime, lang)}`;
  const sel = (e: FEntry) => p.selected.has(e.path);
  const kit: ItemKit = {
    wire: wireItem,
    folderDrop,
    moreButton: moreBtn,
    subtitle: sub,
    isSelected: sel,
    dropFolder,
    trash,
    lang,
    t,
  };

  const title = virtual ? t(`place.${loc.replace(':', '')}`) : basename(loc) || '/';

  let body;
  if (data.error)
    body = (
      <ErrorView
        err={data.error}
        loc={loc}
        t={t}
        onRetry={p.onRetryAdmin}
        onHome={() => p.onNavigate('home:')}
      />
    );
  else if (data.loading && rows.length === 0) body = <LoadingView grid={state.view === 'grid'} />;
  else if (rows.length === 0)
    body = <EmptyView loc={loc} query={p.query} searching={data.searching} t={t} />;
  else if (state.view === 'grid') body = <GridView rows={rows} admin={state.admin} kit={kit} />;
  else {
    body = (
      <ListView
        rows={rows}
        kit={kit}
        sort={p.sort}
        onSort={p.onSort}
        onSelected={p.onSelected}
        onToggle={(e, shiftKey) => pick(e, { ctrlKey: true, metaKey: false, shiftKey })}
        showDir={data.entries !== rows && !trash && !!p.query}
      />
    );
  }

  return (
    <div
      ref={root}
      className={`files-pn${p.active ? ' is-act' : ''}${drop ? ' is-drop' : ''}`}
      tabIndex={0}
      onKeyDown={onKeyDown}
      onMouseDown={() => p.onFocus()}
      onDragOver={onDragOver}
      onDragLeave={(ev) => {
        if (!ev.currentTarget.contains(ev.relatedTarget as Node)) {
          setDrop(false);
          setDropFolder(null);
        }
      }}
      onDrop={(ev) => canDropHere && doDrop(ev, loc)}
    >
      <div className="files-bar">
        {p.label && <span className="files-pl">{p.label}</span>}
        <IconButton
          icon="chevronup"
          label={t('up')}
          disabled={!canUp}
          onClick={() => p.onShortcut('up')}
        />
        <Crumbs loc={loc} onNavigate={p.onNavigate} title={title} t={t} />
        {state.admin && (
          <Badge tone="warn" dot>
            {t('adminBrowsing')}
          </Badge>
        )}
        {state.admin && (
          <Button size="sm" variant="ghost" onClick={p.onLeaveAdmin}>
            {t('leaveAdmin')}
          </Button>
        )}
        <SearchBox query={p.query} onQuery={p.onQuery} canOpen={!virtual} t={t} />
        <SortMenu sort={p.sort} onSort={p.onSort} t={t} />
        <Segmented<View>
          aria-label={t('view.label')}
          value={state.view}
          onChange={p.onView}
          options={[
            { value: 'grid', icon: 'grid', title: t('view.grid') },
            { value: 'list', icon: 'list', title: t('view.list') },
          ]}
        />
      </div>
      <div
        className="files-content"
        onClick={(ev) => {
          if (
            ev.target === ev.currentTarget ||
            (ev.target as HTMLElement).classList.contains('files-grid') ||
            (ev.target as HTMLElement).classList.contains('files-folds')
          )
            p.onSelected(new Set());
        }}
        onContextMenu={(ev) => {
          ev.preventDefault();
          p.onFocus();
          p.onSelected(new Set());
          p.onContext(null, ev.clientX, ev.clientY);
        }}
      >
        {data.searching && rows.length > 0 && <div className="files-note">{t('searching')}</div>}
        {data.truncated && (
          <div className="files-note">{p.query ? t('searchTruncated') : t('listTruncated')}</div>
        )}
        {body}
        {drop && !virtual && (
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

function SearchBox({
  query,
  onQuery,
  canOpen,
  t,
}: {
  query: string;
  onQuery(q: string): void;
  canOpen: boolean;
  t: TFn;
}) {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!query) setOpen(false);
  }, [query]);

  if (!open && !query)
    return (
      canOpen && <IconButton icon="search" label={t('search')} onClick={() => setOpen(true)} />
    );
  return (
    <div className="files-search ui-in">
      <Icon name="search" />
      <input
        autoFocus
        value={query}
        placeholder={t('searchHere')}
        aria-label={t('searchHere')}
        onChange={(e) => onQuery(e.target.value)}
        onBlur={() => !query && setOpen(false)}
        onKeyDown={(e) => e.key === 'Escape' && (onQuery(''), setOpen(false))}
      />
    </div>
  );
}

function SortMenu({ sort, onSort, t }: { sort: SortState; onSort(s: SortState): void; t: TFn }) {
  const items: MenuItem[] = (['name', 'mtime', 'size'] as SortKey[]).map((k) => ({
    id: k,
    label: (sort.key === k ? (sort.dir === 'asc' ? '↑ ' : '↓ ') : '') + t(`sort.${k}`),
    onSelect: () => onSort(nextSort(sort, k)),
  }));
  return (
    <DropdownMenu
      items={items}
      aria-label={t('sort.label')}
      trigger={(tp) => <IconButton icon="sort" label={t('sort.label')} {...tp} />}
    />
  );
}

function LoadingView({ grid }: { grid: boolean }) {
  return (
    <div className={grid ? 'files-grid' : 'files-skel'}>
      {Array.from({ length: 8 }, (_, i) =>
        grid ? (
          <Skeleton key={i} height={150} style={{ borderRadius: 18 }} />
        ) : (
          <Skeleton key={i} height={38} style={{ borderRadius: 12 }} />
        ),
      )}
    </div>
  );
}

function EmptyView({
  loc,
  query,
  searching,
  t,
}: {
  loc: string;
  query: string;
  searching: boolean;
  t: TFn;
}) {
  const icon = VIRTUAL_ICON[loc] ?? (query ? 'search' : 'files');
  if (query)
    return (
      <EmptyState
        hue="file"
        icon={icon}
        title={t('empty.search', { q: query })}
        text={searching ? t('searching') : t('empty.searchHint')}
      />
    );
  const place = VIRTUAL_ICON[loc] ? loc.replace(':', '') : 'folder';
  return (
    <EmptyState hue="file" icon={icon} title={t(`empty.${place}`)} text={t(`empty.${place}Hint`)} />
  );
}

function GridView({ rows, admin, kit }: { rows: FEntry[]; admin: boolean; kit: ItemKit }) {
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
                {...kit.wire(e)}
                {...kit.folderDrop(e)}
                className={`files-fold files-t-dir${kit.isSelected(e) ? ' is-sel' : ''}${isHidden(e) ? ' is-hid' : ''}${kit.dropFolder === e.path ? ' is-drop' : ''}`}
              >
                <span className="files-ic">
                  <Icon name={e.type === 'symlink' ? 'link' : 'files'} />
                </span>
                <div className="files-fold-t">
                  <b>{e.name}</b>
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
                  <Thumb e={e} admin={admin} />
                  <div className="files-meta">
                    <span className="files-ic files-ic--sm">
                      <Icon name={e.type === 'symlink' ? 'link' : KIND_ICON[k]} />
                    </span>
                    <b title={e.name}>{e.name}</b>
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

function ListView({ rows, kit, sort, onSort, onSelected, onToggle, showDir }: ListViewProps) {
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
                  <span className="files-ic files-ic--sm">
                    <Icon name={e.type === 'symlink' ? 'link' : KIND_ICON[k]} />
                  </span>
                  <span className="files-nm-t">{e.name}</span>
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

function ErrorView({
  err,
  loc,
  t,
  onRetry,
  onHome,
}: {
  err: ApiError;
  loc: string;
  t: ReturnType<typeof useT>;
  onRetry(): void;
  onHome(): void;
}) {
  let title = t('error.generic');
  let text = err.message;
  if (err.code === 'needs_admin') {
    title = t('error.needsAdmin');
    text = t('error.needsAdminHint');
  } else if (err.code === 'forbidden') {
    title = t('error.forbidden');
    text = t('error.forbiddenHint');
  } else if (err.code === 'not_found') {
    title = t('error.notFound');
    text = t('error.notFoundHint', { path: loc });
  } else if (err.code === 'invalid') title = t('error.invalid');
  else if (err.code === 'network' || err.code === 'unavailable') {
    title = t('error.offline');
    text = t('error.offlineHint');
  }
  return (
    <EmptyState
      hue="file"
      icon={err.code === 'needs_admin' || err.code === 'forbidden' ? 'lock' : 'alert'}
      title={title}
      text={text}
      action={
        <div className="files-err-act">
          {err.code === 'needs_admin' && (
            <Button variant="primary" onClick={onRetry}>
              {t('error.unlock')}
            </Button>
          )}
          {err.code === 'not_found' || err.code === 'invalid' ? (
            <Button onClick={onHome}>{t('error.goHome')}</Button>
          ) : (
            <Button onClick={onRetry}>{t('error.retry')}</Button>
          )}
        </div>
      }
    />
  );
}

function Crumbs({
  loc,
  onNavigate,
  title,
  t,
}: {
  loc: string;
  onNavigate(l: string): void;
  title: string;
  t: ReturnType<typeof useT>;
}) {
  const [edit, setEdit] = useState(false);
  const [val, setVal] = useState(loc);
  useEffect(() => setVal(loc), [loc]);
  if (isVirtual(loc)) {
    return (
      <div className="files-crumb">
        <span className="files-crumb-v">
          <Icon name={VIRTUAL_ICON[loc] ?? 'files'} />
          {title}
        </span>
      </div>
    );
  }
  if (edit) {
    return (
      <form
        className="files-crumb files-crumb-edit ui-in"
        onSubmit={(e) => {
          e.preventDefault();
          setEdit(false);
          const v = val.trim();
          if (v.startsWith('/')) onNavigate(v.length > 1 ? v.replace(/\/+$/, '') : '/');
        }}
      >
        <input
          autoFocus
          spellCheck={false}
          aria-label={t('pathLabel')}
          value={val}
          onChange={(e) => setVal(e.target.value)}
          onBlur={() => setEdit(false)}
          onKeyDown={(e) => e.key === 'Escape' && setEdit(false)}
        />
      </form>
    );
  }
  const cs = crumbs(loc);
  const hiddenCount = cs.length > 4 ? cs.length - 3 : 0;
  const shown = cs.slice(hiddenCount);
  const hiddenItems: MenuItem[] = cs
    .slice(0, hiddenCount)
    .map((c) => ({
      id: c.path,
      label: c.name === '/' ? '/' : c.path,
      onSelect: () => onNavigate(c.path),
    }));
  return (
    <nav
      className="files-crumb"
      aria-label={t('pathLabel')}
      onDoubleClick={() => setEdit(true)}
      title={t('editPathHint')}
    >
      {hiddenCount > 0 && (
        <span className="files-crumb-i">
          <DropdownMenu
            items={hiddenItems}
            aria-label={t('pathLabel')}
            trigger={(tp) => (
              <button type="button" aria-label={t('pathLabel')} {...tp}>
                <Icon name="more" />
              </button>
            )}
          />
        </span>
      )}
      {shown.map((c, i) => (
        <span key={c.path} className="files-crumb-i">
          {(i > 0 || hiddenCount > 0) && <Icon name="chevron" className="files-crumb-sep" />}
          <button
            type="button"
            className={i === shown.length - 1 ? 'is-last' : ''}
            onClick={() => onNavigate(c.path)}
          >
            {c.name}
          </button>
        </span>
      ))}
    </nav>
  );
}
