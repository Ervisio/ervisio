import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from 'react';
import { useSearchParams } from 'react-router-dom';
import { ApiError, call, usePrefs, useSession } from '../../api';
import { useI18n, useT, type TFn } from '../../i18n';
import { relativeTime } from '../../lib/format';
import {
  Button,
  ConfirmDialog,
  EmptyState,
  Icon,
  Page,
  Sheet,
  Tooltip,
  toast,
  useIsMobile,
  useMediaQuery,
  type IconName,
} from '../../ui';
import { useFocusMode, usePaletteActions } from '../../sections';
import { HostDialog, RenameDialog, SnippetDialog } from './Dialogs';
import ExtraKeys from './ExtraKeys';
import Palette, { type PaletteItem } from './Palette';
import Sidebar from './Sidebar';
import Snippets, { snippetLook } from './Snippets';
import TerminalView from './TerminalView';
import {
  newId,
  type Host,
  type HistoryItem,
  type Kind,
  type SessionInfo,
  type Snippet,
  type TermHandle,
} from './types';
import './terminal.css';

const LAYOUT_KEY = 'ervisio.term.layout';
type Panes = [string | null, string | null];
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

const errMsg = (e: unknown) => (e instanceof ApiError ? e.message : String(e));
let bootstrapping = false;

const REFRESH_MS = 4000;
const DEFAULT_FONT_SIZE = 14;
const HISTORY_LIMIT = 50;
const LINK_CMD_MAX = 1000;
const NEW_SESSION_SIZE = { cols: 100, rows: 30 };
const KIND_DOT = 'terminal-dot terminal-k-';

type SetBool = Dispatch<SetStateAction<boolean>>;
type SessionList = { sessions: Omit<SessionInfo, 'admin'>[] };
interface TerminalOpts {
  theme?: string;
  fontSize?: number;
  copyOnSelect?: boolean;
  snippets?: boolean;
}

function builtinSnippets(t: TFn, win: boolean): Snippet[] {
  return win
    ? [
        { id: 'd1', title: t('defaults.disk'), command: 'Get-PSDrive -PSProvider FileSystem' },
        {
          id: 'd2',
          title: t('defaults.memory'),
          command:
            'Get-CimInstance Win32_OperatingSystem | Select FreePhysicalMemory,TotalVisibleMemorySize',
        },
        {
          id: 'd3',
          title: t('defaults.top'),
          command: 'Get-Process | Sort-Object CPU -Descending | Select -First 15',
        },
        { id: 'd4', title: t('defaults.ports'), command: 'Get-NetTCPConnection -State Listen' },
        {
          id: 'd5',
          title: t('defaults.failed'),
          command: "Get-Service | ? {$_.StartType -eq 'Automatic' -and $_.Status -ne 'Running'}",
        },
        {
          id: 'd6',
          title: t('defaults.journal'),
          command: 'Get-WinEvent -LogName System -MaxEvents 50',
        },
      ]
    : [
        { id: 'd1', title: t('defaults.disk'), command: 'df -h' },
        { id: 'd2', title: t('defaults.memory'), command: 'free -h' },
        { id: 'd3', title: t('defaults.top'), command: 'ps aux --sort=-%cpu | head -15' },
        { id: 'd4', title: t('defaults.ports'), command: 'ss -tulpn' },
        { id: 'd5', title: t('defaults.failed'), command: 'systemctl --failed' },
        { id: 'd6', title: t('defaults.journal'), command: 'journalctl -f' },
      ];
}

