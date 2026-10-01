import { useMemo, useState, type ReactNode } from 'react';
import { useT } from '../i18n';
import { Checkbox } from './Form';
import { Icon } from './Icon';

export interface Column<T> {
  key: string;
  header: ReactNode;
  render?(row: T): ReactNode;
  /** Value used for sorting (and for rendering when no render()). */
  value?(row: T): string | number | boolean | null | undefined;
  sortable?: boolean;
  align?: 'left' | 'right';
  width?: number | string;
  mono?: boolean;
}

export interface SortState {
  key: string;
  dir: 'asc' | 'desc';
}

export interface TableProps<T> {
  columns: Column<T>[];
  rows: T[];
  rowKey(row: T): string;
  onRowClick?(row: T): void;
  /** Shows a checkbox column. */
  selectable?: boolean;
  selected?: ReadonlySet<string>;
  onSelectedChange?(keys: Set<string>): void;
  /** Highlighted row (e.g. the one open in the side panel). */
  activeKey?: string;
  /** Controlled sort; omit for built-in client-side sorting. */
  sort?: SortState | null;
  onSortChange?(s: SortState | null): void;
  empty?: ReactNode;
  maxHeight?: number | string;
  caption?: string;
}

export function Table<T>({ columns, rows, rowKey, onRowClick, selectable, selected, onSelectedChange, activeKey, sort: sortProp, onSortChange, empty, maxHeight, caption }: TableProps<T>) {
  const t = useT('ui');
  const [inner, setInner] = useState<SortState | null>(null);
  const controlled = sortProp !== undefined;
  const sort = controlled ? sortProp : inner;
  const setSort = (s: SortState | null) => (controlled ? onSortChange?.(s) : setInner(s));

  const sorted = useMemo(() => {
    if (!sort || controlled) return rows;
    const col = columns.find((c) => c.key === sort.key);
    if (!col?.value) return rows;
    const f = col.value;
    const m = sort.dir === 'asc' ? 1 : -1;
    return [...rows].sort((a, b) => {
      const x = f(a) ?? '';
      const y = f(b) ?? '';
      if (typeof x === 'number' && typeof y === 'number') return (x - y) * m;
      return String(x).localeCompare(String(y), undefined, { numeric: true, sensitivity: 'base' }) * m;
    });
  }, [rows, sort, controlled, columns]);

  const sel = selected ?? new Set<string>();
  const allKeys = sorted.map(rowKey);
  const allOn = allKeys.length > 0 && allKeys.every((k) => sel.has(k));
  const someOn = allKeys.some((k) => sel.has(k));
  const toggle = (k: string, on: boolean) => {
    const n = new Set(sel);
    if (on) n.add(k);
    else n.delete(k);
    onSelectedChange?.(n);
  };
  const cycle = (key: string) => {
    if (!sort || sort.key !== key) setSort({ key, dir: 'asc' });
    else if (sort.dir === 'asc') setSort({ key, dir: 'desc' });
    else setSort(null);
  };

  return (
    <div className="ui-table-wrap" style={maxHeight ? { maxHeight } : undefined}>
      <table className="ui-table">
        {caption && <caption className="sr-only">{caption}</caption>}
        <thead>
          <tr>
            {selectable && (
              <th className="ui-td-sel">
                <Checkbox checked={allOn} indeterminate={!allOn && someOn} aria-label={t('selectAll')} onChange={(on) => onSelectedChange?.(on ? new Set(allKeys) : new Set())} />
              </th>
            )}
            {columns.map((c) => (
              <th key={c.key} className={c.align === 'right' ? 'num' : undefined} style={{ width: c.width, textAlign: c.align }} aria-sort={sort?.key === c.key ? (sort.dir === 'asc' ? 'ascending' : 'descending') : undefined}>
                {c.sortable ? (
                  <button type="button" onClick={() => cycle(c.key)}>
                    {c.header}
                    {sort?.key === c.key && <Icon name={sort.dir === 'asc' ? 'chevronup' : 'chevron'} />}
                  </button>
                ) : (
                  c.header
                )}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sorted.map((r) => {
            const k = rowKey(r);
            const isSel = sel.has(k) || activeKey === k;
            return (
              <tr
                key={k}
                data-click={onRowClick ? 'true' : undefined}
                aria-selected={isSel || undefined}
                tabIndex={onRowClick ? 0 : undefined}
                onClick={onRowClick ? () => onRowClick(r) : undefined}
                onKeyDown={onRowClick ? (e) => { if (e.key === 'Enter' && e.target === e.currentTarget) onRowClick(r); } : undefined}
              >
                {selectable && (
                  <td className="ui-td-sel" onClick={(e) => e.stopPropagation()}>
                    <Checkbox checked={sel.has(k)} aria-label={t('selectRow')} onChange={(on) => toggle(k, on)} />
                  </td>
                )}
                {columns.map((c) => (
                  <td key={c.key} className={`${c.align === 'right' ? 'num' : ''}${c.mono ? ' mono' : ''}`}>
                    {c.render ? c.render(r) : String(c.value?.(r) ?? '')}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
      {sorted.length === 0 && empty}
    </div>
  );
}
