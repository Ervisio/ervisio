import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ApiError, call, usePrefs, useSession } from '../../api';
import { useI18n, useT } from '../../i18n';
import { relativeTime } from '../../lib/format';
import { Button, ConfirmDialog, EmptyState, Icon, Page, Sheet, Tooltip, toast, useIsMobile, useMediaQuery } from '../../ui';
import { useFocusMode, usePaletteActions } from '../../sections';
import { HostDialog, RenameDialog, SnippetDialog } from './Dialogs';
import ExtraKeys from './ExtraKeys';
import Palette, { type PaletteItem } from './Palette';
import Sidebar from './Sidebar';
import Snippets, { snippetLook } from './Snippets';
import TerminalView from './TerminalView';
import { newId, type Host, type HistoryItem, type SessionInfo, type Snippet, type TermHandle } from './types';
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
    if (l && Array.isArray(l.open) && Array.isArray(l.panes)) return { open: l.open.filter((x: unknown) => typeof x === 'string'), panes: [l.panes[0] ?? null, l.panes[1] ?? null], split: !!l.split };
  } catch {
    /* ignore */
  }
  return { open: [], panes: [null, null], split: false };
}

const errMsg = (e: unknown) => (e instanceof ApiError ? e.message : String(e));
let bootstrapping = false;

