import { useEffect, useRef, useState, type MutableRefObject } from 'react';
import { useSearchParams } from 'react-router-dom';
import type { usePrefs } from '../../api';
import { fcall } from './fapi';
import type { Side } from './filesPageTypes';
import {
  isDirLike,
  type FEntry,
  type PaneState,
  type SortState,
  type TabState,
  type View,
} from './types';
import { dirname, isVirtual, nextId } from './util';
import { isRemoteLocation } from './remoteLocation';

type SetPref = ReturnType<typeof usePrefs>['set'];
type RestoredTabs = { tabs: TabState[]; active: string };

const SORT0: SortState = { key: 'name', dir: 'asc' };
const none = new Set<string>();
const SAVE_TABS_DELAY_MS = 500;
// The bridge validates paths against its platform; accept Windows drive and UNC links too.
const absolutePath = (path: string) =>
  path.startsWith('/') || /^[a-z]:[\\/]/i.test(path) || path.startsWith('\\\\');

const newPane = (loc: string, view: View = 'grid'): PaneState => ({ loc, view, admin: false });
const newTab = (home: string, loc = 'computer:'): TabState => ({
  id: nextId(),
  left: newPane(loc),
  right: newPane(home),
  split: false,
});

function sanitizeTabs(raw: unknown, home: string, forceComputer = false): RestoredTabs | null {
  const arr = Array.isArray(raw) ? raw : raw && typeof raw === 'object' ? (raw as any).tabs : null;
  if (!Array.isArray(arr) || arr.length === 0) return null;
  const okLoc = (l: unknown) =>
    typeof l === 'string' &&
    (absolutePath(l) ||
      isRemoteLocation(l) ||
      ['computer:', 'trash:', 'recent:', 'starred:'].includes(l))
      ? l
      : home;
  const okView = (v: unknown): View => (v === 'list' ? 'list' : 'grid');
  const tabs: TabState[] = arr.slice(0, 12).map((x: any) => ({
    id: typeof x?.id === 'string' ? x.id : nextId(),
    left: { loc: okLoc(x?.left?.loc), view: okView(x?.left?.view), admin: false },
    right: { loc: okLoc(x?.right?.loc), view: okView(x?.right?.view), admin: false },
    split: !!x?.split,
  }));
  let active =
    !Array.isArray(raw) &&
    typeof (raw as any)?.active === 'string' &&
    tabs.some((x) => x.id === (raw as any).active)
      ? (raw as any).active
      : tabs[0].id;
  // One-time landing migration keeps existing folder tabs available, while opening
  // This PC first. Explicit ?path= links are applied afterwards and take precedence.
  if (
    forceComputer ||
    !raw ||
    typeof raw !== 'object' ||
    (raw as { landingVersion?: number }).landingVersion !== 2
  ) {
    const existing = tabs.find((x) => x.left.loc === 'computer:');
    if (existing) active = existing.id;
    else {
      const landing = newTab(home);
      tabs.unshift(landing);
      active = landing.id;
    }
  }
  return { tabs: tabs.slice(0, 12), active };
}

/** Applies the saved tabs once prefs are ready; the returned ref is true from then on. */
function useRestoredTabs(
  ready: boolean,
  saved: unknown,
  home: string,
  apply: (r: RestoredTabs) => void,
  forceComputer = false,
): MutableRefObject<boolean> {
  const restored = useRef(false);
  useEffect(() => {
    if (!ready || restored.current) return;
    restored.current = true;
    const r = sanitizeTabs(saved, home, forceComputer);
    if (r) apply(r);
  }, [ready]); // eslint-disable-line react-hooks/exhaustive-deps
  return restored;
}

/** ?path=/some/dir or ?path=/some/file (links from Overview and Logs): opens it, then drops the parameter. */
function useLinkPath(ready: boolean, open: (dir: string, select?: string) => void) {
  const [params, setParams] = useSearchParams();
  const linkPath = params.get('path');
  useEffect(() => {
    if (!ready || !linkPath || (!absolutePath(linkPath) && !isRemoteLocation(linkPath))) return;
    let live = true;
    const openOnce = (dir: string, select?: string) => {
      if (!live) return;
      open(dir, select);
      setParams(
        (p) => {
          const n = new URLSearchParams(p);
          n.delete('path');
          return n;
        },
        { replace: true },
      );
    };
    fcall<FEntry>('files.stat', { path: linkPath }).then(
      (e) => (isDirLike(e) ? openOnce(e.path || linkPath) : openOnce(dirname(linkPath), linkPath)),
      () => openOnce(dirname(linkPath), linkPath),
    );
    return () => {
      live = false;
    };
  }, [ready, linkPath]); // eslint-disable-line react-hooks/exhaustive-deps
}

