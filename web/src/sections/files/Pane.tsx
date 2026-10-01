import { useEffect, useMemo, useRef, useState, type DragEvent, type KeyboardEvent, type MouseEvent } from 'react';
import { ApiError } from '../../api';
import { useI18n, useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import { Badge, Button, DropdownMenu, EmptyState, Icon, IconButton, Segmented, Skeleton, type IconName, type MenuItem } from '../../ui';
import { DRAG_TYPE } from './dropfiles';
import type { PaneData } from './data';
import { KIND_ICON, kindOf } from './kinds';
import { Thumb } from './Thumb';
import { isDirLike, type FEntry, type PaneState, type SortKey, type SortState, type View } from './types';
import { basename, crumbs, dirname, formatWhen, isVirtual, sortEntries } from './util';

export type Shortcut = 'delete' | 'permDelete' | 'rename' | 'copy' | 'cut' | 'paste' | 'open' | 'up' | 'newFolder' | 'refresh' | 'details';

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

const VIRTUAL_ICON: Record<string, IconName> = { 'trash:': 'trash', 'recent:': 'clock', 'starred:': 'star' };

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
  const [searchOpen, setSearchOpen] = useState(false);
  const virtual = isVirtual(loc);
  const trash = loc === 'trash:';
  const canUp = !virtual && loc !== '/';

  useEffect(() => {
    if (!p.query) setSearchOpen(false);
  }, [p.query]);

  /* ---------- selection ---------- */
  const pick = (e: FEntry, ev: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }) => {
    p.onFocus();
    const next = new Set(ev.ctrlKey || ev.metaKey ? p.selected : []);
    if (ev.shiftKey && anchor.current) {
      const a = rows.findIndex((r) => r.path === anchor.current);
      const b = rows.findIndex((r) => r.path === e.path);
      if (a >= 0 && b >= 0) {
        next.clear();
        for (let i = Math.min(a, b); i <= Math.max(a, b); i++) next.add(rows[i].path);
      }
    } else if (ev.ctrlKey || ev.metaKey) {
      if (next.has(e.path)) next.delete(e.path);
      else next.add(e.path);
      anchor.current = e.path;
    } else {
      next.add(e.path);
      anchor.current = e.path;
    }
    p.onSelected(next);
  };

  const onKeyDown = (ev: KeyboardEvent<HTMLDivElement>) => {
    const tag = (ev.target as HTMLElement).tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA') return;
    const mod = ev.ctrlKey || ev.metaKey;
    const k = ev.key;
    const go = (s: Shortcut) => {
      ev.preventDefault();
      p.onShortcut(s);
    };
    if (k === 'Delete') return go(ev.shiftKey ? 'permDelete' : 'delete');
    if (k === 'F2') return go('rename');
    if (k === 'F5') return go('refresh');
    if (k === 'Enter' && !mod) return go('open');
    if (k === 'Backspace' || (ev.altKey && k === 'ArrowUp')) return go('up');
    if (mod && k.toLowerCase() === 'c') return go('copy');
    if (mod && k.toLowerCase() === 'x') return go('cut');
    if (mod && k.toLowerCase() === 'v') return go('paste');
    if (mod && ev.shiftKey && k.toLowerCase() === 'n') return go('newFolder');
    if (mod && k.toLowerCase() === 'a') {
      ev.preventDefault();
      p.onSelected(new Set(rows.map((r) => r.path)));
      return;
    }
    if (k === 'Escape') {
      if (p.selected.size) p.onSelected(new Set());
      else if (p.query) p.onQuery('');
      return;
    }
    if (['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight'].includes(k) && rows.length) {
      ev.preventDefault();
      const cur = anchor.current ? rows.findIndex((r) => r.path === anchor.current) : -1;
      const step = k === 'ArrowDown' || k === 'ArrowRight' ? 1 : -1;
      const n = Math.min(rows.length - 1, Math.max(0, cur < 0 ? 0 : cur + step));
      pick(rows[n], ev);
      root.current?.querySelector(`[data-path="${CSS.escape(rows[n].path)}"]`)?.scrollIntoView({ block: 'nearest' });
    }
  };

  /* ---------- drag and drop ---------- */
  const hasFiles = (dt: DataTransfer) => Array.from(dt.types).includes('Files');
  const hasInternal = (dt: DataTransfer) => Array.from(dt.types).includes(DRAG_TYPE);
  const canDropHere = !virtual;
  const onDragOver = (ev: DragEvent) => {
    if (!canDropHere || !(hasFiles(ev.dataTransfer) || hasInternal(ev.dataTransfer))) return;
    ev.preventDefault();
    ev.dataTransfer.dropEffect = hasInternal(ev.dataTransfer) && !(ev.ctrlKey || ev.altKey) ? 'move' : 'copy';
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

  /* ---------- header ---------- */
  const sortItems: MenuItem[] = (['name', 'mtime', 'size'] as SortKey[]).map((k) => ({
    id: k,
    label: (p.sort.key === k ? (p.sort.dir === 'asc' ? '↑ ' : '↓ ') : '') + t(`sort.${k}`),
    onSelect: () => p.onSort({ key: k, dir: p.sort.key === k && p.sort.dir === 'asc' ? 'desc' : 'asc' }),
  }));

  const folders = rows.filter(isDirLike);
  const files = rows.filter((r) => !isDirLike(r));
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
  const sub = (e: FEntry) => (trash && e.deletedAt ? t('deletedAt', { when: formatWhen(e.deletedAt, lang) }) : `${isDirLike(e) ? '' : formatBytes(e.size) + ', '}${formatWhen(e.mtime, lang)}`);
  const hidden = (e: FEntry) => e.name.startsWith('.');
  const sel = (e: FEntry) => p.selected.has(e.path);

  const title = virtual ? t(`place.${loc.replace(':', '')}`) : basename(loc) || '/';

  let body;
  if (data.error) body = <ErrorView err={data.error} loc={loc} t={t} onRetry={p.onRetryAdmin} onHome={() => p.onNavigate('home:')} />;
  else if (data.loading && rows.length === 0) {
    body = (
      <div className={p.state.view === 'grid' ? 'files-grid' : 'files-skel'}>
        {Array.from({ length: 8 }, (_, i) => (p.state.view === 'grid' ? <Skeleton key={i} height={150} style={{ borderRadius: 18 }} /> : <Skeleton key={i} height={38} style={{ borderRadius: 12 }} />))}
      </div>
    );
  } else if (rows.length === 0) {
    body = (
      <EmptyState
        hue="file"
        icon={trash ? 'trash' : loc === 'starred:' ? 'star' : loc === 'recent:' ? 'clock' : p.query ? 'search' : 'files'}
        title={p.query ? t('empty.search', { q: p.query }) : trash ? t('empty.trash') : loc === 'starred:' ? t('empty.starred') : loc === 'recent:' ? t('empty.recent') : t('empty.folder')}
        text={p.query ? (data.searching ? t('searching') : t('empty.searchHint')) : trash ? t('empty.trashHint') : loc === 'starred:' ? t('empty.starredHint') : loc === 'recent:' ? t('empty.recentHint') : t('empty.folderHint')}
      />
    );
  } else if (p.state.view === 'grid') {
    body = (
      <>
        {folders.length > 0 && (
          <>
            <div className="files-sec">
              {t('folders')} <span>{folders.length}</span>
            </div>
            <div className="files-folds">
              {folders.map((e) => (
                <div key={e.path} {...wireItem(e)} {...folderDrop(e)} className={`files-fold files-t-dir${sel(e) ? ' is-sel' : ''}${hidden(e) ? ' is-hid' : ''}${dropFolder === e.path ? ' is-drop' : ''}`}>
                  <span className="files-ic">
                    <Icon name={e.type === 'symlink' ? 'link' : 'files'} />
                  </span>
                  <div className="files-fold-t">
                    <b>{e.name}</b>
                    <small>{trash ? sub(e) : formatWhen(e.mtime, lang)}</small>
                  </div>
                  {moreBtn(e)}
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
                  <div key={e.path} {...wireItem(e)} className={`files-fc files-t-${k}${sel(e) ? ' is-sel' : ''}${hidden(e) ? ' is-hid' : ''}`}>
                    <Thumb e={e} admin={state.admin} />
                    <div className="files-meta">
                      <span className="files-ic files-ic--sm">
                        <Icon name={e.type === 'symlink' ? 'link' : KIND_ICON[k]} />
                      </span>
                      <b title={e.name}>{e.name}</b>
                      {moreBtn(e)}
                    </div>
                    <div className="files-sub">{sub(e)}</div>
                  </div>
                );
              })}
            </div>
          </>
        )}
      </>
    );
  } else {
    const allOn = rows.length > 0 && rows.every((r) => sel(r));
    const th = (key: SortKey, label: string, cls?: string) => (
      <th className={cls} aria-sort={p.sort.key === key ? (p.sort.dir === 'asc' ? 'ascending' : 'descending') : undefined}>
        <button type="button" onClick={() => p.onSort({ key, dir: p.sort.key === key && p.sort.dir === 'asc' ? 'desc' : 'asc' })}>
          {label}
          {p.sort.key === key && <Icon name={p.sort.dir === 'asc' ? 'chevronup' : 'chevron'} />}
        </button>
      </th>
    );
    body = (
      <table className="ui-table files-lv">
        <thead>
          <tr>
            <th className="files-cb">
              <button type="button" className={`files-box${allOn ? ' on' : ''}`} aria-label={t('selectAll')} aria-pressed={allOn} onClick={() => p.onSelected(allOn ? new Set() : new Set(rows.map((r) => r.path)))}>
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
            return (
              <tr key={e.path} {...wireItem(e)} {...folderDrop(e)} aria-selected={sel(e) || undefined} className={`files-t-${k}${hidden(e) ? ' is-hid' : ''}${dropFolder === e.path ? ' is-drop' : ''}`}>
                <td className="files-cb">
                  <button
                    type="button"
                    className={`files-box${sel(e) ? ' on' : ''}`}
                    aria-label={t('selectRow', { name: e.name })}
                    aria-pressed={sel(e)}
                    onClick={(ev) => {
                      ev.stopPropagation();
                      pick(e, { ctrlKey: true, metaKey: false, shiftKey: ev.shiftKey });
                    }}
                  >
                    {sel(e) && <Icon name="check" />}
                  </button>
                </td>
                <td>
                  <div className="files-nm">
                    <span className="files-ic files-ic--sm">
                      <Icon name={e.type === 'symlink' ? 'link' : KIND_ICON[k]} />
                    </span>
                    <span className="files-nm-t">{e.name}</span>
                    {e.type === 'symlink' && e.target && <small className="files-link">→ {e.target}</small>}
                    {data.entries !== rows && e.path && !trash && p.query && <small className="files-link">{dirname(e.path)}</small>}
                  </div>
                </td>
                <td className="num">{isDirLike(e) ? '' : formatBytes(e.size)}</td>
                <td className="files-c-owner">{e.owner}</td>
                <td className="files-c-perm mono">{e.perm}</td>
                <td className="files-c-mtime files-muted">{formatWhen(trash && e.deletedAt ? e.deletedAt : e.mtime, lang)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
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
        <IconButton icon="chevronup" label={t('up')} disabled={!canUp} onClick={() => p.onShortcut('up')} />
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
        {searchOpen || p.query ? (
          <div className="files-search ui-in">
            <Icon name="search" />
            <input autoFocus value={p.query} placeholder={t('searchHere')} aria-label={t('searchHere')} onChange={(e) => p.onQuery(e.target.value)} onBlur={() => !p.query && setSearchOpen(false)} onKeyDown={(e) => e.key === 'Escape' && (p.onQuery(''), setSearchOpen(false))} />
          </div>
        ) : (
          !virtual && <IconButton icon="search" label={t('search')} onClick={() => setSearchOpen(true)} />
        )}
        <DropdownMenu items={sortItems} aria-label={t('sort.label')} trigger={(tp) => <IconButton icon="sort" label={t('sort.label')} {...tp} />} />
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
          if (ev.target === ev.currentTarget || (ev.target as HTMLElement).classList.contains('files-grid') || (ev.target as HTMLElement).classList.contains('files-folds')) p.onSelected(new Set());
        }}
        onContextMenu={(ev) => {
          ev.preventDefault();
          p.onFocus();
          p.onSelected(new Set());
          p.onContext(null, ev.clientX, ev.clientY);
        }}
      >
        {data.searching && rows.length > 0 && <div className="files-note">{t('searching')}</div>}
        {data.truncated && <div className="files-note">{p.query ? t('searchTruncated') : t('listTruncated')}</div>}
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

function ErrorView({ err, loc, t, onRetry, onHome }: { err: ApiError; loc: string; t: ReturnType<typeof useT>; onRetry(): void; onHome(): void }) {
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
          {err.code === 'needs_admin' && <Button variant="primary" onClick={onRetry}>{t('error.unlock')}</Button>}
          {err.code === 'not_found' || err.code === 'invalid' ? <Button onClick={onHome}>{t('error.goHome')}</Button> : <Button onClick={onRetry}>{t('error.retry')}</Button>}
        </div>
      }
    />
  );
}

