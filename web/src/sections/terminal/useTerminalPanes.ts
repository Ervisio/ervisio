import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react';
import { ApiError, call } from '../../api';
import type { TFn } from '../../i18n';
import { toast } from '../../ui';
import { errMsg } from './terminalErrors';
import type { SessionInfo } from './types';

const LAYOUT_KEY = 'ervisio.term.layout';
const NEW_SESSION_SIZE = { cols: 100, rows: 30 };

export type Panes = [string | null, string | null];
interface Layout {
  open: string[];
  panes: Panes;
  split: boolean;
}

function loadLayout(): Layout {
  try {
    const l = JSON.parse(localStorage.getItem(LAYOUT_KEY) || 'null');
    if (l && Array.isArray(l.open) && Array.isArray(l.panes))
      return {
        open: l.open.filter((x: unknown) => typeof x === 'string'),
        panes: [l.panes[0] ?? null, l.panes[1] ?? null],
        split: !!l.split,
      };
  } catch {
    /* ignore */
  }
  return { open: [], panes: [null, null], split: false };
}

let bootstrapping = false;

// Which sessions are open here and what the two panes show, kept in localStorage.
function useStoredLayout() {
  const [layout, setLayout] = useState<Layout>(loadLayout);
  const patch = useCallback((fn: (l: Layout) => Layout) => setLayout((l) => fn(l)), []);
  useEffect(() => {
    try {
      localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout));
    } catch {
      /* ignore */
    }
  }, [layout]);
  return { layout, patch };
}

const withOpen = (open: string[], id: string) => (open.includes(id) ? open : [...open, id]);

function withoutPane(l: Layout, idx: 0 | 1): Layout {
  const id = l.panes[idx];
  const panes: Panes = [...l.panes];
  panes[idx] = null;
  if (!panes[0] && panes[1]) {
    panes[0] = panes[1];
    panes[1] = null;
  }
  return { ...l, panes, open: id && !panes.includes(id) ? l.open.filter((x) => x !== id) : l.open };
}

function withoutSession(l: Layout, id: string): Layout {
  let panes: Panes = [l.panes[0] === id ? null : l.panes[0], l.panes[1] === id ? null : l.panes[1]];
  if (!panes[0] && panes[1]) panes = [panes[1], null];
  return { ...l, panes, open: l.open.filter((x) => x !== id) };
}

function withSplitToggled(l: Layout, byId: Map<string, SessionInfo>): Layout {
  if (l.split) return { ...l, split: false };
  let second = l.panes[1];
  if (!second || second === l.panes[0])
    second = l.open.find((id) => id !== l.panes[0] && byId.has(id)) ?? null;
  return { ...l, split: true, panes: [l.panes[0], second] };
}

// Drop panes whose session is gone; the first pane falls back to an open session, then the newest one.
function bootPanes(
  panes: Panes,
  open: string[],
  byId: Map<string, SessionInfo>,
  sessions: SessionInfo[],
): Panes {
  const keep = (id: string | null) => (id && byId.has(id) ? id : null);
  const second = keep(panes[1]);
  const first =
    keep(panes[0]) ??
    second ??
    open.find((id) => byId.has(id)) ??
    (sessions.length ? sessions[sessions.length - 1].id : null);
  return [first, second];
}

interface PanesInput {
  t: TFn;
  mobile: boolean;
  sessions: SessionInfo[];
  loaded: boolean;
  loadError: string;
  lastInfo: MutableRefObject<Map<string, SessionInfo>>;
  refresh(): Promise<void>;
}

// Open sessions, the two panes and which one is active, plus starting new sessions into them.
export function useTerminalPanes({
  t,
  mobile,
  sessions,
  loaded,
  loadError,
  lastInfo,
  refresh,
}: PanesInput) {
  const { layout, patch } = useStoredLayout();
  const { open, panes, split } = layout;
  const [active, setActive] = useState<0 | 1>(0);

  const splitShown = split && !mobile;
  const activePane: 0 | 1 = splitShown ? active : 0;
  const byId = useMemo(() => new Map(sessions.map((s) => [s.id, s])), [sessions]);
  const infoOf = (id: string | null) =>
    id ? (byId.get(id) ?? lastInfo.current.get(id)) : undefined;
  const openSessions = open.map((id) => byId.get(id)).filter((s): s is SessionInfo => !!s);
  const detached = sessions.filter((s) => !open.includes(s.id));
  const visible: Panes = splitShown ? panes : [panes[0], null];
  const activeId = visible[active] ?? visible[0];

  const showSession = useCallback(
    (id: string) =>
      patch((l) => {
        const open = withOpen(l.open, id);
        const splitNow = l.split && !mobile;
        const shown = splitNow ? l.panes : [l.panes[0], null];
        const at = shown.indexOf(id);
        if (at >= 0) {
          setActive(at as 0 | 1);
          return { ...l, open };
        }
        const panes: Panes = [...l.panes];
        panes[splitNow ? active : 0] = id;
        return { ...l, open, panes };
      }),
    [patch, active, mobile],
  );

  const create = useCallback(
    async (p: Record<string, unknown>, admin = false) => {
      try {
        const r = await call<{ id: string }>(
          'terminal.create',
          { ...NEW_SESSION_SIZE, ...p },
          { admin },
        );
        await refresh();
        showSession(r.id);
        return r.id;
      } catch (e) {
        const forbidden = e instanceof ApiError && e.code === 'forbidden';
        toast.err(forbidden ? errMsg(e) : t('createFailed', { error: errMsg(e) }));
        return null;
      }
    },
    [refresh, showSession, t],
  );

  // First load: show a surviving or newest session, or start one.
  const didBoot = useRef(false);
  useEffect(() => {
    if (!loaded || loadError || didBoot.current) return;
    didBoot.current = true;
    const [first, second] = bootPanes(panes, open, byId, sessions);
    if (first) {
      patch((l) => ({
        ...l,
        panes: [first, second && second !== first ? second : null],
        open: withOpen(l.open, first),
      }));
    } else if (!bootstrapping && !new URLSearchParams(window.location.search).has('cmd')) {
      // (with ?cmd= the command link handler starts the session)
      bootstrapping = true;
      void create({}).finally(() => (bootstrapping = false));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, loadError]);

  // Forget open ids whose session ended (panes keep showing their "ended" bar until closed).
  useEffect(() => {
    if (!loaded || loadError) return;
    const gone = open.filter((id) => !byId.has(id));
    if (gone.length) patch((l) => ({ ...l, open: l.open.filter((id) => byId.has(id)) }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessions, loaded, loadError]);

  const closePane = useCallback((idx: 0 | 1) => patch((l) => withoutPane(l, idx)), [patch]);
  const detach = useCallback((id: string) => patch((l) => withoutSession(l, id)), [patch]);
  const toggleSplit = () => patch((l) => withSplitToggled(l, byId));

  return {
    split,
    splitShown,
    activePane,
    setActive,
    visible,
    activeId,
    openSessions,
    detached,
    infoOf,
    showSession,
    create,
    closePane,
    detach,
    toggleSplit,
  };
}
