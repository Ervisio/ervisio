import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react';
import { useSearchParams } from 'react-router-dom';
import { call, usePrefs, useSession } from '../../api';
import { useI18n, useT, type TFn } from '../../i18n';
import { useWindowActivity } from '../../lib/windowActivity';
import {
  Icon,
  Page,
  Sheet,
  Tooltip,
  toast,
  useIsMobile,
  useMediaQuery,
  type IconName,
} from '../../ui';
import { useFocusMode } from '../../sections';
import ExtraKeys from './ExtraKeys';
import Palette from './Palette';
import Sidebar from './Sidebar';
import Snippets from './Snippets';
import { terminalPaletteItems, useTerminalPaletteActions } from './terminalPalette';
import { errMsg } from './terminalErrors';
import { TerminalPageDialogs } from './TerminalPageDialogs';
import { TerminalPaneArea } from './TerminalPanes';
import type { Host, HistoryItem, SessionInfo, Snippet } from './types';
import {
  useTerminalCommands,
  useTerminalHosts,
  useTerminalSessionDialogs,
  useTerminalSnippets,
} from './useTerminalActions';
import { useTerminalPanes } from './useTerminalPanes';
import { NativeTerminalChrome } from './NativeTerminalChrome';
import { NativeTerminalAppearance } from './NativeTerminalAppearance';
import { nativeAppearance, terminalFont, type NativeAppearance } from './appearance';
import { terminalLink } from './terminalLink';
import './terminal.css';
import './terminal-native.css';

const REFRESH_MS = 4000;
const DEFAULT_FONT_SIZE = 14;
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
  const active = useWindowActivity();
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
    if (!active) return;
    void refresh();
    const id = window.setInterval(() => {
      if (!document.hidden) void refresh();
    }, REFRESH_MS);
    return () => window.clearInterval(id);
  }, [refresh, active]);

  return { sessions, loaded, loadError, lastInfo, refresh };
}

