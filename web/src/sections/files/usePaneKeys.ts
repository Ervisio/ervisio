import type { KeyboardEvent } from 'react';
import type { Shortcut } from './Pane';
import type { FEntry } from './types';
import type { PickKeys } from './usePaneSelection';
import { fileKeysEditable, fileShortcut } from './fileShortcuts';

const ARROW_STEP: Record<string, number> = {
  ArrowDown: 1,
  ArrowRight: 1,
  ArrowUp: -1,
  ArrowLeft: -1,
};
interface KeysArgs {
  rows: FEntry[];
  selected: ReadonlySet<string>;
  onSelected(s: Set<string>): void;
  query: string;
  onQuery(q: string): void;
  onShortcut(s: Shortcut): void;
  moveSelection(step: number, ev: PickKeys): void;
}

export function usePaneKeys(a: KeysArgs) {
  return (ev: KeyboardEvent<HTMLDivElement>) => {
    if (ev.defaultPrevented || fileKeysEditable(ev.target)) return;
    const k = ev.key;
    const shortcut = fileShortcut(ev);
    if (shortcut === 'selectAll') {
      ev.preventDefault();
      a.onSelected(new Set(a.rows.map((r) => r.path)));
    } else if (shortcut) {
      ev.preventDefault();
      a.onShortcut(shortcut);
    } else if (k === 'Escape') {
      if (a.selected.size) a.onSelected(new Set());
      else if (a.query) a.onQuery('');
    } else if (ARROW_STEP[k] && a.rows.length) {
      ev.preventDefault();
      a.moveSelection(ARROW_STEP[k], ev);
    }
  };
}
