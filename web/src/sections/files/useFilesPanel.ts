import { useEffect, useState } from 'react';
import type { FEntry } from './types';

/**
 * The details panel for the selection. Inline (single pane, desktop) it opens on its own until
 * dismissed for that selection; in split view and on mobile it opens only on request.
 */
export function useFilesPanel(entries: FEntry[], mobile: boolean, split: boolean) {
  const [dismissed, setDismissed] = useState('');
  const [forcePanel, setForcePanel] = useState(false);
  const selKey = entries.map((e) => e.path).join('\n');
  useEffect(() => {
    if (entries.length === 0) setForcePanel(false);
  }, [entries.length]);
  const isOpen = entries.length > 0 && (mobile || split ? forcePanel : dismissed !== selKey);
  return {
    isOpen,
    show: () => setForcePanel(true),
    hide: () => setForcePanel(false),
    dismiss: () => {
      setDismissed(selKey);
      setForcePanel(false);
    },
  };
}

export type FilesPanel = ReturnType<typeof useFilesPanel>;
