import { useRef } from 'react';
import type { FEntry } from './types';

export type PickKeys = { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean };

interface SelectionArgs {
  rows: FEntry[];
  selected: ReadonlySet<string>;
  onSelected(s: Set<string>): void;
  onFocus(): void;
}

function rangePaths(rows: FEntry[], fromPath: string, toPath: string): string[] | null {
  const a = rows.findIndex((r) => r.path === fromPath);
  const b = rows.findIndex((r) => r.path === toPath);
  if (a < 0 || b < 0) return null;
  return rows.slice(Math.min(a, b), Math.max(a, b) + 1).map((r) => r.path);
}

/** Click/shift/ctrl selection with a range anchor, plus arrow-key movement inside `root`. */
export function usePaneSelection({ rows, selected, onSelected, onFocus }: SelectionArgs) {
  const anchor = useRef<string | null>(null);
  const root = useRef<HTMLDivElement>(null);

  const pick = (e: FEntry, ev: PickKeys) => {
    onFocus();
    const next = new Set(ev.ctrlKey || ev.metaKey ? selected : []);
    const range = ev.shiftKey && anchor.current ? rangePaths(rows, anchor.current, e.path) : null;
    if (range) {
      next.clear();
      range.forEach((path) => next.add(path));
    } else if (!(ev.shiftKey && anchor.current)) {
      if ((ev.ctrlKey || ev.metaKey) && next.has(e.path)) next.delete(e.path);
      else next.add(e.path);
      anchor.current = e.path;
    }
    onSelected(next);
  };

  const moveSelection = (step: number, ev: PickKeys) => {
    const cur = anchor.current ? rows.findIndex((r) => r.path === anchor.current) : -1;
    const n = Math.min(rows.length - 1, Math.max(0, cur < 0 ? 0 : cur + step));
    pick(rows[n], ev);
    root.current
      ?.querySelector(`[data-path="${CSS.escape(rows[n].path)}"]`)
      ?.scrollIntoView({ block: 'nearest' });
  };

  return { anchor, root, pick, moveSelection };
}