function Crumbs({ loc, onNavigate, title, t }: { loc: string; onNavigate(l: string): void; title: string; t: ReturnType<typeof useT> }) {
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
        <input autoFocus spellCheck={false} aria-label={t('pathLabel')} value={val} onChange={(e) => setVal(e.target.value)} onBlur={() => setEdit(false)} onKeyDown={(e) => e.key === 'Escape' && setEdit(false)} />
      </form>
    );
  }
  const cs = crumbs(loc);
  const hiddenCount = cs.length > 4 ? cs.length - 3 : 0;
  const shown = cs.slice(hiddenCount);
  const hiddenItems: MenuItem[] = cs.slice(0, hiddenCount).map((c) => ({ id: c.path, label: c.name === '/' ? '/' : c.path, onSelect: () => onNavigate(c.path) }));
  return (
    <nav className="files-crumb" aria-label={t('pathLabel')} onDoubleClick={() => setEdit(true)} title={t('editPathHint')}>
      {hiddenCount > 0 && (
        <span className="files-crumb-i">
          <DropdownMenu items={hiddenItems} aria-label={t('pathLabel')} trigger={(tp) => <button type="button" aria-label={t('pathLabel')} {...tp}><Icon name="more" /></button>} />
        </span>
      )}
      {shown.map((c, i) => (
        <span key={c.path} className="files-crumb-i">
          {(i > 0 || hiddenCount > 0) && <Icon name="chevron" className="files-crumb-sep" />}
          <button type="button" className={i === shown.length - 1 ? 'is-last' : ''} onClick={() => onNavigate(c.path)}>
            {c.name}
          </button>
        </span>
      ))}
    </nav>
  );
}
