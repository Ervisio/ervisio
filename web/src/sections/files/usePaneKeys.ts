import type { KeyboardEvent } from 'react';
import type { Shortcut } from './Pane';
import type { FEntry } from './types';
import type { PickKeys } from './usePaneSelection';

const ARROW_STEP: Record<string, number> = {
  ArrowDown: 1,
  ArrowRight: 1,
  ArrowUp: -1,
  ArrowLeft: -1,
};
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
    const tag = (ev.target as HTMLElement).tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA') return;
    const mod = ev.ctrlKey || ev.metaKey;
    const k = ev.key;
    const shortcut = keyShortcut(ev, mod);
    if (shortcut) {
      ev.preventDefault();
      a.onShortcut(shortcut);
    } else if (mod && k.toLowerCase() === 'a') {
      ev.preventDefault();
      a.onSelected(new Set(a.rows.map((r) => r.path)));
    } else if (k === 'Escape') {
      if (a.selected.size) a.onSelected(new Set());
      else if (a.query) a.onQuery('');
    } else if (ARROW_STEP[k] && a.rows.length) {
      ev.preventDefault();
      a.moveSelection(ARROW_STEP[k], ev);
    }
  };
}
