import type { ApiError } from '../../api';
import type { useT } from '../../i18n';
import { Button, EmptyState, Skeleton, Table, type Column } from '../../ui';
import type { Unit } from './types';
import { PURPOSES } from './types';

type T = ReturnType<typeof useT>;
export type View = 'table' | 'cards';

type BodyProps = {
  t: T;
  win: boolean;
  error: ApiError | null;
  all: Unit[] | undefined;
  rows: Unit[];
  view: View;
  columns: Column<Unit>[];
  selected: string | null;
  renderCard: (u: Unit) => React.ReactNode;
  onOpen: (name: string) => void;
  onRetry: () => void;
};

/** The unit list: error, loading skeleton, table or cards. */
export function ServicesBody({
  t,
  win,
  error,
  all,
  rows,
  view,
  columns,
  selected,
  renderCard,
  onOpen,
  onRetry,
}: BodyProps) {
  const emptyState = (
    <EmptyState icon="services" hue="svc" title={t('empty.title')} text={t('empty.text')} />
  );
  if (error && !all)
    return (
      <EmptyState
        icon="alert"
        hue="svc"
        title={t('error.title')}
        text={error.message}
        action={<Button onClick={onRetry}>{t('retry')}</Button>}
      />
    );
  if (!all)
    return (
      <div className="svc-skel">
        <Skeleton lines={8} height={44} />
      </div>
    );
  if (view === 'table')
    return (
      <Table
        columns={columns}
        rows={rows}
        rowKey={(u) => u.name}
        activeKey={selected ?? undefined}
        onRowClick={(u) => onOpen(u.name)}
        caption={t('title')}
        empty={emptyState}
      />
    );
  if (rows.length === 0) return emptyState;
  return <UnitCards rows={rows} win={win} t={t} renderCard={renderCard} />;
}

/** Card view: one section per purpose (a single flat section on Windows). */
function UnitCards({
  rows,
  win,
  t,
  renderCard,
}: {
  rows: Unit[];
  win: boolean;
  t: T;
  renderCard: (u: Unit) => React.ReactNode;
}) {
  const grouped: { p: (typeof PURPOSES)[number] | null; units: Unit[] }[] = win
    ? [{ p: null, units: rows }]
    : PURPOSES.map((p) => ({ p, units: rows.filter((u) => u.purpose === p) })).filter(
        (g) => g.units.length,
      );
  return (
    <div className="svc-cards-wrap">
      {grouped.map((g) => (
        <section key={g.p ?? 'all'}>
          {g.p && (
            <h3 className="svc-gt">
              <span className={`svc-dot svc-dot--${g.p}`} />
              {t(`purpose.${g.p}`)} <span className="svc-muted">{g.units.length}</span>
            </h3>
          )}
          <div className="svc-cards">{g.units.map(renderCard)}</div>
        </section>
      ))}
    </div>
  );
}
