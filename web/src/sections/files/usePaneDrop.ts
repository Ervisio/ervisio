import { useState, type DragEvent } from 'react';
import { DRAG_TYPE } from './dropfiles';
import { hasFiles, hasInternal } from './paneUtil';
import { isDirLike, type FEntry } from './types';

interface DropArgs {
  loc: string;
  virtual: boolean;
  trash: boolean;
  selected: ReadonlySet<string>;
  onSelected(s: Set<string>): void;
  onDropFiles(dt: DataTransfer, destDir: string): void;
  onDropMove(paths: string[], destDir: string, copy: boolean): void;
  dragPaths(e: FEntry): string[];
}

/** Drag sources (items) and drop targets (the pane itself and folder items). */
export function usePaneDrop(a: DropArgs) {
  const [drop, setDrop] = useState(false);
  const [dropFolder, setDropFolder] = useState<string | null>(null);
  const canDropHere = !a.virtual;

  const onDragOver = (ev: DragEvent) => {
    if (!canDropHere || !(hasFiles(ev.dataTransfer) || hasInternal(ev.dataTransfer))) return;
    ev.preventDefault();
    ev.dataTransfer.dropEffect =
      hasInternal(ev.dataTransfer) && !(ev.ctrlKey || ev.altKey) ? 'move' : 'copy';
    setDrop(true);
  };
  const onDragLeave = (ev: DragEvent<HTMLDivElement>) => {
    if (!ev.currentTarget.contains(ev.relatedTarget as Node)) {
      setDrop(false);
      setDropFolder(null);
    }
  };
  const doDrop = (ev: DragEvent, dest: string) => {
    ev.preventDefault();
    ev.stopPropagation();
    setDrop(false);
    setDropFolder(null);
    if (hasInternal(ev.dataTransfer)) {
      try {
        const paths: string[] = JSON.parse(ev.dataTransfer.getData(DRAG_TYPE));
        a.onDropMove(paths, dest, ev.ctrlKey || ev.altKey);
      } catch {
        /* ignore malformed drags */
      }
    } else if (hasFiles(ev.dataTransfer)) a.onDropFiles(ev.dataTransfer, dest);
  };
  const onDrop = (ev: DragEvent) => canDropHere && doDrop(ev, a.loc);

  const dragStart = (ev: DragEvent, e: FEntry) => {
    if (a.trash) return ev.preventDefault();
    ev.dataTransfer.setData(DRAG_TYPE, JSON.stringify(a.dragPaths(e)));
    ev.dataTransfer.effectAllowed = 'copyMove';
    if (!a.selected.has(e.path)) a.onSelected(new Set([e.path]));
  };
  const folderDrop = (e: FEntry) =>
    isDirLike(e) && !a.trash
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

  return { drop, dropFolder, onDragOver, onDragLeave, onDrop, dragStart, folderDrop };
}