// Settings writes the `terminal` object; hosts, history and snippets have their own keys.
function useTerminalPrefs(t: TFn, win: boolean) {
  const { prefs, set, ready } = usePrefs();
  const opts = (prefs.terminal ?? {}) as TerminalOpts;
  const hosts: Host[] = Array.isArray(prefs['terminal.hosts']) ? prefs['terminal.hosts'] : [];
  const history: HistoryItem[] = Array.isArray(prefs['terminal.history'])
    ? prefs['terminal.history']
    : [];
  const defaultSnippets = useMemo(() => builtinSnippets(t, win), [t, win]);
  const customSnippets = Array.isArray(prefs['terminal.snippets'])
    ? (prefs['terminal.snippets'] as Snippet[])
    : null;
  const save = useCallback(
    (key: string, value: unknown) =>
      set(key, value).catch((e) => toast.err(t('saveFailed', { error: errMsg(e) }))),
    [set, t],
  );
  return {
    ready,
    opts,
    hosts,
    history,
    save,
    snippets: customSnippets ?? defaultSnippets,
    fontSize: Number(opts.fontSize) > 0 ? Number(opts.fontSize) : DEFAULT_FONT_SIZE,
    copyOnSelect: opts.copyOnSelect !== false,
    themeMode: opts.theme === 'dark' || opts.theme === 'light' ? opts.theme : 'app',
    showSnippets: opts.snippets !== false,
  };
}

async function listSessions(withRoot: boolean): Promise<SessionInfo[]> {
  const mine = await call<SessionList>('terminal.list');
  const all: SessionInfo[] = (mine.sessions ?? []).map((s) => ({ ...s, admin: false }));
  if (!withRoot) return all;
  try {
    const root = await call<SessionList>('terminal.list', {}, { admin: true });
    return all.concat(
      (root.sessions ?? []).map((s) => ({ ...s, kind: 'root' as const, admin: true })),
    );
  } catch {
    // the root bridge may have just locked
    return all;
  }
}

// Sessions from the user bridge (and the root bridge while unlocked), polled while the page is visible.
function useTerminalSessions(adminOk: boolean) {
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState('');
  const lastInfo = useRef(new Map<string, SessionInfo>());

  const refresh = useCallback(async () => {
    try {
      const all = await listSessions(adminOk);
      all.forEach((s) => lastInfo.current.set(s.id, s));
      setSessions(all);
      setLoadError('');
      setLoaded(true);
    } catch (e) {
      setLoadError(errMsg(e));
      setLoaded(true);
    }
  }, [adminOk]);

  useEffect(() => {
    void refresh();
    const id = window.setInterval(() => {
      if (!document.hidden) void refresh();
    }, REFRESH_MS);
    return () => window.clearInterval(id);
  }, [refresh]);

  return { sessions, loaded, loadError, lastInfo, refresh };
}

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

// ?cmd=… (links from other sections, e.g. Software's AUR "Open in terminal"):
// a new session with the command typed at the prompt, not run, so the user reviews it first.
function useCommandLink(
  loaded: boolean,
  loadError: string,
  create: (p: Record<string, unknown>) => Promise<string | null>,
) {
  const [params, setParams] = useSearchParams();
  const linkCmd = params.get('cmd');
  const [pendingInput, setPendingInput] = useState<{ id: string; text: string } | null>(null);
  useEffect(() => {
    if (!loaded || loadError || !linkCmd) return;
    const cmd = linkCmd.replace(/[\r\n]+/g, ' ').slice(0, LINK_CMD_MAX);
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        n.delete('cmd');
        return n;
      },
      { replace: true },
    );
    void create({}).then((id) => id && setPendingInput({ id, text: cmd }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, loadError, linkCmd]);
  return { pendingInput, setPendingInput };
}

function usePaletteShortcut(setPaletteOpen: SetBool) {
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.shiftKey && !e.altKey && e.code === 'KeyP') {
        e.preventDefault();
        e.stopPropagation();
        setPaletteOpen((o) => !o);
      }
    };
    window.addEventListener('keydown', on, true);
    return () => window.removeEventListener('keydown', on, true);
  }, [setPaletteOpen]);
}

function useFocusExitKeys(focus: boolean, setFocus: SetBool) {
  useEffect(() => {
    if (!focus) return;
    const on = (e: KeyboardEvent) => {
      if (e.key === 'F11' || (e.ctrlKey && e.shiftKey && e.code === 'KeyF')) {
        e.preventDefault();
        setFocus(false);
      }
    };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  }, [focus, setFocus]);
}

