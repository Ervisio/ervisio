import { useState } from 'react';
import type { TFn } from '../../i18n';
import { toast } from '../../ui';
import type { Clip, Side } from './filesPageTypes';
import { enqueueCopy } from './transfers';
import type { FEntry } from './types';
import { dropSources } from './dropfiles';
import type { FilesTabs } from './useFilesTabs';

/** Copy/cut/paste, copy or move to the other pane, and drag-and-drop moves. */
export function useFilesClipboard(t: TFn, tabs: FilesTabs) {
  const { adminOf, elevate, writableLoc, clearSel } = tabs;
  const [clip, setClip] = useState<Clip | null>(null);

  const enqueueClip = (c: Clip, dest: string, s: Side) =>
    enqueueCopy({
      from: c.paths,
      to: dest,
      move: c.cut,
      admin: adminOf(s),
      onElevate: elevate(s),
      onDone: () => {
        if (c.cut) setClip((current) => (current === c ? null : current));
      },
    });
  const paste = (s: Side) => {
    const dest = writableLoc(s);
    if (!clip || !dest) return;
    enqueueClip(clip, dest, s);
  };
  const pasteInto = (dest: string, s: Side) => {
    if (clip) enqueueClip(clip, dest, s);
  };
  const copyTo = (es: FEntry[], from: Side, to: Side, move: boolean) => {
    const dest = writableLoc(to);
    if (!dest) return toast.info(t('err.noTargetFolder'));
    enqueueCopy({
      from: es.map((e) => e.path),
      to: dest,
      move,
      admin: adminOf(from) || adminOf(to),
      onElevate: elevate(to),
    });
    clearSel(from);
  };
  const dropMove = (paths: string[], dest: string, copy: boolean, s: Side) => {
    const from = dropSources(paths, dest);
    if (!from.length) return;
    enqueueCopy({ from, to: dest, move: !copy, admin: adminOf(s), onElevate: elevate(s) });
  };
  const clipSelection = (es: FEntry[], cut: boolean) => {
    setClip({ paths: es.map((e) => e.path), cut });
    toast.info(cut ? t('clip.cut', { count: es.length }) : t('clip.copied', { count: es.length }));
  };

  return { clip, paste, pasteInto, copyTo, dropMove, clipSelection };
}
