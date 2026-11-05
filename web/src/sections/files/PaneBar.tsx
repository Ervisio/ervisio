import { useEffect, useState } from 'react';
import type { TFn, useT } from '../../i18n';
import { Badge, Button, DropdownMenu, Icon, IconButton, Segmented, type MenuItem } from '../../ui';
import { nextSort, VIRTUAL_ICON } from './paneUtil';
import type { PaneState, SortKey, SortState, View } from './types';
import { basename, crumbs, isVirtual } from './util';
import { remoteLocation, remoteTarget } from './remoteLocation';

interface PaneBarProps {
  native?: boolean;
  navigationKey?: string;
  label?: string;
  state: PaneState;
  query: string;
  onQuery(q: string): void;
  sort: SortState;
  onSort(s: SortState): void;
  onNavigate(loc: string): void;
  onView(v: View): void;
  onUp(): void;
  onLeaveAdmin(): void;
  t: TFn;
}

export function PaneBar({ label, state, t, ...p }: PaneBarProps) {
  const { loc } = state;
  const virtual = isVirtual(loc);
  const canUp = !virtual && (remoteTarget(loc)?.path ?? loc) !== '/';
  const title = virtual ? t(`place.${loc.replace(':', '')}`) : basename(loc) || '/';
  const [history, setHistory] = useState({ key: p.navigationKey, paths: [loc], index: 0 });
  useEffect(() => {
    if (!p.native) return;
    setHistory((h) => {
      if (h.key !== p.navigationKey) return { key: p.navigationKey, paths: [loc], index: 0 };
      if (h.paths[h.index] === loc) return h;
      return { ...h, paths: [...h.paths.slice(0, h.index + 1), loc], index: h.index + 1 };
    });
  }, [loc, p.native, p.navigationKey]);
  const step = (delta: number) => {
    const index = history.index + delta;
    if (index < 0 || index >= history.paths.length) return;
    setHistory({ ...history, index });
    p.onNavigate(history.paths[index]);
  };
  return (
    <div className="files-bar">
      {label && <span className="files-pl">{label}</span>}
      {p.native && (
        <>
          <IconButton
            icon="right"
            className="files-native-back"
            label={t('native.back')}
            disabled={history.index === 0}
            onClick={() => step(-1)}
          />
          <IconButton
            icon="right"
            label={t('native.forward')}
            disabled={history.index >= history.paths.length - 1}
            onClick={() => step(1)}
          />
        </>
      )}
      <IconButton icon="chevronup" label={t('up')} disabled={!canUp} onClick={() => p.onUp()} />
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
      <SearchBox
        query={p.query}
        onQuery={p.onQuery}
        canOpen={!virtual}
        always={p.native && !virtual}
        t={t}
      />
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
  );
}

function SearchBox({
  query,
  onQuery,
  canOpen,
  always,
  t,
}: {
  query: string;
  onQuery(q: string): void;
  canOpen: boolean;
  always?: boolean;
  t: TFn;
}) {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!query) setOpen(false);
  }, [query]);

  if (!always && !open && !query)
    return (
      canOpen && <IconButton icon="search" label={t('search')} onClick={() => setOpen(true)} />
    );
  return (
    <div className="files-search ui-in">
      <Icon name="search" />
      <input
        autoFocus={!always}
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
  const remote = remoteTarget(loc);
  const [val, setVal] = useState(remote?.path ?? loc);
  useEffect(() => setVal(remoteTarget(loc)?.path ?? loc), [loc]);
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
          if (v.startsWith('/')) {
            const path = v.length > 1 ? v.replace(/\/+$/, '') : '/';
            onNavigate(remote ? remoteLocation(remote.conn, path) : path);
          }
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
  const hiddenItems: MenuItem[] = cs.slice(0, hiddenCount).map((c) => ({
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
            {remote && c.name === '/' ? t('remote.title') : c.name}
          </button>
        </span>
      ))}
    </nav>
  );
}
