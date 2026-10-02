import type { ReactNode } from 'react';
import { Checkbox } from '../kit';

export interface DataColumn<T> {
  key: string;
  header?: ReactNode;
  render(row: T): ReactNode;
  align?: 'left' | 'right';
  width?: number | string;
  /** Hide this column when the table is narrower than this many px (container width is not tracked: uses viewport). */
  hideBelow?: 'sm' | 'md';
  className?: string;
}

/**
 * Table in the design's style: every row is a tinted rounded bar, no divider lines, hover highlight.
 * Use the cell helper classes in your cells: dk-mono, dk-num (right aligned numbers), dk-tag, dk-used, dk-muted.
 */
export function DataTable<T>({ columns, rows, rowKey, onRowClick, selectable, selected, onSelectedChange, rowClass, empty }: {
  columns: DataColumn<T>[];
  rows: T[];
  rowKey(row: T): string;
  onRowClick?(row: T): void;
  selectable?: boolean;
  selected?: ReadonlySet<string>;
  onSelectedChange?(keys: Set<string>): void;
  rowClass?(row: T): string | undefined;
  empty?: ReactNode;
}) {
  if (!rows.length && empty) return <>{empty}</>;
  const sel = selected ?? new Set<string>();
  const allOn = rows.length > 0 && rows.every((r) => sel.has(rowKey(r)));
  const toggle = (k: string, on: boolean) => {
    const n = new Set(sel);
    if (on) n.add(k);
    else n.delete(k);
    onSelectedChange?.(n);
  };
  const cls = (c: DataColumn<T>) => [c.align === 'right' ? 'dk-num' : '', c.hideBelow ? `dk-hide-${c.hideBelow}` : '', c.className ?? ''].filter(Boolean).join(' ');
  return (
    <div className="dk-tablewrap">
      <table className="dk-table">
        <thead>
          <tr>
            {selectable && (
              <th className="dk-cb">
                <Checkbox checked={allOn} indeterminate={!allOn && rows.some((r) => sel.has(rowKey(r)))} onChange={(on) => onSelectedChange?.(on ? new Set(rows.map(rowKey)) : new Set())} aria-label="Select all" />
              </th>
            )}
            {columns.map((c) => (
              <th key={c.key} className={cls(c)} style={{ width: c.width }}>{c.header}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const k = rowKey(r);
            return (
              <tr key={k} className={[onRowClick ? 'dk-click' : '', sel.has(k) ? 'dk-sel' : '', rowClass?.(r) ?? ''].filter(Boolean).join(' ')} onClick={onRowClick ? () => onRowClick(r) : undefined}>
                {selectable && (
                  <td className="dk-cb" onClick={(e) => e.stopPropagation()}>
                    <Checkbox checked={sel.has(k)} onChange={(on) => toggle(k, on)} aria-label="Select" />
                  </td>
                )}
                {columns.map((c) => (
                  <td key={c.key} className={cls(c)}>{c.render(r)}</td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