function useSaveTabs(
  restored: MutableRefObject<boolean>,
  tabs: TabState[],
  activeId: string,
  set: SetPref,
) {
  useEffect(() => {
    if (!restored.current) return;
    const h = window.setTimeout(() => {
      void set('files.tabs', {
        landingVersion: 2,
        active: activeId,
        tabs: tabs.map((x) => ({
          id: x.id,
          split: x.split,
          left: { loc: x.left.loc, view: x.left.view },
          right: { loc: x.right.loc, view: x.right.view },
        })),
      }).catch(() => undefined);
    }, SAVE_TABS_DELAY_MS);
    return () => window.clearTimeout(h);
  }, [tabs, activeId]); // eslint-disable-line react-hooks/exhaustive-deps
}

interface TabsArgs {
  home: string;
  mobile: boolean;
  ready: boolean;
  savedTabs: unknown;
  set: SetPref;
}

/** Tabs, the two panes of the active tab, and each pane's selection, search and sort. */
export function useFilesTabs({ home, mobile, ready, savedTabs, set }: TabsArgs) {
  // A read-only window landing hint; consuming it would navigate the outer
  // desktop router and could create an unwanted second Explorer window.
  const [params] = useSearchParams();
  const [tabs, setTabs] = useState<TabState[]>(() => [newTab(home)]);
  const [activeId, setActiveId] = useState(() => tabs[0].id);
  const [act, setAct] = useState<Side>('L');
  const [sel, setSel] = useState<Record<Side, ReadonlySet<string>>>({ L: none, R: none });
  const [query, setQuery] = useState<Record<Side, string>>({ L: '', R: '' });
  const [sort, setSort] = useState<Record<Side, SortState>>({ L: SORT0, R: SORT0 });

  const restored = useRestoredTabs(
    ready,
    savedTabs,
    home,
    (r) => {
      setTabs(r.tabs);
      setActiveId(r.active);
    },
    params.get('view') === 'computer' || !params.has('path'),
  );
  useLinkPath(ready, (dir, select) => {
    const nt = newTab(home, dir);
    setTabs((ts) => {
      const cur = ts.find((x) => x.left.loc === dir && !x.split);
      if (cur) {
        setActiveId(cur.id);
        return ts;
      }
      setActiveId(nt.id);
      return [...ts, nt].slice(-12);
    });
    setAct('L');
    setQuery({ L: '', R: '' });
    setSel({ L: select ? new Set([select]) : none, R: none });
  });
  useSaveTabs(restored, tabs, activeId, set);

  const tab = tabs.find((x) => x.id === activeId) ?? tabs[0];
  const split = tab.split && !mobile;
  const side: Side = split ? act : 'L';
  const pane = (s: Side) => (s === 'L' ? tab.left : tab.right);
  const updPane = (s: Side, patch: Partial<PaneState>) =>
    setTabs((ts) =>
      ts.map((x) =>
        x.id === tab.id
          ? {
              ...x,
              [s === 'L' ? 'left' : 'right']: { ...(s === 'L' ? x.left : x.right), ...patch },
            }
          : x,
      ),
    );
  const elevate = (s: Side) => () => {
    if (!isRemoteLocation(pane(s).loc)) updPane(s, { admin: true });
  };
  const toggleSplit = () =>
    setTabs((ts) => ts.map((x) => (x.id === tab.id ? { ...x, split: !x.split } : x)));
  const clearSel = (s?: Side) => setSel((c) => (s ? { ...c, [s]: none } : { L: none, R: none }));
  const adminOf = (s: Side) => pane(s).admin;
  const writableLoc = (s: Side) => {
    const l = pane(s).loc;
    return isVirtual(l) ? null : l;
  };

  const navigate = (s: Side, loc: string) => {
    const l = loc === 'home:' ? home : loc;
    updPane(s, { loc: l, ...(isRemoteLocation(l) ? { admin: false } : {}) });
    setQuery((q) => ({ ...q, [s]: '' }));
    clearSel(s);
  };
  const openTab = (loc?: string) => {
    const nt = newTab(home, loc ?? home);
    setTabs((ts) => [...ts, nt]);
    setActiveId(nt.id);
    setAct('L');
    clearSel();
  };
  const closeTab = (id: string) => {
    setTabs((ts) => {
      if (ts.length === 1) return [newTab(home)];
      const i = ts.findIndex((x) => x.id === id);
      const rest = ts.filter((x) => x.id !== id);
      if (id === activeId) setActiveId(rest[Math.max(0, i - 1)].id);
      return rest;
    });
    clearSel();
  };
  const switchTab = (id: string) => {
    setActiveId(id);
    setAct('L');
    clearSel();
    setQuery({ L: '', R: '' });
  };

  return {
    tabs,
    tab,
    split,
    side,
    sel,
    setSel,
    query,
    setQuery,
    sort,
    setSort,
    setAct,
    pane,
    updPane,
    elevate,
    toggleSplit,
    clearSel,
    adminOf,
    writableLoc,
    navigate,
    openTab,
    closeTab,
    switchTab,
  };
}

export type FilesTabs = ReturnType<typeof useFilesTabs>;