// Keyboard on phones: keep the page above the on-screen keyboard.
function useKeyboardSafeHeight(mobile: boolean) {
  const rootRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv || !mobile) return;
    const on = () => rootRef.current?.style.setProperty('--term-vh', `${vv.height}px`);
    on();
    vv.addEventListener('resize', on);
    return () => vv.removeEventListener('resize', on);
  }, [mobile]);
  return rootRef;
}

function kindLook(kind: Kind): { icon: IconName; hue: string } {
  if (kind === 'root') return { icon: 'lock', hue: 'svc' };
  if (kind === 'ssh') return { icon: 'server', hue: 'ov' };
  return { icon: 'terminal', hue: 'term' };
}

function sudoLeftLabel(t: TFn, forever: boolean, left: number) {
  if (forever) return t('sudoForever');
  if (left >= 60) return t('sudoMin', { count: Math.ceil(left / 60) });
  return t('sudoSec', { count: left });
}

function prettyTitle(s: SessionInfo | undefined, home: string | undefined) {
  const raw = s?.title || s?.name || '';
  return home && home !== '/' ? raw.split(home).join('~') : raw;
}

function EmptyPane({
  isActive,
  onActivate,
  onNew,
}: {
  isActive: boolean;
  onActivate(): void;
  onNew(): void;
}) {
  const t = useT('terminal');
  return (
    <div className="terminal-pane" onMouseDown={onActivate}>
      <div className={`terminal-pane-h${isActive ? ' act' : ''}`}>{t('emptyPane')}</div>
      <div className="terminal-pane-empty">
        <Button icon="plus" onClick={onNew}>
          {t('newSession')}
        </Button>
      </div>
    </div>
  );
}

function MobileStrip({
  sessions,
  activeId,
  onShow,
  onNew,
  onMenu,
}: {
  sessions: SessionInfo[];
  activeId: string | null;
  onShow(id: string): void;
  onNew(): void;
  onMenu(): void;
}) {
  const t = useT('terminal');
  return (
    <div className="terminal-strip" role="tablist" aria-label={t('sessions')}>
      {sessions.map((s) => (
        <button
          key={s.id}
          type="button"
          role="tab"
          aria-selected={s.id === activeId}
          className={`terminal-tab${s.id === activeId ? ' on' : ''}`}
          onClick={() => onShow(s.id)}
        >
          <span className={`${KIND_DOT}${s.kind}`} />
          {s.name}
        </button>
      ))}
      <button
        type="button"
        className="terminal-tab terminal-tab-add"
        aria-label={t('newSession')}
        onClick={onNew}
      >
        <Icon name="plus" />
      </button>
      <button
        type="button"
        className="terminal-tab terminal-tab-add"
        aria-label={t('sessions')}
        onClick={onMenu}
      >
        <Icon name="menu" />
      </button>
    </div>
  );
}

function ToolToggle({
  on,
  title,
  icon,
  label,
  onClick,
}: {
  on: boolean;
  title: string;
  icon: IconName;
  label: string;
  onClick(): void;
}) {
  return (
    <button
      type="button"
      className={on ? 'on' : ''}
      aria-pressed={on}
      title={title}
      onClick={onClick}
    >
      <Icon name={icon} />
      <span>{label}</span>
    </button>
  );
}

interface ToolbarProps {
  active?: SessionInfo;
  /** null while the session is locked */
  sudoLeft: string | null;
  onLock(): void;
  showMenu: boolean;
  onMenu(): void;
  showSplit: boolean;
  split: boolean;
  onSplit(): void;
  paletteOpen: boolean;
  onPalette(): void;
  focus: boolean;
  onFocus(): void;
}