export default function TerminalPage() {
  const t = useT('terminal');
  const { lang } = useI18n();
  const { prefs, set, ready } = usePrefs();
  const { session, isUnlocked, unlockLeft, unlockForever, lock } = useSession();
  const mobile = useIsMobile();
  const compact = useMediaQuery('(max-width: 1100px)');

  // ----- preferences (Settings writes the `terminal` object) -----
  const opts = (prefs.terminal ?? {}) as { theme?: string; fontSize?: number; copyOnSelect?: boolean; snippets?: boolean };
  const fontSize = Number(opts.fontSize) > 0 ? Number(opts.fontSize) : 14;
  const copyOnSelect = opts.copyOnSelect !== false;
  const themeMode = opts.theme === 'dark' || opts.theme === 'light' ? opts.theme : 'app';
  const showSnippets = opts.snippets !== false;
  const hosts: Host[] = Array.isArray(prefs['terminal.hosts']) ? prefs['terminal.hosts'] : [];
  const history: HistoryItem[] = Array.isArray(prefs['terminal.history']) ? prefs['terminal.history'] : [];
  const defaultSnippets: Snippet[] = useMemo(
    () => [
      { id: 'd1', title: t('defaults.disk'), command: 'df -h' },
      { id: 'd2', title: t('defaults.memory'), command: 'free -h' },
      { id: 'd3', title: t('defaults.top'), command: 'ps aux --sort=-%cpu | head -15' },
      { id: 'd4', title: t('defaults.ports'), command: 'ss -tulpn' },
      { id: 'd5', title: t('defaults.failed'), command: 'systemctl --failed' },
      { id: 'd6', title: t('defaults.journal'), command: 'journalctl -f' },
    ],
    [t],
  );
  const customSnippets = Array.isArray(prefs['terminal.snippets']) ? (prefs['terminal.snippets'] as Snippet[]) : null;
  const snippets = customSnippets ?? defaultSnippets;
  const save = useCallback((key: string, value: unknown) => set(key, value).catch((e) => toast.err(t('saveFailed', { error: errMsg(e) }))), [set, t]);

  // ----- sessions from the user bridge (and the root bridge while unlocked) -----
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState('');
  const isRootUser = !!session?.isRoot;
  const adminOk = isUnlocked && unlockLeft > 2 && !isRootUser;
  const lastInfo = useRef(new Map<string, SessionInfo>());

  const refresh = useCallback(async () => {
    try {
      const mine = await call<{ sessions: Omit<SessionInfo, 'admin'>[] }>('terminal.list');
      let all: SessionInfo[] = (mine.sessions ?? []).map((s) => ({ ...s, admin: false }));
      if (adminOk) {
        try {
          const root = await call<{ sessions: Omit<SessionInfo, 'admin'>[] }>('terminal.list', {}, { admin: true });
          all = all.concat((root.sessions ?? []).map((s) => ({ ...s, kind: 'root' as const, admin: true })));
        } catch {
          /* the root bridge may have just locked */
        }
      }
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
    }, 4000);
    return () => window.clearInterval(id);
  }, [refresh]);

  // ----- layout: which sessions are open here, what the two panes show -----
  const [layout, setLayout] = useState<Layout>(loadLayout);
  const { open, panes, split } = layout;
  const [active, setActive] = useState<0 | 1>(0);
  const [focus, setFocus] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [sheet, setSheet] = useState(false);
  const [ctrl, setCtrl] = useState(false);
  const handles = useRef<[TermHandle | null, TermHandle | null]>([null, null]);

  const patch = useCallback((fn: (l: Layout) => Layout) => setLayout((l) => fn(l)), []);
  useEffect(() => {
    try {
      localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout));
    } catch {
      /* ignore */
    }
  }, [layout]);

  const byId = useMemo(() => new Map(sessions.map((s) => [s.id, s])), [sessions]);
  const infoOf = (id: string | null) => (id ? byId.get(id) ?? lastInfo.current.get(id) : undefined);
  const openSessions = open.map((id) => byId.get(id)).filter((s): s is SessionInfo => !!s);
  const detached = sessions.filter((s) => !open.includes(s.id));
  const visible: Panes = split && !mobile ? panes : [panes[0], null];
  const activeId = visible[active] ?? visible[0];

  const showSession = useCallback(
    (id: string) =>
      patch((l) => {
        const open = l.open.includes(id) ? l.open : [...l.open, id];
        const shown = l.split && !mobile ? l.panes : [l.panes[0], null];
        const at = shown.indexOf(id);
        if (at >= 0) {
          setActive(at as 0 | 1);
          return { ...l, open };
        }
        const idx = l.split && !mobile ? active : 0;
        const panes: Panes = [...l.panes];
        panes[idx] = id;
        return { ...l, open, panes };
      }),
    [patch, active, mobile],
  );

  // First load: drop panes whose session is gone, then show an open session, the newest one, or start one.
  const didBoot = useRef(false);
  useEffect(() => {
    if (!loaded || loadError || didBoot.current) return;
    didBoot.current = true;
    const keep = (id: string | null) => (id && byId.has(id) ? id : null);
    let p0 = keep(panes[0]);
    const p1 = keep(panes[1]);
    if (!p0 && p1) p0 = p1;
    if (!p0) p0 = open.find((id) => byId.has(id)) ?? (sessions.length ? sessions[sessions.length - 1].id : null);
    if (p0) {
      const id = p0;
      patch((l) => ({ ...l, panes: [id, p1 && p1 !== id ? p1 : null], open: l.open.includes(id) ? l.open : [...l.open, id] }));
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

  const closePane = useCallback(
    (idx: 0 | 1) =>
      patch((l) => {
        const id = l.panes[idx];
        const panes: Panes = [...l.panes];
        panes[idx] = null;
        if (!panes[0] && panes[1]) {
          panes[0] = panes[1];
          panes[1] = null;
        }
        return { ...l, panes, open: id && !panes.includes(id) ? l.open.filter((x) => x !== id) : l.open };
      }),
    [patch],
  );

  const detach = useCallback(
    (id: string) =>
      patch((l) => {
        let panes: Panes = [l.panes[0] === id ? null : l.panes[0], l.panes[1] === id ? null : l.panes[1]];
        if (!panes[0] && panes[1]) panes = [panes[1], null];
        return { ...l, panes, open: l.open.filter((x) => x !== id) };
      }),
    [patch],
  );

  // ----- creating, renaming, killing -----
  const create = useCallback(
    async (p: Record<string, unknown>, admin = false) => {
      try {
        const r = await call<{ id: string }>('terminal.create', { cols: 100, rows: 30, ...p }, { admin });
        await refresh();
        showSession(r.id);
        return r.id;
      } catch (e) {
        if (!(e instanceof ApiError && e.code === 'forbidden')) toast.err(t('createFailed', { error: errMsg(e) }));
        else toast.err(errMsg(e));
        return null;
      }
    },
    [refresh, showSession, t],
  );

  const [renaming, setRenaming] = useState<SessionInfo | null>(null);
  const [killing, setKilling] = useState<SessionInfo | null>(null);
  const [hostDlg, setHostDlg] = useState<{ mode: 'connect' | 'edit'; host: Host | null } | null>(null);
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
      if (!(e instanceof ApiError && e.code === 'not_found')) toast.err(t('killFailed', { error: errMsg(e) }));
    }
    void refresh();
  };

  const connectHost = (h: Host) => {
    const existing = sessions.find((s) => s.kind === 'ssh' && s.name === h.name);
    if (existing) return showSession(existing.id);
    void create({ kind: 'ssh', host: h.host, user: h.user, port: h.port, name: h.name });
  };
  const submitHost = (h: Omit<Host, 'id'> & { id?: string }, saveIt: boolean) => {
    const mode = hostDlg?.mode;
    setHostDlg(null);
    if (saveIt || mode === 'edit') {
      const entry: Host = { id: h.id ?? newId(), name: h.name, host: h.host, user: h.user, port: h.port };
      const next = hosts.some((x) => x.id === entry.id) ? hosts.map((x) => (x.id === entry.id ? entry : x)) : [...hosts, entry];
      void save('terminal.hosts', next);
      if (mode === 'edit') return toast.ok(t('hostSaved', { name: entry.name }));
    }
    void create({ kind: 'ssh', host: h.host, user: h.user, port: h.port, name: h.name });
  };

  // ----- ?cmd=… (links from other sections, e.g. Software's AUR "Open in terminal"):
  // a new session with the command typed at the prompt, not run, so the user reviews it first.
  const [params, setParams] = useSearchParams();
  const linkCmd = params.get('cmd');
  const [pendingInput, setPendingInput] = useState<{ id: string; text: string } | null>(null);
  useEffect(() => {
    if (!loaded || loadError || !linkCmd) return;
    const cmd = linkCmd.replace(/[\r\n]+/g, ' ').slice(0, 1000);
    setParams((p) => {
      const n = new URLSearchParams(p);
      n.delete('cmd');
      return n;
    }, { replace: true });
    void create({}).then((id) => id && setPendingInput({ id, text: cmd }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, loadError, linkCmd]);

  // ----- running things in the active pane -----
  const target = (): TermHandle | null => handles.current[(split && !mobile ? active : 0) as 0 | 1] ?? handles.current[0];
  const remember = (cmd: string) => {
    const c = cmd.trim();
    if (!c) return;
    void save('terminal.history', [{ cmd: c, at: Date.now() }, ...history.filter((h) => h.cmd !== c)].slice(0, 50));
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
    const base = customSnippets ?? defaultSnippets;
    const next = edit && base.some((s) => s.id === edit.id) ? base.map((s) => (s.id === edit.id ? { ...s, title, command } : s)) : [...base, { id: newId(), title, command }];
    void save('terminal.snippets', next);
    toast.ok(t('snippet.saved', { name: title }));
  };
  const deleteSnippet = (s: Snippet) => {
    const base = customSnippets ?? defaultSnippets;
    const before = base;
    void save('terminal.snippets', base.filter((x) => x.id !== s.id));
    toast.undo(t('snippet.deleted', { name: s.title }), t('undo'), () => void save('terminal.snippets', before));
  };
  const setShowSnippets = (v: boolean) => {
    void save('terminal', { ...opts, snippets: v });
    void save('terminal.showSnippets', v);
  };

  // ----- split / palette / focus -----
  const toggleSplit = () =>
    patch((l) => {
      if (l.split) return { ...l, split: false };
      let second = l.panes[1];
      if (!second || second === l.panes[0]) second = l.open.find((id) => id !== l.panes[0] && byId.has(id)) ?? null;
      return { ...l, split: true, panes: [l.panes[0], second] };
    });

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
  }, []);
  useFocusMode('terminal', focus);
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
  }, [focus]);

  // Keyboard on phones: keep the page above the on-screen keyboard.
  const rootRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv || !mobile) return;
    const on = () => rootRef.current?.style.setProperty('--term-vh', `${vv.height}px`);
    on();
    vv.addEventListener('resize', on);
    return () => vv.removeEventListener('resize', on);
  }, [mobile]);

  const newLocal = () => void create({});
  const newRoot = () => void create({}, true);
  const newSsh = () => setHostDlg({ mode: 'connect', host: null });

  usePaletteActions('terminal', [
    { id: 'terminal.new', title: t('newSession'), hint: t('title'), icon: 'plus', hue: 'term', keywords: ['shell', 'terminal'], run: newLocal },
    { id: 'terminal.root', title: t('newRoot'), hint: t('title'), icon: 'lock', hue: 'term', keywords: ['sudo', 'admin'], run: newRoot },
    { id: 'terminal.focus', title: t('focusMode'), hint: t('title'), icon: 'overview', hue: 'term', run: () => setFocus((f) => !f) },
  ]);

  const paletteItems: PaletteItem[] = [
    ...snippets.map((s) => {
      const look = snippetLook(s);
      return { id: 'sn' + s.id, group: 'snippets' as const, title: s.title, sub: s.command, icon: look.icon, hue: look.hue, run: () => runCmd(s.command), paste: () => pasteCmd(s.command) };
    }),
    ...sessions.map((s) => ({
      id: 'se' + s.id,
      group: 'sessions' as const,
      title: t('palette.openName', { name: s.name }),
      sub: t('palette.switch'),
      icon: (s.kind === 'root' ? 'lock' : s.kind === 'ssh' ? 'server' : 'terminal') as 'lock',
      hue: s.kind === 'root' ? 'svc' : s.kind === 'ssh' ? 'ov' : 'term',
      keywords: s.kind,
      run: () => showSession(s.id),
    })),
    ...history.map((h, i) => ({ id: 'hi' + i, group: 'history' as const, title: h.cmd, sub: relativeTime(h.at, lang), icon: 'clock' as const, hue: 'sw', run: () => runCmd(h.cmd), paste: () => pasteCmd(h.cmd) })),
    { id: 'a-new', group: 'actions' as const, title: t('newSession'), icon: 'plus' as const, hue: 'term', run: newLocal },
    { id: 'a-root', group: 'actions' as const, title: t('newRoot'), icon: 'lock' as const, hue: 'svc', keywords: 'sudo admin', run: newRoot },
    { id: 'a-ssh', group: 'actions' as const, title: t('newSsh'), icon: 'server' as const, hue: 'ov', run: newSsh },
    ...(mobile ? [] : [{ id: 'a-split', group: 'actions' as const, title: split ? t('splitOff') : t('splitOn'), icon: 'columns' as const, hue: 'term', run: toggleSplit }]),
    { id: 'a-focus', group: 'actions' as const, title: focus ? t('focusOff') : t('focusMode'), icon: 'overview' as const, hue: 'term', run: () => setFocus((f) => !f) },
    { id: 'a-save', group: 'actions' as const, title: t('snippet.add'), icon: 'command' as const, hue: 'term', run: () => setSnipDlg({}) },
    { id: 'a-chips', group: 'actions' as const, title: showSnippets ? t('hideSnippets') : t('showSnippets'), icon: 'eye' as const, hue: 'term', run: () => setShowSnippets(!showSnippets) },
  ];

  // ----- render -----
  const paneOf: Record<string, number> = {};
  if (split && !mobile) visible.forEach((id, i) => id && (paneOf[id] = i + 1));
  const connectedHosts = new Set(sessions.filter((s) => s.kind === 'ssh').map((s) => s.name));
  const activeInfo = infoOf(activeId ?? null);
  const home = session?.home;
  const prettyTitle = (s?: SessionInfo) => {
    const raw = s?.title || s?.name || '';
    return home && home !== '/' ? raw.split(home).join('~') : raw;
  };
  const sudoLeft = unlockForever ? t('sudoForever') : unlockLeft >= 60 ? t('sudoMin', { count: Math.ceil(unlockLeft / 60) }) : t('sudoSec', { count: unlockLeft });
  const sidebar = (
    <Sidebar
      open={openSessions}
      detached={detached}
      hosts={hosts}
      paneOf={paneOf}
      activeId={activeId ?? null}
      connectedHosts={connectedHosts}
      onNew={() => {
        setSheet(false);
        newLocal();
      }}
      onNewRoot={() => {
        setSheet(false);
        newRoot();
      }}
      onNewSsh={() => {
        setSheet(false);
        newSsh();
      }}
      onOpen={(s) => {
        setSheet(false);
        showSession(s.id);
      }}
      onDetach={(s) => detach(s.id)}
      onRename={setRenaming}
      onKill={setKilling}
      onConnect={(h) => {
        setSheet(false);
        connectHost(h);
      }}
      onEditHost={(h) => setHostDlg({ mode: 'edit', host: h })}
      onDeleteHost={(h) => {
        const before = hosts;
        void save('terminal.hosts', hosts.filter((x) => x.id !== h.id));
        toast.undo(t('hostDeleted', { name: h.name }), t('undo'), () => void save('terminal.hosts', before));
      }}
    />
  );

  const paneView = (idx: 0 | 1) => {
    const id = visible[idx];
    const info = infoOf(id);
    const isActive = (split && !mobile ? active : 0) === idx;
    if (!id) {
      return (
        <div className="terminal-pane" key={`empty${idx}`} onMouseDown={() => setActive(idx)}>
          <div className={`terminal-pane-h${isActive ? ' act' : ''}`}>{t('emptyPane')}</div>
          <div className="terminal-pane-empty">
            <Button icon="plus" onClick={() => (setActive(idx), newLocal())}>
              {t('newSession')}
            </Button>
          </div>
        </div>
      );
    }
    return (
      <div className="terminal-pane" key={id}>
        <div className={`terminal-pane-h${isActive ? ' act' : ''}`}>
          <span className="terminal-pane-t">{prettyTitle(info)}</span>
          <span className="sp" />
          <button type="button" aria-label={t('closePane')} title={t('closePane')} onClick={() => closePane(idx)}>
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

  const kindOfActive = activeInfo?.kind ?? 'local';
  const stripKinds = 'terminal-dot terminal-k-';

  return (
    <Page flush hue="term">
      <div ref={rootRef} className={`terminal-root${focus ? ' terminal-focus' : ''}${split && !mobile ? ' terminal-split' : ''}${mobile ? ' terminal-mobile' : ''}`}>
        {!compact && !focus && sidebar}
        <div className="terminal-body">
          {mobile && (
            <div className="terminal-strip" role="tablist" aria-label={t('sessions')}>
              {openSessions.map((s) => (
                <button key={s.id} type="button" role="tab" aria-selected={s.id === activeId} className={`terminal-tab${s.id === activeId ? ' on' : ''}`} onClick={() => showSession(s.id)}>
                  <span className={`${stripKinds}${s.kind}`} />
                  {s.name}
                </button>
              ))}
              <button type="button" className="terminal-tab terminal-tab-add" aria-label={t('newSession')} onClick={newLocal}>
                <Icon name="plus" />
              </button>
              <button type="button" className="terminal-tab terminal-tab-add" aria-label={t('sessions')} onClick={() => setSheet(true)}>
                <Icon name="menu" />
              </button>
            </div>
          )}
          <div className="terminal-tb">
            <span className="terminal-ttl">
              <span className={`${stripKinds}${kindOfActive}`} />
              {activeInfo?.name ?? t('title')}
            </span>
            {isUnlocked && (
              <Tooltip label={t('lockNow')}>
                <button type="button" className="terminal-sudo" onClick={() => void lock()} aria-label={`${t('sudoChip', { left: sudoLeft })}. ${t('lockNow')}`}>
                  <Icon name="lock" />
                  {t('sudoChip', { left: sudoLeft })}
                </button>
              </Tooltip>
            )}
            <span className="sp" />
            {compact && !mobile && (
              <button type="button" onClick={() => setSheet(true)} title={t('sessions')}>
                <Icon name="menu" />
                <span>{t('sessions')}</span>
              </button>
            )}
            {!mobile && (
              <button type="button" className={split ? 'on' : ''} aria-pressed={split} title={t('splitTip')} onClick={toggleSplit}>
                <Icon name="columns" />
                <span>{t('split')}</span>
              </button>
            )}
            <button type="button" className={paletteOpen ? 'on' : ''} aria-pressed={paletteOpen} title={t('paletteTip')} onClick={() => setPaletteOpen((o) => !o)}>
              <Icon name="search" />
              <span>{t('palette.title')}</span>
            </button>
            <button type="button" className={focus ? 'on' : ''} aria-pressed={focus} title={t('focusTip')} onClick={() => setFocus((f) => !f)}>
              <Icon name="overview" />
              <span>{t('focus')}</span>
            </button>
          </div>

          {loadError && !sessions.length ? (
            <div className="terminal-center">
              <EmptyState icon="alert" hue="term" title={t('loadFailed')} text={loadError} action={<Button onClick={() => void refresh()}>{t('retry')}</Button>} />
            </div>
          ) : !visible[0] && !visible[1] ? (
            <div className="terminal-center">
              {loaded && ready ? (
                <EmptyState icon="terminal" hue="term" title={t('noneTitle')} text={t('noneText')} action={<Button variant="primary" icon="plus" onClick={newLocal}>{t('newSession')}</Button>} />
              ) : null}
            </div>
          ) : (
            <div className="terminal-panes">
              {paneView(0)}
              {split && !mobile && paneView(1)}
            </div>
          )}

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

      <Sheet open={sheet && (compact || mobile)} onClose={() => setSheet(false)} title={t('sessions')}>
        <div className="terminal-sheet">{sidebar}</div>
      </Sheet>
      <RenameDialog open={!!renaming} name={renaming?.name ?? ''} onClose={() => setRenaming(null)} onSave={doRename} />
      <ConfirmDialog
        open={!!killing}
        onClose={() => setKilling(null)}
        onConfirm={doKill}
        title={t('killTitle', { name: killing?.name ?? '' })}
        description={t('killText')}
        confirmLabel={t('kill')}
      />
      <HostDialog open={!!hostDlg} mode={hostDlg?.mode ?? 'connect'} initial={hostDlg?.host} onClose={() => setHostDlg(null)} onSubmit={submitHost} />
      <SnippetDialog open={!!snipDlg} initial={snipDlg?.snippet ?? (snipDlg?.command ? { command: snipDlg.command } : undefined)} onClose={() => setSnipDlg(null)} onSave={saveSnippet} />
    </Page>
  );
}
