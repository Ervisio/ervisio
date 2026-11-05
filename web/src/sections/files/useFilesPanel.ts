import { useEffect, useRef, useState } from 'react';
import type { FEntry } from './types';

/**
 * The details panel for the selection. Inline (single pane, desktop) it opens on its own until
 * dismissed for that selection; in split view and on mobile it opens only on request.
 */
export function useFilesPanel(
  entries: FEntry[],
  mobile: boolean,
  split: boolean,
  manual = false,
  navigationKey = '',
) {
  const [dismissed, setDismissed] = useState('');
  const [forcePanel, setForcePanel] = useState(false);
  const [inspection, setInspection] = useState<{ entries: FEntry[]; navigation: string } | null>(
    null,
  );
  // Right-click may activate another pane after its context-menu callback was created.
  const latestNavigation = useRef(navigationKey);
  latestNavigation.current = navigationKey;
  const selKey = entries.map((e) => e.path).join('\n');
  useEffect(() => {
    if (entries.length === 0) setForcePanel(false);
  }, [entries.length]);
  useEffect(() => {
    if (manual) setInspection(null);
  }, [manual, navigationKey]);
  const inspected = manual
    ? inspection?.navigation === navigationKey
      ? inspection.entries
      : []
    : entries;
  const isOpen = manual
    ? inspected.length > 0
    : entries.length > 0 && (mobile || split ? forcePanel : dismissed !== selKey);
  return {
    isOpen,
    entries: inspected,
    show: (requested = entries) => {
      if (manual) setInspection({ entries: [...requested], navigation: latestNavigation.current });
      else setForcePanel(true);
    },
    hide: () => {
      setForcePanel(false);
      setInspection(null);
    },
    dismiss: () => {
      setDismissed(selKey);
      setForcePanel(false);
      setInspection(null);
    },
  };
}

export type FilesPanel = ReturnType<typeof useFilesPanel>;
