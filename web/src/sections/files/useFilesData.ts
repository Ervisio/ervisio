import { useCallback, useEffect, useRef, useState } from 'react';
import { useWindowActivity } from '../../lib/windowActivity';
import { usePaneData, type PaneData } from './data';
import { fcall, onFilesChanged } from './fapi';
import type { Side } from './filesPageTypes';
import type { FEntry, Places } from './types';
import type { FilesTabs } from './useFilesTabs';

const PLACES_REFRESH_MS = 60_000;

export function usePlaces(): Places | null {
  const active = useWindowActivity();
  const [places, setPlaces] = useState<Places | null>(null);
  const loadPlaces = useCallback(() => {
    fcall<Places>('files.places', {}).then(setPlaces, () => undefined);
  }, []);
  useEffect(() => {
    if (!active) return;
    loadPlaces();
    const iv = window.setInterval(loadPlaces, PLACES_REFRESH_MS);
    const off = onFilesChanged(() => loadPlaces());
    return () => {
      window.clearInterval(iv);
      off();
    };
  }, [loadPlaces, active]);
  return places;
}

/** Listings of both panes of the active tab; the right one loads only in split view. */
export function useFilesPaneData(tabs: FilesTabs, showHidden: boolean, starredList: string[]) {
  const { tab, split, query, sel, updPane } = tabs;
  const onElevateL = useCallback(() => updPaneRef.current('L', { admin: true }), []);
  const onElevateR = useCallback(() => updPaneRef.current('R', { admin: true }), []);
  const updPaneRef = useRef(updPane);
  updPaneRef.current = updPane;
  const dL = usePaneData({
    loc: tab.left.loc,
    admin: tab.left.admin,
    showHidden,
    starred: starredList,
    query: query.L,
    onElevate: onElevateL,
    enabled: true,
  });
  const dR = usePaneData({
    loc: tab.right.loc,
    admin: tab.right.admin,
    showHidden,
    starred: starredList,
    query: query.R,
    onElevate: onElevateR,
    enabled: split,
  });
  const data = (s: Side): PaneData => (s === 'L' ? dL : dR);
  const selectedEntries = (s: Side): FEntry[] => data(s).entries.filter((e) => sel[s].has(e.path));
  return { data, selectedEntries };
}
