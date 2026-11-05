import type { Shortcut } from './Pane';

export interface FileKey {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}
export function fileShortcut(ev: FileKey): Shortcut | 'selectAll' | null {
  const mod = ev.ctrlKey || ev.metaKey;
  const key = ev.key.toLowerCase();
  if (mod && !ev.altKey) {
    if (ev.shiftKey && key === 'n') return 'newFolder';
    return (
      ({ a: 'selectAll', c: 'copy', v: 'paste', x: 'cut', z: 'undo' } as const)[key as 'a'] ?? null
    );
  }
  if (ev.altKey) return ev.key === 'ArrowUp' ? 'up' : null;
  if (ev.key === 'Delete') return ev.shiftKey ? 'permDelete' : 'delete';
  if (ev.key === 'Enter') return 'open';
  return ({ F2: 'rename', F5: 'refresh', Backspace: 'up' } as const)[ev.key as 'F2'] ?? null;
}

export function fileKeysEditable(target: EventTarget | null): boolean {
  return (
    target instanceof Element &&
    !!target.closest('input,textarea,select,[contenteditable="true"],[role="textbox"]')
  );
}
