import { ApiError } from '../../api';
import type { TFn, useT } from '../../i18n';
import { Button, EmptyState, Skeleton } from '../../ui';
import type { PaneData } from './data';
import { GridView, ListView, type ItemKit } from './PaneItems';
import { VIRTUAL_ICON } from './paneUtil';
import type { FEntry, PaneState, SortState } from './types';
import type { PickKeys } from './usePaneSelection';

interface PaneBodyProps {
  state: PaneState;
  data: PaneData;
  rows: FEntry[];
  kit: ItemKit;
  query: string;
  sort: SortState;
  onSort(s: SortState): void;
  onSelected(s: Set<string>): void;
  onNavigate(loc: string): void;
  onRetryAdmin(): void;
  pick(e: FEntry, ev: PickKeys): void;
}

export function PaneBody({ state, data, rows, kit, query, ...p }: PaneBodyProps) {
  const { loc } = state;
  const { t, trash } = kit;
  if (data.error)
    return (
      <ErrorView
        err={data.error}
        loc={loc}
        t={t}
        onRetry={p.onRetryAdmin}
        onHome={() => p.onNavigate('home:')}
      />
    );
  if (data.loading && rows.length === 0) return <LoadingView grid={state.view === 'grid'} />;
  if (rows.length === 0)
    return <EmptyView loc={loc} query={query} searching={data.searching} t={t} />;
  if (state.view === 'grid') return <GridView rows={rows} admin={state.admin} kit={kit} />;
  return (
    <ListView
      rows={rows}
      kit={kit}
      sort={p.sort}
      onSort={p.onSort}
      onSelected={p.onSelected}
      onToggle={(e, shiftKey) => p.pick(e, { ctrlKey: true, metaKey: false, shiftKey })}
      showDir={data.entries !== rows && !trash && !!query}
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