function Toolbar(p: ToolbarProps) {
  const t = useT('terminal');
  return (
    <div className="terminal-tb">
      <span className="terminal-ttl">
        <span className={`${KIND_DOT}${p.active?.kind ?? 'local'}`} />
        {p.active?.name ?? t('title')}
      </span>
      {p.sudoLeft !== null && (
        <Tooltip label={t('lockNow')}>
          <button
            type="button"
            className="terminal-sudo"
            onClick={p.onLock}
            aria-label={`${t('sudoChip', { left: p.sudoLeft })}. ${t('lockNow')}`}
          >
            <Icon name="lock" />
            {t('sudoChip', { left: p.sudoLeft })}
          </button>
        </Tooltip>
      )}
      <span className="sp" />
      {p.showMenu && (
        <button type="button" onClick={p.onMenu} title={t('sessions')}>
          <Icon name="menu" />
          <span>{t('sessions')}</span>
        </button>
      )}
      {p.showSplit && (
        <ToolToggle
          on={p.split}
          title={t('splitTip')}
          icon="columns"
          label={t('split')}
          onClick={p.onSplit}
        />
      )}
      <ToolToggle
        on={p.paletteOpen}
        title={t('paletteTip')}
        icon="search"
        label={t('palette.title')}
        onClick={p.onPalette}
      />
      <ToolToggle
        on={p.focus}
        title={t('focusTip')}
        icon="overview"
        label={t('focus')}
        onClick={p.onFocus}
      />
    </div>
  );
}