// Links open a new session in cwd. Optional cmd is typed at the prompt, never run.
function useCommandLink(
  loaded: boolean,
  loadError: string,
  create: (p: Record<string, unknown>, admin?: boolean) => Promise<string | null>,
) {
  const [params, setParams] = useSearchParams();
  const linkKey = params.toString();
  const handled = useRef<string | null>(null);
  const [pendingInput, setPendingInput] = useState<{ id: string; text: string } | null>(null);
  useEffect(() => {
    const link = terminalLink(new URLSearchParams(linkKey));
    if (!link) {
      handled.current = null;
      return;
    }
    if (!loaded || loadError || handled.current === linkKey) return;
    handled.current = linkKey;
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        n.delete('cmd');
        n.delete('cwd');
        n.delete('admin');
        n.delete('launch');
        return n;
      },
      { replace: true },
    );
    void create({ ...(link.cwd ? { cwd: link.cwd } : {}) }, link.admin).then((id) => {
      if (id && link.command) setPendingInput({ id, text: link.command });
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, loadError, linkKey]);
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

function sudoLeftLabel(t: TFn, forever: boolean, left: number) {
  if (forever) return t('sudoForever');
  if (left >= 60) return t('sudoMin', { count: Math.ceil(left / 60) });
  return t('sudoSec', { count: left });
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
        icon="code"
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

function terminalRootClass(focus: boolean, splitShown: boolean, mobile: boolean) {
  return [
    'terminal-root',
    focus && 'terminal-focus',
    splitShown && 'terminal-split',
    mobile && 'terminal-mobile',
  ]
    .filter(Boolean)
    .join(' ');
}

export default function TerminalPage() {
  const t = useT('terminal');
  const { lang } = useI18n();
  const { session, host, isUnlocked, unlockLeft, unlockForever, lock } = useSession();
  const { prefs: accountPrefs } = usePrefs();
  const native = (accountPrefs['shell.mode'] ?? host?.defaultShell) === 'ervisios';
  const mobile = useIsMobile();
  const compact = useMediaQuery('(max-width: 1100px)');
  const prefs = useTerminalPrefs(t, session?.os === 'windows');
  const { hosts, history, save, snippets, showSnippets } = prefs;
  const [launchParams] = useSearchParams();

  const adminOk = isUnlocked && unlockLeft > 2 && !session?.isRoot;
  const { sessions, loaded, loadError, lastInfo, refresh } = useTerminalSessions(adminOk);
  const panesCtl = useTerminalPanes({
    t,
    mobile,
    sessions,
    loaded,
    loadError,
    lastInfo,
    refresh,
    launchRequested: !!terminalLink(launchParams),
  });
  const { splitShown, activePane, setActive, visible, activeId, showSession, create } = panesCtl;
  const [focus, setFocus] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [sheet, setSheet] = useState(false);
  const [ctrl, setCtrl] = useState(false);
  const [appearanceOpen, setAppearanceOpen] = useState(false);
  const [appearancePreview, setAppearancePreview] = useState<NativeAppearance | null>(null);
  const appearance = appearancePreview ?? nativeAppearance({ ...prefs.opts });

  const sessionDialogs = useTerminalSessionDialogs(t, refresh);
  const hostsCtl = useTerminalHosts({ t, hosts, save, sessions, create, showSession });
  const snippetsCtl = useTerminalSnippets({ t, snippets, opts: prefs.opts, save });
  const { setSnipDlg, setShowSnippets } = snippetsCtl;
  const { pendingInput, setPendingInput } = useCommandLink(loaded, loadError, create);
  const { handles, target, runCmd, pasteCmd } = useTerminalCommands(t, activePane, history, save);

  usePaletteShortcut(setPaletteOpen);
  useFocusMode('terminal', focus);
  useFocusExitKeys(focus, setFocus);
  const rootRef = useKeyboardSafeHeight(mobile);

  const newLocal = () => void create({});
  const newRoot = () => void create({}, true);
  const newSsh = () => hostsCtl.setHostDlg({ mode: 'connect', host: null });
  const toggleFocus = () => setFocus((f) => !f);
  const duplicate = (item: SessionInfo) => {
    const saved = item.kind === 'ssh' ? hosts.find((value) => value.name === item.name) : undefined;
    if (item.kind === 'ssh' && !saved) return;
    void create(
      saved
        ? { kind: 'ssh', host: saved.host, user: saved.user, port: saved.port, name: saved.name }
        : { cwd: item.cwd || undefined },
      item.admin,
    );
  };
  const pasteClipboard = async () => {
    try {
      pasteCmd(await navigator.clipboard.readText());
    } catch {
      toast.err(t('native.clipboardFailed'));
    }
  };
  const nativeKeys = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (
      !native ||
      !(event.target instanceof Node) ||
      !(
        rootRef.current?.contains(event.target) ||
        rootRef.current?.closest('.eos-window')?.contains(event.target)
      )
    )
      return;
    if (event.ctrlKey && event.shiftKey && event.code === 'KeyT') {
      event.preventDefault();
      event.stopPropagation();
      newLocal();
    } else if (event.ctrlKey && event.shiftKey && event.code === 'KeyW' && activeId) {
      const item = panesCtl.infoOf(activeId);
      if (item) {
        event.preventDefault();
        event.stopPropagation();
        sessionDialogs.setKilling(item);
      }
    } else if (event.ctrlKey && event.code === 'Tab' && panesCtl.openSessions.length) {
      event.preventDefault();
      event.stopPropagation();
      const open = panesCtl.openSessions;
      const index = open.findIndex((value) => value.id === activeId);
      showSession(open[(index + (event.shiftKey ? open.length - 1 : 1)) % open.length].id);
    }
  };
  useTerminalPaletteActions(t, { newLocal, newRoot, toggleFocus });

  const paletteItems = terminalPaletteItems({
    t,
    lang,
    snippets,
    sessions,
    history,
    mobile,
    split: panesCtl.split,
    focus,
    showSnippets,
    runCmd,
    pasteCmd,
    showSession,
    newLocal,
    newRoot,
    newSsh,
    toggleSplit: panesCtl.toggleSplit,
    toggleFocus,
    addSnippet: () => setSnipDlg({}),
    setShowSnippets,
  });

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
      open={panesCtl.openSessions}
      detached={panesCtl.detached}
      hosts={hosts}
      paneOf={paneOf}
      activeId={activeId ?? null}
      connectedHosts={connectedHosts}
      onNew={closeSheetThen(newLocal)}
      onNewRoot={closeSheetThen(newRoot)}
      onNewSsh={closeSheetThen(newSsh)}
      onOpen={closeSheetThen((s: SessionInfo) => showSession(s.id))}
      onDetach={(s) => panesCtl.detach(s.id)}
      onRename={sessionDialogs.setRenaming}
      onKill={sessionDialogs.setKilling}
      onConnect={closeSheetThen((h: Host) => hostsCtl.connectHost(h))}
      onEditHost={(h) => hostsCtl.setHostDlg({ mode: 'edit', host: h })}
      onDeleteHost={hostsCtl.deleteHost}
    />
  );

  return (
    <Page flush hue="term">
      <div
        ref={rootRef}
        className={`${terminalRootClass(focus, splitShown, mobile)}${native ? ' terminal-ervisios' : ''}`}
        onKeyDownCapture={nativeKeys}
        data-terminal-theme={native ? appearance.theme : undefined}
        style={
          native
            ? ({
                '--terminal-opacity': `${appearance.backgroundOpacity}%`,
                '--terminal-blur': `${appearance.backgroundBlur}px`,
              } as CSSProperties)
            : undefined
        }
      >
        {!native && !compact && !focus && sidebar}
        <div className="terminal-body">
          {native && (
            <NativeTerminalChrome
              sessions={panesCtl.openSessions}
              detached={panesCtl.detached}
              hosts={hosts}
              activeId={activeId ?? null}
              split={panesCtl.split}
              canSplit={!mobile}
              snippets={showSnippets}
              fontSize={appearance.fontSize}
              onShow={showSession}
              onNew={newLocal}
              onRoot={newRoot}
              onSsh={newSsh}
              onHost={hostsCtl.connectHost}
              onDuplicate={duplicate}
              onDetach={panesCtl.detach}
              onRename={sessionDialogs.setRenaming}
              onEnd={sessionDialogs.setKilling}
              onSessions={() => setSheet(true)}
              onSplit={panesCtl.toggleSplit}
              onPalette={() => setPaletteOpen((value) => !value)}
              onSnippets={() => setShowSnippets(!showSnippets)}
              onFont={(delta) =>
                void save('terminal', {
                  ...prefs.opts,
                  fontSize: Math.max(9, Math.min(32, appearance.fontSize + delta)),
                })
              }
              onCopy={() => target()?.copySelection()}
              onPaste={() => void pasteClipboard()}
              onFocus={toggleFocus}
              onAppearance={() => setAppearanceOpen(true)}
            />
          )}
          {!native && mobile && (
            <MobileStrip
              sessions={panesCtl.openSessions}
              activeId={activeId}
              onShow={showSession}
              onNew={newLocal}
              onMenu={() => setSheet(true)}
            />
          )}
          {!native && (
            <Toolbar
              active={panesCtl.infoOf(activeId ?? null)}
              sudoLeft={isUnlocked ? sudoLeftLabel(t, unlockForever, unlockLeft) : null}
              onLock={() => void lock()}
              showMenu={compact && !mobile}
              onMenu={() => setSheet(true)}
              showSplit={!mobile}
              split={panesCtl.split}
              onSplit={panesCtl.toggleSplit}
              paletteOpen={paletteOpen}
              onPalette={() => setPaletteOpen((o) => !o)}
              focus={focus}
              onFocus={toggleFocus}
            />
          )}

          <TerminalPaneArea
            loadError={loadError}
            hasSessions={sessions.length > 0}
            showNone={loaded && prefs.ready}
            visible={visible}
            splitShown={splitShown}
            activePane={activePane}
            onRetry={() => void refresh()}
            pane={{
              home: session?.home,
              fontSize: native ? appearance.fontSize : prefs.fontSize,
              fontFamily: native ? terminalFont(appearance.fontFamily) : undefined,
              transparent: native,
              cursorStyle: native ? appearance.cursorStyle : undefined,
              cursorBlink: native ? appearance.cursorBlink : undefined,
              lineHeight: native ? appearance.lineHeight : undefined,
              scrollback: native ? appearance.scrollback : undefined,
              copyOnSelect: prefs.copyOnSelect,
              themeMode: native ? appearance.theme : prefs.themeMode,
              ctrl,
              pendingInput,
              handles,
              infoOf: panesCtl.infoOf,
              onCtrlUsed: () => setCtrl(false),
              onEnded: () => void refresh(),
              onInitialInputUsed: () => setPendingInput(null),
              onActivate: setActive,
              onClosePane: panesCtl.closePane,
              onNew: newLocal,
            }}
          />

          {showSnippets && (
            <Snippets
              snippets={snippets}
              onPaste={(s) => pasteCmd(s.command)}
              onRun={(s) => runCmd(s.command)}
              onEdit={(s) => setSnipDlg({ snippet: s })}
              onDelete={snippetsCtl.deleteSnippet}
              onAdd={() => setSnipDlg({})}
            />
          )}
          {mobile && <ExtraKeys ctrl={ctrl} onCtrl={setCtrl} term={target} />}
          {native && (
            <div className="terminal-native-status">
              <span>
                <Icon name="server" size={12} />
                {host?.hostname}
              </span>
              <span>{session?.user}</span>
              <span className="terminal-native-status-path">
                {panesCtl.infoOf(activeId ?? null)?.title || panesCtl.infoOf(activeId ?? null)?.cwd}
              </span>
              {isUnlocked && (
                <button
                  onClick={() => void lock().catch((error) => toast.err(errMsg(error)))}
                  title={t('lockNow')}
                >
                  <Icon name="lock" size={12} />
                  {sudoLeftLabel(t, unlockForever, unlockLeft)}
                </button>
              )}
              <span>{t('native.terminal')}</span>
            </div>
          )}
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
        open={sheet && (native || compact || mobile)}
        onClose={() => setSheet(false)}
        title={t('sessions')}
      >
        <div className="terminal-sheet">{sidebar}</div>
      </Sheet>
      <TerminalPageDialogs
        sessionDialogs={sessionDialogs}
        hostsCtl={hostsCtl}
        snippetsCtl={snippetsCtl}
      />
      {native && (
        <NativeTerminalAppearance
          open={appearanceOpen}
          onClose={() => setAppearanceOpen(false)}
          onPreview={setAppearancePreview}
        />
      )}
    </Page>
  );
}