export default function TerminalPage() {
  const t = useT('terminal');
  const { lang } = useI18n();
  const { session, isUnlocked, unlockLeft, unlockForever, lock } = useSession();
  const mobile = useIsMobile();
  const compact = useMediaQuery('(max-width: 1100px)');
  const {
    ready,
    opts,
    hosts,
    history,
    save,
    snippets,
    fontSize,
    copyOnSelect,
    themeMode,
    showSnippets,
  } = useTerminalPrefs(t, session?.os === 'windows');

  const adminOk = isUnlocked && unlockLeft > 2 && !session?.isRoot;
  const { sessions, loaded, loadError, lastInfo, refresh } = useTerminalSessions(adminOk);

  const { layout, patch } = useStoredLayout();
  const { open, panes, split } = layout;
  const [active, setActive] = useState<0 | 1>(0);
  const [focus, setFocus] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [sheet, setSheet] = useState(false);
  const [ctrl, setCtrl] = useState(false);
  const handles = useRef<[TermHandle | null, TermHandle | null]>([null, null]);

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
      // (with ?cmd= the link handler below starts the session)
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

  const [renaming, setRenaming] = useState<SessionInfo | null>(null);
  const [killing, setKilling] = useState<SessionInfo | null>(null);
  const [hostDlg, setHostDlg] = useState<{ mode: 'connect' | 'edit'; host: Host | null } | null>(
    null,
  );
  const [snipDlg, setSnipDlg] = useState<{ snippet?: Snippet; command?: string } | null>(null);

  const doRename = async (name: string) => {
    const s = renaming;
    setRenaming(null);
    if (!s) return;
    try {
      await call('terminal.rename', { id: s.id, name }, { admin: s.admin });
      toast.ok(t('renamed', { name }));
      void refresh();
    } catch (e) {
      toast.err(t('renameFailed', { error: errMsg(e) }));
    }
  };
  const doKill = async () => {
    const s = killing;
    setKilling(null);
    if (!s) return;
    try {
      await call('terminal.kill', { id: s.id }, { admin: s.admin });
      toast.ok(t('killed', { name: s.name }));
    } catch (e) {
      if (!(e instanceof ApiError && e.code === 'not_found'))
        toast.err(t('killFailed', { error: errMsg(e) }));
    }
    void refresh();
  };

  const connectSsh = (h: Omit<Host, 'id'>) =>
    void create({ kind: 'ssh', host: h.host, user: h.user, port: h.port, name: h.name });
  const connectHost = (h: Host) => {
    const existing = sessions.find((s) => s.kind === 'ssh' && s.name === h.name);
    if (existing) return showSession(existing.id);
    connectSsh(h);
  };
  const submitHost = (h: Omit<Host, 'id'> & { id?: string }, saveIt: boolean) => {
    const mode = hostDlg?.mode;
    setHostDlg(null);
    if (saveIt || mode === 'edit') {
      const entry: Host = {
        id: h.id ?? newId(),
        name: h.name,
        host: h.host,
        user: h.user,
        port: h.port,
      };
      const next = hosts.some((x) => x.id === entry.id)
        ? hosts.map((x) => (x.id === entry.id ? entry : x))
        : [...hosts, entry];
      void save('terminal.hosts', next);
      if (mode === 'edit') return toast.ok(t('hostSaved', { name: entry.name }));
    }
    connectSsh(h);
  };
  const deleteHost = (h: Host) => {
    void save(
      'terminal.hosts',
      hosts.filter((x) => x.id !== h.id),
    );
    toast.undo(
      t('hostDeleted', { name: h.name }),
      t('undo'),
      () => void save('terminal.hosts', hosts),
    );
  };

  const { pendingInput, setPendingInput } = useCommandLink(loaded, loadError, create);

  const target = (): TermHandle | null => handles.current[activePane] ?? handles.current[0];
  const remember = (cmd: string) => {
    const c = cmd.trim();
    if (!c) return;
    void save(
      'terminal.history',
      [{ cmd: c, at: Date.now() }, ...history.filter((h) => h.cmd !== c)].slice(0, HISTORY_LIMIT),
    );
  };
  const runCmd = (cmd: string) => {
    const h = target();
    if (!h) return toast.info(t('noSession'));
    h.run(cmd);
    remember(cmd);
  };
  const pasteCmd = (cmd: string) => {
    const h = target();
    if (!h) return toast.info(t('noSession'));
    h.paste(cmd);
  };

  const saveSnippet = (title: string, command: string) => {
    const edit = snipDlg?.snippet;
    setSnipDlg(null);
    const next =
      edit && snippets.some((s) => s.id === edit.id)
        ? snippets.map((s) => (s.id === edit.id ? { ...s, title, command } : s))
        : [...snippets, { id: newId(), title, command }];
    void save('terminal.snippets', next);
    toast.ok(t('snippet.saved', { name: title }));
  };
  const deleteSnippet = (s: Snippet) => {
    void save(
      'terminal.snippets',
      snippets.filter((x) => x.id !== s.id),
    );
    toast.undo(
      t('snippet.deleted', { name: s.title }),
      t('undo'),
      () => void save('terminal.snippets', snippets),
    );
  };
  const setShowSnippets = (v: boolean) => {
    void save('terminal', { ...opts, snippets: v });
    void save('terminal.showSnippets', v);
  };

  usePaletteShortcut(setPaletteOpen);
  useFocusMode('terminal', focus);
  useFocusExitKeys(focus, setFocus);
  const rootRef = useKeyboardSafeHeight(mobile);

  const newLocal = () => void create({});
  const newRoot = () => void create({}, true);
  const newSsh = () => setHostDlg({ mode: 'connect', host: null });
  const toggleFocus = () => setFocus((f) => !f);

  usePaletteActions('terminal', [
    {
      id: 'terminal.new',
      title: t('newSession'),
      hint: t('title'),
      icon: 'plus',
      hue: 'term',
      keywords: ['shell', 'terminal'],
      run: newLocal,
    },
    {
      id: 'terminal.root',
      title: t('newRoot'),
      hint: t('title'),
      icon: 'lock',
      hue: 'term',
      keywords: ['sudo', 'admin'],
      run: newRoot,
    },
    {
      id: 'terminal.focus',
      title: t('focusMode'),
      hint: t('title'),
      icon: 'overview',
      hue: 'term',
      run: toggleFocus,
    },
  ]);

  const paletteItems: PaletteItem[] = [
    ...snippets.map((s) => {
      const look = snippetLook(s);
      return {
        id: 'sn' + s.id,
        group: 'snippets' as const,
        title: s.title,
        sub: s.command,
        icon: look.icon,
        hue: look.hue,
        run: () => runCmd(s.command),
        paste: () => pasteCmd(s.command),
      };
    }),
    ...sessions.map((s) => ({
      id: 'se' + s.id,
      group: 'sessions' as const,
      title: t('palette.openName', { name: s.name }),
      sub: t('palette.switch'),
      ...kindLook(s.kind),
      keywords: s.kind,
      run: () => showSession(s.id),
    })),
    ...history.map((h, i) => ({
      id: 'hi' + i,
      group: 'history' as const,
      title: h.cmd,
      sub: relativeTime(h.at, lang),
      icon: 'clock' as const,
      hue: 'sw',
      run: () => runCmd(h.cmd),
      paste: () => pasteCmd(h.cmd),
    })),
    {
      id: 'a-new',
      group: 'actions' as const,
      title: t('newSession'),
      icon: 'plus' as const,
      hue: 'term',
      run: newLocal,
    },
    {
      id: 'a-root',
      group: 'actions' as const,
      title: t('newRoot'),
      icon: 'lock' as const,
      hue: 'svc',
      keywords: 'sudo admin',
      run: newRoot,
    },
    {
      id: 'a-ssh',
      group: 'actions' as const,
      title: t('newSsh'),
      icon: 'server' as const,
      hue: 'ov',
      run: newSsh,
    },
    ...(mobile
      ? []
      : [
          {
            id: 'a-split',
            group: 'actions' as const,
            title: split ? t('splitOff') : t('splitOn'),
            icon: 'columns' as const,
            hue: 'term',
            run: toggleSplit,
          },
        ]),
    {
      id: 'a-focus',
      group: 'actions' as const,
      title: focus ? t('focusOff') : t('focusMode'),
      icon: 'overview' as const,
      hue: 'term',
      run: toggleFocus,
    },
    {
      id: 'a-save',
      group: 'actions' as const,
      title: t('snippet.add'),
      icon: 'command' as const,
      hue: 'term',
      run: () => setSnipDlg({}),
    },
    {
      id: 'a-chips',
      group: 'actions' as const,
      title: showSnippets ? t('hideSnippets') : t('showSnippets'),
      icon: 'eye' as const,
      hue: 'term',
      run: () => setShowSnippets(!showSnippets),
    },
  ];

  const paneOf: Record<string, number> = {};
  if (splitShown) visible.forEach((id, i) => id && (paneOf[id] = i + 1));
  const connectedHosts = new Set(sessions.filter((s) => s.kind === 'ssh').map((s) => s.name));
  const closeSheetThen =
    <A extends unknown[]>(fn: (...args: A) => void) =>
    (...args: A) => {
      setSheet(false);
      fn(...args);
    };
  const sidebar = (
    <Sidebar
      open={openSessions}
      detached={detached}
      hosts={hosts}
      paneOf={paneOf}
      activeId={activeId ?? null}
      connectedHosts={connectedHosts}
      onNew={closeSheetThen(newLocal)}
      onNewRoot={closeSheetThen(newRoot)}
      onNewSsh={closeSheetThen(newSsh)}
      onOpen={closeSheetThen((s: SessionInfo) => showSession(s.id))}
      onDetach={(s) => detach(s.id)}
      onRename={setRenaming}
      onKill={setKilling}
      onConnect={closeSheetThen((h: Host) => connectHost(h))}
      onEditHost={(h) => setHostDlg({ mode: 'edit', host: h })}
      onDeleteHost={deleteHost}
    />
  );

  const paneView = (idx: 0 | 1) => {
    const id = visible[idx];
    const isActive = activePane === idx;
    if (!id) {
      return (
        <EmptyPane
          key={`empty${idx}`}
          isActive={isActive}
          onActivate={() => setActive(idx)}
          onNew={() => {
            setActive(idx);
            newLocal();
          }}
        />
      );
    }
    const info = infoOf(id);
    return (
      <div className="terminal-pane" key={id}>
        <div className={`terminal-pane-h${isActive ? ' act' : ''}`}>
          <span className="terminal-pane-t">{prettyTitle(info, session?.home)}</span>
          <span className="sp" />
          <button
            type="button"
            aria-label={t('closePane')}
            title={t('closePane')}
            onClick={() => closePane(idx)}
          >
            <Icon name="close" />
          </button>
        </div>
        <TerminalView
          ref={(h) => {
            handles.current[idx] = h;
          }}
          id={id}
          admin={!!info?.admin}
          fontSize={fontSize}
          copyOnSelect={copyOnSelect}
          themeMode={themeMode}
          ctrl={ctrl && isActive}
          onCtrlUsed={() => setCtrl(false)}
          onActivate={() => setActive(idx)}
          onEnded={() => void refresh()}
          onClosePane={() => closePane(idx)}
          initialInput={pendingInput?.id === id ? pendingInput.text : undefined}
          onInitialInputUsed={() => setPendingInput(null)}
        />
      </div>
    );
  };

  const paneArea = () => {
    if (loadError && !sessions.length) {
      return (
        <div className="terminal-center">
          <EmptyState
            icon="alert"
            hue="term"
            title={t('loadFailed')}
            text={loadError}
            action={<Button onClick={() => void refresh()}>{t('retry')}</Button>}
          />
        </div>
      );
    }
    if (!visible[0] && !visible[1]) {
      return (
        <div className="terminal-center">
          {loaded && ready ? (
            <EmptyState
              icon="terminal"
              hue="term"
              title={t('noneTitle')}
              text={t('noneText')}
              action={
                <Button variant="primary" icon="plus" onClick={newLocal}>
                  {t('newSession')}
                </Button>
              }
            />
          ) : null}
        </div>
      );
    }
    return (
      <div className="terminal-panes">
        {paneView(0)}
        {splitShown && paneView(1)}
      </div>
    );
  };

  const rootClass = [
    'terminal-root',
    focus && 'terminal-focus',
    splitShown && 'terminal-split',
    mobile && 'terminal-mobile',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <Page flush hue="term">
      <div ref={rootRef} className={rootClass}>
        {!compact && !focus && sidebar}
        <div className="terminal-body">
          {mobile && (
            <MobileStrip
              sessions={openSessions}
              activeId={activeId}
              onShow={showSession}
              onNew={newLocal}
              onMenu={() => setSheet(true)}
            />
          )}
          <Toolbar
            active={infoOf(activeId ?? null)}
            sudoLeft={isUnlocked ? sudoLeftLabel(t, unlockForever, unlockLeft) : null}
            onLock={() => void lock()}
            showMenu={compact && !mobile}
            onMenu={() => setSheet(true)}
            showSplit={!mobile}
            split={split}
            onSplit={toggleSplit}
            paletteOpen={paletteOpen}
            onPalette={() => setPaletteOpen((o) => !o)}
            focus={focus}
            onFocus={toggleFocus}
          />

          {paneArea()}

          {showSnippets && (
            <Snippets
              snippets={snippets}
              onPaste={(s) => pasteCmd(s.command)}
              onRun={(s) => runCmd(s.command)}
              onEdit={(s) => setSnipDlg({ snippet: s })}
              onDelete={deleteSnippet}
              onAdd={() => setSnipDlg({})}
            />
          )}
          {mobile && <ExtraKeys ctrl={ctrl} onCtrl={setCtrl} term={target} />}
        </div>

        {paletteOpen && (
          <Palette
            items={paletteItems}
            onClose={() => {
              setPaletteOpen(false);
              window.setTimeout(() => target()?.focus(), 0);
            }}
          />
        )}
      </div>

      <Sheet
        open={sheet && (compact || mobile)}
        onClose={() => setSheet(false)}
        title={t('sessions')}
      >
        <div className="terminal-sheet">{sidebar}</div>
      </Sheet>
      <RenameDialog
        open={!!renaming}
        name={renaming?.name ?? ''}
        onClose={() => setRenaming(null)}
        onSave={doRename}
      />
      <ConfirmDialog
        open={!!killing}
        onClose={() => setKilling(null)}
        onConfirm={doKill}
        title={t('killTitle', { name: killing?.name ?? '' })}
        description={t('killText')}
        confirmLabel={t('kill')}
      />
      <HostDialog
        open={!!hostDlg}
        mode={hostDlg?.mode ?? 'connect'}
        initial={hostDlg?.host}
        onClose={() => setHostDlg(null)}
        onSubmit={submitHost}
      />
      <SnippetDialog
        open={!!snipDlg}
        initial={snipDlg?.snippet ?? (snipDlg?.command ? { command: snipDlg.command } : undefined)}
        onClose={() => setSnipDlg(null)}
        onSave={saveSnippet}
      />
    </Page>
  );
}
