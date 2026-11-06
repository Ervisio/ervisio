import {
  Component,
  Suspense,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
  type ErrorInfo,
  type ReactNode,
} from 'react';
import {
  MemoryRouter,
  Route,
  Routes,
  UNSAFE_LocationContext,
  UNSAFE_NavigationContext,
  UNSAFE_RouteContext,
  useLocation,
  useNavigate,
} from 'react-router-dom';
import { API_BASE, call, errorMessage, usePrefs, useSession } from '../api';
import { useI18n, useT } from '../i18n';
import { formatDate, formatTime, relativeTime } from '../lib/format';
import { PluginFrame } from '../plugins/PluginFrame';
import { usePlugins } from '../plugins';
import { SECTIONS } from '../sections';
import { useNav, type NavEntry } from '../shell/useNav';
import { clearNotices, markAllRead, useNotices } from '../shell/notifications';
import { useTheme } from '../theme';
import { BrandMark, Button, Dialog, Icon, Input, Skeleton, toast, useFocusTrap } from '../ui';
import { AppWindow, type DesktopWindow } from './AppWindow';
import {
  addShortcut,
  clampBounds,
  DESKTOP_CELL,
  layoutDesktopItems,
  moveItem,
  placeDesktopItems,
  readDesktop,
  removeFolder,
  type DesktopData,
  type WindowBounds,
} from './model';
import { Wallpaper } from './Wallpaper';
import { Connectivity } from './Connectivity';
import { WindowActivityProvider, useWindowActivity } from '../lib/windowActivity';
import { requestWindowClose } from '../lib/windowClose';
import {
  collectDropped,
  hasExternalFiles,
  DRAG_TYPE as FILE_DRAG_TYPE,
  type Dropped,
} from '../sections/files/dropfiles';
import { DirectoryShortcutDialog } from './DirectoryShortcutDialog';
import { menuPosition } from '../ui/menuPosition';
import { rememberDesktopRecovery, takeDesktopRecovery } from './recovery';
import './desktop.css';

type AppEntry = NavEntry & { native: boolean; plugin?: string; page?: string; nativeView?: string };
type Context = {
  x: number;
  y: number;
  item?: string;
  folder?: string | null;
  app?: string;
  anchor?: { left: number; top: number };
};
type Edit = { kind: 'create'; parent: string | null } | { kind: 'rename'; id: string };
const DRAG_TYPE = 'application/x-ervisio-desktop';
const importFailure = (error: Error) =>
  /dynamically imported module|loading chunk|module script/i.test(error.message);

class WindowBoundary extends Component<
  { children: ReactNode; fallback(error: Error, retry: () => void): ReactNode },
  { error: Error | null }
> {
  state: { error: Error | null } = { error: null };
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('ErvisiOS application:', error, info.componentStack);
  }
  render() {
    return this.state.error
      ? this.props.fallback(this.state.error, () => this.setState({ error: null }))
      : this.props.children;
  }
}

function AppIcon({ app, size = 24 }: { app: AppEntry; size?: number }) {
  return app.logo ? (
    <img src={app.logo} alt="" width={size} height={size} draggable={false} />
  ) : (
    <Icon name={app.icon} size={size} />
  );
}

function AppContent({ app, url }: { app: AppEntry; url: string }) {
  // Each desktop window owns navigation history; the browser router remains the desktop's '/'.
  return (
    <UNSAFE_LocationContext.Provider value={null!}>
      <UNSAFE_RouteContext.Provider value={{ outlet: null, matches: [], isDataRoute: false }}>
        <MemoryRouter
          initialEntries={[url]}
          future={{ v7_startTransition: true, v7_relativeSplatPath: true }}
        >
          <WindowNavigation app={app}>
            <WindowAppContent app={app} url={url} />
          </WindowNavigation>
        </MemoryRouter>
      </UNSAFE_RouteContext.Provider>
    </UNSAFE_LocationContext.Provider>
  );
}

function WindowNavigation({ app, children }: { app: AppEntry; children: ReactNode }) {
  const context = useContext(UNSAFE_NavigationContext);
  const navigation = useMemo(() => {
    const base = app.key === 'this-pc' ? '/files' : app.to.split(/[?#]/)[0];
    const intercept = (to: Parameters<typeof context.navigator.push>[0]) => {
      const target =
        typeof to === 'string' ? to : `${to.pathname ?? '/'}${to.search ?? ''}${to.hash ?? ''}`;
      const path = target.split(/[?#]/)[0];
      if (path === base || (base !== '/' && path.startsWith(`${base}/`))) return false;
      const event = new CustomEvent('ervisio:shell-action', {
        cancelable: true,
        detail: { action: 'open-app', to: target },
      });
      return !window.dispatchEvent(event);
    };
    return {
      ...context,
      navigator: {
        ...context.navigator,
        push: ((...args) => {
          if (!intercept(args[0])) context.navigator.push(...args);
        }) as typeof context.navigator.push,
        replace: ((...args) => {
          if (!intercept(args[0])) context.navigator.replace(...args);
        }) as typeof context.navigator.replace,
      },
    };
  }, [context, app]);
  return (
    <UNSAFE_NavigationContext.Provider value={navigation}>
      {children}
    </UNSAFE_NavigationContext.Provider>
  );
}

function WindowAppContent({ app, url }: { app: AppEntry; url: string }) {
  const { plugins } = usePlugins();
  const active = useWindowActivity();
  const t = useT('shell');
  const navigate = useNavigate();
  const previousURL = useRef(url);
  useEffect(() => {
    if (url !== previousURL.current) {
      previousURL.current = url;
      navigate(url);
    }
  }, [url, navigate]);
  if (app.plugin) {
    const plugin = plugins.find((p) => p.id === app.plugin);
    if (!plugin)
      return (
        <div className="eos-empty">
          <Icon name="plugins" />
          <p>{t('desktop.unavailable')}</p>
        </div>
      );
    return (
      <PluginFrame
        plugin={plugin}
        active={active}
        view={{ kind: 'page', id: app.nativeView ?? app.page!, shell: 'ervisios' }}
        title={app.label}
      />
    );
  }
  return (
    <Routes>
      {SECTIONS.map((s) => (
        <Route
          key={s.id}
          path={s.path === '/' ? '/' : `${s.path}/*`}
          element={s.desktopPage ? <s.desktopPage /> : <s.page />}
        />
      ))}
    </Routes>
  );
}

export function DesktopShell() {
  const t = useT('shell');
  const filesT = useT('files');
  const { lang } = useI18n();
  const { session, host, isUnlocked, signOut, lock } = useSession();
  const { prefs, set, ready } = usePrefs();
  const { isDark, toggleDark } = useTheme();
  const nav = useNav();
  const { pages, plugins, loading: pluginsLoading } = usePlugins();
  const location = useLocation();
  const navigate = useNavigate();
  const notices = useNotices();
  const [clock, setClock] = useState(() => new Date());
  const [panel, setPanel] = useState<'start' | 'quick' | 'notices' | null>(null);
  const [query, setQuery] = useState('');
  const [selectedResult, setSelectedResult] = useState(0);
  const [draggingApp, setDraggingApp] = useState(false);
  const [windows, setWindows] = useState<DesktopWindow[]>([]);
  const [context, setContext] = useState<Context | null>(null);
  const [editing, setEditing] = useState<Edit | null>(null);
  const [directoryEdit, setDirectoryEdit] = useState<{ folder: string | null; id?: string } | null>(
    null,
  );
  const [name, setName] = useState('');
  const [moveId, setMoveId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [upload, setUpload] = useState<Dropped[] | null>(null);
  const [uploadDest, setUploadDest] = useState('');
  const [uploadBusy, setUploadBusy] = useState(false);
  const [uploadError, setUploadError] = useState('');
  const [saveError, setSaveError] = useState('');
  const [selectedItems, setSelectedItems] = useState<string[]>([]);
  const recoveryChecked = useRef(false);
  const recoveryScope = session
    ? JSON.stringify([API_BASE || window.location.origin, session.uid, session.user])
    : '';
  const [gridSize, setGridSize] = useState({ columns: 1, rows: 1 });
  const [selectionRect, setSelectionRect] = useState<{
    x: number;
    y: number;
    width: number;
    height: number;
  } | null>(null);
  const selectionAnchor = useRef<string | null>(null);
  const selectionStart = useRef<{
    x: number;
    y: number;
    selected: string[];
    additive: boolean;
  } | null>(null);
  const desktopGrid = useRef<HTMLDivElement>(null);
  const surface = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const contextRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  useFocusTrap(menuRef, panel !== null && !context && !editing && !moveId && !upload, () =>
    setPanel(null),
  );
  useFocusTrap(contextRef, context !== null, () => setContext(null));
  useLayoutEffect(() => {
    if (!context || !contextRef.current) return;
    const box = contextRef.current.getBoundingClientRect();
    const { x, y } = menuPosition(
      { x: context.anchor?.left ?? context.x, y: context.anchor?.top ?? context.y },
      box,
      { width: window.innerWidth, height: window.innerHeight },
      !!context.anchor,
    );
    if (context.x !== x || context.y !== y) setContext({ ...context, x, y });
  }, [context]);

  const apps = useMemo<AppEntry[]>(
    () => [
      {
        key: 'this-pc',
        to: '/?desktop=pc',
        icon: 'server',
        label: t('desktop.thisPC'),
        hueClass: 'hue-file',
        kind: 'section',
        native: true,
      },
      ...SECTIONS.filter(
        (section) =>
          !('desktopHidden' in section && section.desktopHidden) &&
          (!section.platforms || section.platforms.includes(session?.os ?? 'linux')),
      ).map((section) => {
        const a = [...nav.main, nav.settings].find((entry) => entry.key === section.id) ?? {
          key: section.id,
          to: section.path,
          icon: section.icon,
          label: t(section.desktopTitleKey ?? section.titleKey),
          hueClass: `hue-${section.hue}`,
          kind: 'section' as const,
        };
        return {
          ...a,
          label:
            a.key === 'files'
              ? t('desktop.explorer')
              : a.key === 'overview'
                ? t('desktop.taskManager')
                : a.label,
          native: true,
        };
      }),
      ...pages.map((p) => {
        const m = plugins.find((x) => x.id === p.plugin);
        const entry = nav.plugins.find((a) => a.key === `p:${p.plugin}/${p.id}`);
        return {
          ...entry!,
          key: `p:${p.plugin}/${p.id}`,
          to: `/p/${p.plugin}/${p.id}`,
          label: p.title,
          icon: p.icon ?? m?.icon ?? 'plugins',
          hueClass: entry?.hueClass ?? 'hue-plg',
          kind: 'plugin' as const,
          native: !!p.ervisios,
          nativeView: p.ervisios?.view,
          plugin: p.plugin,
          page: p.id,
        };
      }),
    ],
    [nav.main, nav.settings, nav.plugins, pages, plugins, t, session?.os],
  );
  const appIds = useMemo(() => apps.map((a) => a.key), [apps]);
  const data = useMemo(() => readDesktop(prefs['shell.desktop'], appIds), [prefs, appIds]);
  const desktopIds = useMemo(
    () => [
      ...data.folders.filter((folder) => folder.parent === null).map((folder) => folder.id),
      ...data.shortcuts
        .filter((shortcut) => shortcut.folder === null && appIds.includes(shortcut.app))
        .map((shortcut) => shortcut.id),
      ...data.directoryShortcuts
        .filter((shortcut) => shortcut.folder === null)
        .map((shortcut) => shortcut.id),
    ],
    [data.folders, data.shortcuts, data.directoryShortcuts, appIds],
  );
  const gridRows = Math.max(gridSize.rows, Math.ceil(desktopIds.length / gridSize.columns));
  const gridPositions = useMemo(
    () => layoutDesktopItems(desktopIds, data.positions, gridSize.columns, gridRows),
    [desktopIds, data.positions, gridSize.columns, gridRows],
  );
  const dataRef = useRef(data);
  dataRef.current = data;
  const windowsRef = useRef(windows);
  windowsRef.current = windows;
  const appRef = useRef(apps);
  appRef.current = apps;
  const writes = useRef(0);
  const failedData = useRef<DesktopData | null>(null);
  const save = useCallback(
    (next: DesktopData) => {
      dataRef.current = next;
      setSaving(true);
      setSaveError('');
      writes.current += 1;
      // PrefsProvider updates optimistically and serializes per-user server writes.
      void set('shell.desktop', next)
        .then(
          () => {
            failedData.current = null;
          },
          (error: unknown) => {
            failedData.current = next;
            setSaveError(errorMessage(error));
          },
        )
        .finally(() => {
          writes.current -= 1;
          if (!writes.current) setSaving(false);
        });
    },
    [set],
  );
  const changeData = (fn: (d: DesktopData) => DesktopData) => save(fn(dataRef.current));
  const findApp = (id: string) => apps.find((a) => a.key === id);
  useEffect(() => {
    const area = surface.current;
    if (!area) return;
    const resize = () => {
      const columns = Math.max(1, Math.floor((area.clientWidth - 30) / DESKTOP_CELL.width));
      const rows = Math.max(1, Math.floor((area.clientHeight - 36) / DESKTOP_CELL.height));
      setGridSize((old) =>
        old.columns === columns && old.rows === rows ? old : { columns, rows },
      );
    };
    resize();
    const observer = new ResizeObserver(resize);
    observer.observe(area);
    return () => observer.disconnect();
  }, []);

  const selectItem = (event: React.MouseEvent, id: string, folder: string | null) => {
    if (folder !== null) return;
    const additive = event.ctrlKey || event.metaKey;
    if (event.shiftKey && selectionAnchor.current) {
      const ordered = [...desktopIds].sort(
        (a, b) =>
          gridPositions[a].column - gridPositions[b].column ||
          gridPositions[a].row - gridPositions[b].row,
      );
      const from = ordered.indexOf(selectionAnchor.current),
        to = ordered.indexOf(id);
      const range = ordered.slice(Math.min(from < 0 ? to : from, to), Math.max(from, to) + 1);
      setSelectedItems(additive ? [...new Set([...selectedItems, ...range])] : range);
    } else
      setSelectedItems(
        additive
          ? selectedItems.includes(id)
            ? selectedItems.filter((item) => item !== id)
            : [...selectedItems, id]
          : [id],
      );
    if (!event.shiftKey) selectionAnchor.current = id;
  };
  const beginSelection = (event: React.PointerEvent<HTMLDivElement>) => {
    if (
      event.button !== 0 ||
      event.pointerType === 'touch' ||
      (event.target as Element).closest('.eos-shortcut')
    )
      return;
    event.preventDefault();
    event.currentTarget.focus();
    event.currentTarget.setPointerCapture(event.pointerId);
    setPanel(null);
    setContext(null);
    const box = event.currentTarget.getBoundingClientRect();
    selectionStart.current = {
      x: event.clientX - box.left,
      y: event.clientY - box.top,
      selected: selectedItems,
      additive: event.ctrlKey || event.metaKey || event.shiftKey,
    };
    if (!selectionStart.current.additive) setSelectedItems([]);
  };
  const updateSelection = (event: React.PointerEvent<HTMLDivElement>) => {
    const start = selectionStart.current;
    if (!start) return;
    const box = event.currentTarget.getBoundingClientRect();
    const endX = Math.max(0, Math.min(box.width, event.clientX - box.left));
    const endY = Math.max(0, Math.min(box.height, event.clientY - box.top));
    const rect = {
      x: Math.min(start.x, endX),
      y: Math.min(start.y, endY),
      width: Math.abs(start.x - endX),
      height: Math.abs(start.y - endY),
    };
    if (rect.width < 3 && rect.height < 3) return;
    setSelectionRect(rect);
    const intersected = desktopIds.filter((id) => {
      const cell = gridPositions[id];
      const x = cell.column * DESKTOP_CELL.width,
        y = cell.row * DESKTOP_CELL.height;
      return (
        x < rect.x + rect.width && x + 94 > rect.x && y < rect.y + rect.height && y + 94 > rect.y
      );
    });
    setSelectedItems(
      start.additive ? [...new Set([...start.selected, ...intersected])] : intersected,
    );
  };
  const endSelection = (event: React.PointerEvent<HTMLDivElement>) => {
    selectionStart.current = null;
    setSelectionRect(null);
    if (event.currentTarget.hasPointerCapture(event.pointerId))
      event.currentTarget.releasePointerCapture(event.pointerId);
  };
  const desktopKey = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'a') {
      event.preventDefault();
      setSelectedItems(desktopIds);
      return;
    }
    if (event.key === 'Escape') {
      setSelectedItems([]);
      return;
    }
    const delta = (
      { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] } as Record<
        string,
        number[]
      >
    )[event.key];
    if (!delta) return;
    const focused = (event.target as HTMLElement).closest<HTMLElement>('[data-desktop-item]')
      ?.dataset.desktopItem;
    const current = gridPositions[focused ?? selectedItems.at(-1) ?? desktopIds[0]];
    if (!current) return;
    event.preventDefault();
    const candidates = desktopIds
      .filter((id) => {
        const cell = gridPositions[id];
        return delta[0]
          ? Math.sign(cell.column - current.column) === delta[0]
          : Math.sign(cell.row - current.row) === delta[1];
      })
      .sort((a, b) => {
        const score = (id: string) =>
          Math.abs(gridPositions[id].column - current.column) * (delta[0] ? 1 : 100) +
          Math.abs(gridPositions[id].row - current.row) * (delta[1] ? 1 : 100);
        return score(a) - score(b);
      });
    const next = candidates[0];
    if (!next) return;
    setSelectedItems(event.shiftKey ? [...new Set([...selectedItems, next])] : [next]);
    selectionAnchor.current = next;
    [...(desktopGrid.current?.querySelectorAll<HTMLElement>('[data-desktop-item]') ?? [])]
      .find((element) => element.dataset.desktopItem === next)
      ?.focus();
  };
  const focus = useCallback(
    (id: string) =>
      setWindows((list) => {
        const win = list.find((w) => w.id === id);
        return win ? [...list.filter((w) => w.id !== id), { ...win, minimized: false }] : list;
      }),
    [],
  );
  const patch = (id: string, delta: Partial<DesktopWindow>) =>
    setWindows((list) => list.map((w) => (w.id === id ? { ...w, ...delta } : w)));

  const open = useCallback((app: AppEntry, url = app.to) => {
    const area = surface.current;
    const width = area?.clientWidth ?? window.innerWidth,
      height = area?.clientHeight ?? window.innerHeight - 90;
    setWindows((list) => {
      const existing = list.find((w) => w.app === app.key);
      if (existing)
        return [
          ...list.filter((w) => w.id !== existing.id),
          { ...existing, url, minimized: false },
        ];
      const bounds = clampBounds(
        dataRef.current.bounds[app.key] ?? {
          x: 100 + (list.length % 5) * 32,
          y: 26 + (list.length % 5) * 30,
          width: app.plugin === 'ai' ? 850 : 1120,
          height: 710,
        },
        width,
        height,
      );
      return [
        ...list,
        {
          id: crypto.randomUUID(),
          app: app.key,
          url,
          bounds,
          minimized: false,
          maximized: width < 860,
        },
      ];
    });
    setPanel(null);
    setContext(null);
  }, []);
  const openFolder = (id: string) => {
    const folder = dataRef.current.folders.find((f) => f.id === id);
    if (!folder) return;
    const app: AppEntry = {
      key: `folder:${id}`,
      to: '/',
      icon: 'files',
      label: folder.name,
      hueClass: 'hue-file',
      kind: 'section',
      native: true,
    };
    open(app, '/');
  };
  const openDirectory = (path: string) => {
    const app = findApp('files');
    if (app) open(app, `/files?${new URLSearchParams({ path })}`);
  };
  const currentURL = `${location.pathname}${location.search}${location.hash}`;
  const lastURL = useRef('');
  useEffect(() => {
    if (currentURL === lastURL.current || pluginsLoading) return;
    const app =
      apps.find((a) => a.to === currentURL) ??
      apps.find(
        (a) =>
          a.key !== 'this-pc' &&
          (a.to === '/'
            ? location.pathname === '/'
            : location.pathname === a.to || location.pathname.startsWith(`${a.to}/`)),
      );
    if (app) {
      lastURL.current = currentURL;
      // A fresh sign-in lands on the desktop; direct links still open their real app.
      if (currentURL !== '/') open(app, currentURL);
    }
    if (currentURL !== '/') navigate('/', { replace: true });
  }, [currentURL, apps, location.pathname, open, pluginsLoading, navigate]);

  useEffect(() => {
    if (recoveryChecked.current || pluginsLoading || !recoveryScope) return;
    recoveryChecked.current = true;
    try {
      const intent = takeDesktopRecovery(sessionStorage, recoveryScope, apps);
      // A newly supplied deep link takes precedence over an old recovery intent.
      if (intent && currentURL === '/') {
        const app = apps.find((entry) => entry.key === intent.app);
        if (app) open(app, intent.url);
      }
    } catch {
      /* Browser policy may disable sessionStorage itself. */
    }
  }, [apps, pluginsLoading, recoveryScope, currentURL, open]);

  useEffect(() => {
    let timer: number;
    const tick = () => {
      setClock(new Date());
      timer = window.setTimeout(tick, 60_000 - (Date.now() % 60_000));
    };
    timer = window.setTimeout(tick, 60_000 - (Date.now() % 60_000));
    return () => window.clearTimeout(timer);
  }, []);
  useEffect(() => {
    if (panel === 'start') {
      setQuery('');
      setSelectedResult(0);
      searchRef.current?.focus();
    }
  }, [panel]);
  useEffect(() => {
    const action = (event: Event) => {
      const d = (event as CustomEvent<{ action: string; to?: string }>).detail;
      if (d.action === 'open-app' && d.to) {
        const path = d.to.split(/[?#]/)[0];
        const app =
          appRef.current.find((a) => a.to === d.to) ??
          appRef.current.find((a) => a.key !== 'this-pc' && a.to === path);
        if (app) {
          event.preventDefault();
          open(app, d.to);
        }
      } else if (d.action === 'show-desktop')
        setWindows((list) => list.map((w) => ({ ...w, minimized: true })));
      else if (d.action === 'close-window') {
        const activeWindow = [...windowsRef.current].reverse().find((win) => !win.minimized);
        if (activeWindow)
          requestWindowClose(activeWindow.id, () =>
            setWindows((list) => list.filter((win) => win.id !== activeWindow.id)),
          );
      } else if (d.action === 'minimize-window')
        setWindows((list) => {
          const active = [...list].reverse().find((w) => !w.minimized);
          return list.map((w) => (w.id === active?.id ? { ...w, minimized: true } : w));
        });
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setPanel(null);
        setContext(null);
      }
      if (e.altKey && e.key === 'F4') {
        e.preventDefault();
        action(new CustomEvent('ervisio:shell-action', { detail: { action: 'close-window' } }));
      }
      if ((e.ctrlKey || e.metaKey) && e.altKey && e.key.toLowerCase() === 'd') {
        e.preventDefault();
        action(new CustomEvent('ervisio:shell-action', { detail: { action: 'show-desktop' } }));
      }
      if (e.altKey && e.key === 'Tab') {
        e.preventDefault();
        const list = windowsRef.current;
        if (list.length > 1) focus(list[0].id);
      }
    };
    window.addEventListener('ervisio:shell-action', action);
    window.addEventListener('keydown', key);
    return () => {
      window.removeEventListener('ervisio:shell-action', action);
      window.removeEventListener('keydown', key);
    };
  }, [focus, open]);

  // Persist the initial desktop only after the complete installed-app catalog is available.
  useEffect(() => {
    if (ready && !pluginsLoading && prefs['shell.desktop'] === undefined) save(dataRef.current);
  }, [ready, pluginsLoading, prefs, save]);

  const togglePin = (app: string) =>
    changeData((d) => ({
      ...d,
      pinned: d.pinned.includes(app)
        ? d.pinned.filter((x) => x !== app)
        : [...d.pinned, app].slice(0, 32),
    }));
  const createFolder = (parent: string | null) => {
    if (dataRef.current.folders.length >= 256) {
      toast.err(t('desktop.tooManyFolders'));
      return;
    }
    setName(t('desktop.newFolder'));
    setEditing({ kind: 'create', parent });
    setContext(null);
    setPanel(null);
  };
  const renameFolder = (id: string) => {
    setName(data.folders.find((f) => f.id === id)?.name ?? '');
    setEditing({ kind: 'rename', id });
    setContext(null);
  };
  const completeEdit = () => {
    const label = name.trim();
    if (!editing || !label) return;
    const edit = editing;
    changeData((d) =>
      edit.kind === 'create'
        ? {
            ...d,
            folders: [...d.folders, { id: crypto.randomUUID(), name: label, parent: edit.parent }],
          }
        : { ...d, folders: d.folders.map((f) => (f.id === edit.id ? { ...f, name: label } : f)) },
    );
    setEditing(null);
  };
  const dragStart = (e: DragEvent, payload: { item?: string; app?: string }) => {
    const items =
      payload.item && desktopIds.includes(payload.item) && selectedItems.includes(payload.item)
        ? selectedItems.filter((id) => desktopIds.includes(id))
        : payload.item
          ? [payload.item]
          : undefined;
    if (items) setSelectedItems(items);
    e.dataTransfer.setData(DRAG_TYPE, JSON.stringify({ ...payload, items }));
    e.dataTransfer.effectAllowed = 'copyMove';
    setSelectionRect(null);
    selectionStart.current = null;
  };
  const drop = (e: DragEvent, folder: string | null) => {
    setDraggingApp(false);
    if (e.dataTransfer.types.includes(FILE_DRAG_TYPE)) {
      e.preventDefault();
      e.stopPropagation();
      return;
    }
    if (hasExternalFiles(e.dataTransfer)) {
      e.preventDefault();
      e.stopPropagation();
      setUploadError('');
      setUploadDest(session?.home ?? '/');
      // Read directory handles during the drop event while browser access is granted.
      void collectDropped(e.dataTransfer)
        .then((items) => {
          if (items.length) setUpload(items);
          else toast.info(t('desktop.emptyDrop'));
        })
        .catch((error) => toast.err(errorMessage(error)));
      return;
    }
    if (!e.dataTransfer.types.includes(DRAG_TYPE)) return;
    e.preventDefault();
    e.stopPropagation();
    try {
      const payload: { item?: string; app?: string; items?: string[] } = JSON.parse(
        e.dataTransfer.getData(DRAG_TYPE),
      );
      const grid = desktopGrid.current;
      const box = grid?.getBoundingClientRect();
      const target = box
        ? {
            column: Math.max(
              0,
              Math.min(
                gridSize.columns - 1,
                Math.round((e.clientX - box.left - 47) / DESKTOP_CELL.width),
              ),
            ),
            row: Math.max(
              0,
              Math.min(gridRows - 1, Math.round((e.clientY - box.top - 47) / DESKTOP_CELL.height)),
            ),
          }
        : { column: 0, row: 0 };
      if (payload.item) {
        const items = Array.isArray(payload.items)
          ? payload.items.filter((id): id is string => typeof id === 'string').slice(0, 1536)
          : [payload.item];
        if (!items.includes(payload.item)) items.unshift(payload.item);
        changeData((d) => {
          const next = items.reduce((state, id) => moveItem(state, id, folder), d);
          if (folder !== null) return next;
          const visible = [
            ...next.folders.filter((item) => item.parent === null).map((item) => item.id),
            ...next.shortcuts
              .filter((item) => item.folder === null && appIds.includes(item.app))
              .map((item) => item.id),
            ...next.directoryShortcuts
              .filter((item) => item.folder === null)
              .map((item) => item.id),
          ];
          return placeDesktopItems(
            next,
            visible,
            items,
            payload.item!,
            target,
            gridSize.columns,
            gridRows,
          );
        });
      } else if (payload.app && findApp(payload.app))
        changeData((d) => {
          const next = addShortcut(d, payload.app!, folder);
          if (folder !== null) return next;
          const shortcut = next.shortcuts.find(
            (item) => item.app === payload.app && item.folder === null,
          );
          if (!shortcut) return next;
          const visible = [...desktopIds.filter((id) => id !== shortcut.id), shortcut.id];
          return placeDesktopItems(
            next,
            visible,
            [shortcut.id],
            shortcut.id,
            target,
            gridSize.columns,
            gridRows,
          );
        });
      setPanel(null);
    } catch {
      /* Ignore other applications' drag payloads. */
    }
  };
  const allowDrop = (e: DragEvent) => {
    if (e.dataTransfer.types.includes(FILE_DRAG_TYPE)) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'none';
      return;
    }
    if (e.dataTransfer.types.includes(DRAG_TYPE) || hasExternalFiles(e.dataTransfer))
      e.preventDefault();
  };
  const menu = (e: React.MouseEvent, value: Omit<Context, 'x' | 'y'>) => {
    e.preventDefault();
    e.stopPropagation();
    setPanel(null);
    if (value.item && !selectedItems.includes(value.item)) setSelectedItems([value.item]);
    setContext({
      ...value,
      ...(e.currentTarget.closest('.eos-taskbar')
        ? { anchor: e.currentTarget.getBoundingClientRect() }
        : {}),
      x: e.clientX,
      y: e.clientY,
    });
  };

  const renderItems = (folder: string | null) => (
    <div
      className={`eos-icon-grid${folder === null ? ' eos-desktop-grid' : ''}`}
      ref={folder === null ? desktopGrid : undefined}
      role={folder === null ? 'listbox' : undefined}
      aria-label={folder === null ? t('desktop.icons') : undefined}
      aria-multiselectable={folder === null ? true : undefined}
      tabIndex={folder === null ? 0 : undefined}
      style={folder === null ? { minHeight: gridRows * DESKTOP_CELL.height } : undefined}
      onPointerDown={folder === null ? beginSelection : undefined}
      onPointerMove={folder === null ? updateSelection : undefined}
      onPointerUp={folder === null ? endSelection : undefined}
      onPointerCancel={folder === null ? endSelection : undefined}
      onKeyDown={folder === null ? desktopKey : undefined}
      onDragOver={allowDrop}
      onDrop={(e) => drop(e, folder)}
      onContextMenu={(e) => menu(e, { folder })}
    >
      {data.folders
        .filter((f) => f.parent === folder)
        .map((f) => (
          <button
            key={f.id}
            className={`eos-shortcut eos-folder${folder === null && selectedItems.includes(f.id) ? ' is-selected' : ''}`}
            data-desktop-item={folder === null ? f.id : undefined}
            role={folder === null ? 'option' : undefined}
            aria-selected={folder === null ? selectedItems.includes(f.id) : undefined}
            style={
              folder === null
                ? {
                    left: gridPositions[f.id]?.column * DESKTOP_CELL.width,
                    top: gridPositions[f.id]?.row * DESKTOP_CELL.height,
                  }
                : undefined
            }
            draggable
            onDragStart={(e) => dragStart(e, { item: f.id })}
            onDragOver={allowDrop}
            onDrop={(e) => drop(e, f.id)}
            onDoubleClick={() => openFolder(f.id)}
            onClick={(e) => {
              selectItem(e, f.id, folder);
              if (e.detail === 0 || window.matchMedia('(pointer: coarse)').matches)
                openFolder(f.id);
            }}
            onContextMenu={(e) => menu(e, { item: f.id, folder })}
          >
            <span className="eos-shortcut-icon">
              <Icon name="files" size={34} />
              <span className="eos-folder-mini">
                {data.shortcuts
                  .filter((s) => s.folder === f.id)
                  .slice(0, 4)
                  .map((s) => {
                    const app = findApp(s.app);
                    return app ? <AppIcon key={s.id} app={app} size={10} /> : null;
                  })}
              </span>
            </span>
            <span>{f.name}</span>
          </button>
        ))}
      {data.shortcuts
        .filter((s) => s.folder === folder)
        .map((s) => {
          const app = findApp(s.app);
          if (!app) return null;
          return (
            <button
              key={s.id}
              className={`eos-shortcut ${app.hueClass}${folder === null && selectedItems.includes(s.id) ? ' is-selected' : ''}`}
              data-desktop-item={folder === null ? s.id : undefined}
              role={folder === null ? 'option' : undefined}
              aria-selected={folder === null ? selectedItems.includes(s.id) : undefined}
              style={
                folder === null
                  ? {
                      left: gridPositions[s.id]?.column * DESKTOP_CELL.width,
                      top: gridPositions[s.id]?.row * DESKTOP_CELL.height,
                    }
                  : undefined
              }
              draggable
              onDragStart={(e) => dragStart(e, { item: s.id })}
              onDoubleClick={() => open(app)}
              onClick={(e) => {
                selectItem(e, s.id, folder);
                if (e.detail === 0 || window.matchMedia('(pointer: coarse)').matches) open(app);
              }}
              onContextMenu={(e) => menu(e, { item: s.id, app: app.key, folder })}
            >
              <span className="eos-shortcut-icon">
                <AppIcon app={app} size={32} />
              </span>
              <span>{app.label}</span>
            </button>
          );
        })}
      {data.directoryShortcuts
        .filter((item) => item.folder === folder)
        .map((item) => (
          <button
            key={item.id}
            className={`eos-shortcut eos-directory-shortcut hue-file${folder === null && selectedItems.includes(item.id) ? ' is-selected' : ''}`}
            data-desktop-item={folder === null ? item.id : undefined}
            role={folder === null ? 'option' : undefined}
            aria-selected={folder === null ? selectedItems.includes(item.id) : undefined}
            title={item.path}
            style={
              folder === null
                ? {
                    left: gridPositions[item.id]?.column * DESKTOP_CELL.width,
                    top: gridPositions[item.id]?.row * DESKTOP_CELL.height,
                  }
                : undefined
            }
            draggable
            onDragStart={(event) => dragStart(event, { item: item.id })}
            onDoubleClick={() => openDirectory(item.path)}
            onClick={(event) => {
              selectItem(event, item.id, folder);
              if (event.detail === 0 || window.matchMedia('(pointer: coarse)').matches)
                openDirectory(item.path);
            }}
            onContextMenu={(event) => menu(event, { item: item.id, folder })}
          >
            <span className="eos-shortcut-icon">
              <Icon name="files" size={34} />
              <span className="eos-shortcut-link" aria-hidden="true">
                ↗
              </span>
            </span>
            <span>{item.name}</span>
          </button>
        ))}
      {folder === null && selectionRect && (
        <div
          className="eos-selection-rectangle"
          aria-hidden="true"
          style={{
            left: selectionRect.x,
            top: selectionRect.y,
            width: selectionRect.width,
            height: selectionRect.height,
          }}
        />
      )}
      {folder === null && (
        <span className="eos-selection-status" role="status">
          {selectedItems.length ? t('desktop.selectedIcons', { count: selectedItems.length }) : ''}
        </span>
      )}
    </div>
  );
  const active = [...windows].reverse().find((w) => !w.minimized)?.id;
  const pinnedApps = data.pinned.map(findApp).filter((a): a is AppEntry => !!a);
  const taskApps = [
    ...pinnedApps,
    ...windows
      .filter((w) => !w.app.startsWith('folder:') && !data.pinned.includes(w.app))
      .map((w) => findApp(w.app))
      .filter((a): a is AppEntry => !!a),
  ];
  const filtered = apps.filter((a) =>
    `${a.label} ${a.plugin ?? ''}`.toLocaleLowerCase(lang).includes(query.toLocaleLowerCase(lang)),
  );
  const resultIndex = Math.min(selectedResult, Math.max(0, filtered.length - 1));
  const resultId = (index: number) => `eos-app-result-${index}`;
  const ai = apps.find((a) => a.plugin === 'ai');
  const folderItem = context?.item ? data.folders.find((f) => f.id === context.item) : undefined;
  const shortcutItem = context?.item
    ? data.shortcuts.find((s) => s.id === context.item)
    : undefined;
  const contextApp = context?.app ? findApp(context.app) : undefined;
  const directoryItem = data.directoryShortcuts.find((item) => item.id === context?.item);
  const unread = notices.filter((n) => !n.read).length;
  const togglePanel = (next: typeof panel) => {
    setContext(null);
    if (panel === 'notices') markAllRead();
    setPanel(panel === next ? null : next);
  };

  return (
    <div
      className={`eos${draggingApp ? ' eos-is-dragging-app' : ''}`}
      data-shell="ervisios"
      onDragOver={allowDrop}
      onDrop={(e) => drop(e, null)}
    >
      <Wallpaper kind={data.wallpaper} />
      <header className="eos-menubar">
        <span className="eos-wordmark">
          <BrandMark size={28} />
          <b>ErvisiOS</b>
        </span>
        <span className="eos-host">
          <Icon name="server" size={14} />
          {host?.hostname}
        </span>
        <span className="eos-save" role="status">
          {saving ? (
            t('desktop.saving')
          ) : saveError ? (
            <button
              onClick={() => failedData.current && save(failedData.current)}
              title={saveError}
            >
              <Icon name="alert" size={14} />
              {t('desktop.retrySave')}
            </button>
          ) : (
            t('desktop.saved')
          )}
        </span>
        <button
          className="eos-menubar-button"
          onClick={() => open(apps.find((a) => a.key === 'settings')!)}
          title={t('desktop.personalize')}
        >
          <Icon name="palette" size={16} />
        </button>
      </header>
      <main
        id="main"
        className="eos-workspace"
        ref={surface}
        onContextMenu={(e) => {
          if (e.target === e.currentTarget) menu(e, { folder: null });
        }}
        onDragOver={allowDrop}
        onDrop={(e) => drop(e, null)}
      >
        <div className="eos-desktop-items">{renderItems(null)}</div>
        <div className="eos-desktop-signature" aria-hidden="true">
          <BrandMark size={94} />
          <span>ErvisiOS</span>
          <small>{host?.hostname}</small>
        </div>
        {windows.map((win, index) => {
          const folder = win.app.startsWith('folder:')
            ? data.folders.find((f) => `folder:${f.id}` === win.app)
            : undefined;
          const app = apps.find((a) => a.key === win.app);
          if (!app && !folder) return null;
          const title = folder?.name ?? app!.label;
          return (
            <AppWindow
              key={win.id}
              win={win}
              title={title}
              icon={app ? <AppIcon app={app} size={18} /> : <Icon name="files" size={18} />}
              active={active === win.id}
              z={10 + index}
              onFocus={() => {
                if (active !== win.id) focus(win.id);
              }}
              onClose={() =>
                requestWindowClose(win.id, () =>
                  setWindows((list) => list.filter((w) => w.id !== win.id)),
                )
              }
              onChange={(delta) => patch(win.id, delta)}
              onSaveBounds={(bounds: WindowBounds) =>
                changeData((d) => ({ ...d, bounds: { ...d.bounds, [win.app]: bounds } }))
              }
            >
              <WindowActivityProvider active={!win.minimized}>
                <WindowBoundary
                  fallback={(error, retry) => (
                    <div className="eos-empty eos-app-error" role="alert">
                      <Icon name="alert" />
                      <p>{t('desktop.appFailed')}</p>
                      <details>
                        <summary>{t('desktop.errorDetails')}</summary>
                        <pre>{error.message}</pre>
                      </details>
                      <Button
                        onClick={
                          importFailure(error)
                            ? () => {
                                try {
                                  if (app)
                                    rememberDesktopRecovery(
                                      sessionStorage,
                                      recoveryScope,
                                      app,
                                      win.url,
                                    );
                                } catch {
                                  /* Reload remains available when browser storage is blocked. */
                                }
                                window.location.reload();
                              }
                            : retry
                        }
                      >
                        {t(importFailure(error) ? 'desktop.reload' : 'desktop.retry')}
                      </Button>
                      <Button
                        onClick={() => setWindows((list) => list.filter((w) => w.id !== win.id))}
                      >
                        {t('desktop.close')}
                      </Button>
                    </div>
                  )}
                >
                  <Suspense
                    fallback={
                      <div className="eos-loading">
                        <Skeleton lines={6} />
                      </div>
                    }
                  >
                    {folder ? (
                      <div className="eos-folder-content">
                        <div className="eos-folder-toolbar">
                          <Icon name="files" />
                          <span>{t('desktop.virtualFolder')}</span>
                          <Button icon="folderplus" onClick={() => createFolder(folder.id)}>
                            {t('desktop.newFolder')}
                          </Button>
                        </div>
                        <p>{t('desktop.virtualHint')}</p>
                        {renderItems(folder.id)}
                        {!data.folders.some((f) => f.parent === folder.id) &&
                          !data.shortcuts.some((s) => s.folder === folder.id) &&
                          !data.directoryShortcuts.some((s) => s.folder === folder.id) && (
                            <div className="eos-empty">
                              <Icon name="files" size={36} />
                              <p>{t('desktop.emptyFolder')}</p>
                            </div>
                          )}
                      </div>
                    ) : app!.key === 'this-pc' ? (
                      <AppContent app={app!} url="/files?view=computer" />
                    ) : (
                      <AppContent app={app!} url={win.url} />
                    )}
                  </Suspense>
                </WindowBoundary>
              </WindowActivityProvider>
            </AppWindow>
          );
        })}
      </main>
      {(panel || context) && (
        <div
          className="eos-dismiss"
          onClick={() => {
            if (panel === 'notices') markAllRead();
            setPanel(null);
            setContext(null);
          }}
        />
      )}
      {panel && (
        <div
          ref={menuRef}
          className={`eos-panel eos-panel--${panel}`}
          role="dialog"
          aria-label={t(`desktop.${panel}`)}
        >
          {panel === 'start' && (
            <>
              <div className="eos-start-search">
                <Icon name="search" />
                <input
                  ref={searchRef}
                  value={query}
                  onChange={(e) => {
                    setQuery(e.target.value);
                    setSelectedResult(0);
                  }}
                  placeholder={t('desktop.search')}
                  aria-label={t('desktop.search')}
                  role="combobox"
                  aria-autocomplete="list"
                  aria-expanded="true"
                  aria-controls="eos-app-results"
                  aria-activedescendant={filtered.length ? resultId(resultIndex) : undefined}
                  onKeyDown={(e) => {
                    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      if (filtered[resultIndex]) open(filtered[resultIndex]);
                    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                      e.preventDefault();
                      if (!filtered.length) return;
                      const next =
                        (resultIndex + (e.key === 'ArrowDown' ? 1 : -1) + filtered.length) %
                        filtered.length;
                      setSelectedResult(next);
                      document.getElementById(resultId(next))?.scrollIntoView({ block: 'nearest' });
                    }
                  }}
                />
                {query && (
                  <button
                    onClick={() => {
                      setQuery('');
                      setSelectedResult(0);
                    }}
                    aria-label={t('desktop.clearSearch')}
                  >
                    <Icon name="close" />
                  </button>
                )}
              </div>
              <div className="eos-panel-heading">
                <h2>{query ? t('desktop.searchResults') : t('desktop.allApps')}</h2>
                <span>{filtered.length}</span>
              </div>
              <div
                className="eos-apps"
                id="eos-app-results"
                role="listbox"
                aria-label={t('desktop.searchResults')}
              >
                {filtered.map((app, index) => (
                  <div
                    key={app.key}
                    id={resultId(index)}
                    role="option"
                    aria-label={app.label}
                    aria-selected={index === resultIndex}
                    className={`eos-start-app-row${index === resultIndex ? ' is-selected' : ''}`}
                    onMouseEnter={() => setSelectedResult(index)}
                  >
                    <button
                      className={`eos-start-app ${app.hueClass}`}
                      onClick={() => open(app)}
                      draggable
                      onDragStart={(e) => {
                        dragStart(e, { app: app.key });
                        setDraggingApp(true);
                      }}
                      onDragEnd={() => setDraggingApp(false)}
                    >
                      <span className="eos-app-icon">
                        <AppIcon app={app} size={26} />
                      </span>
                      <span>
                        <b>{app.label}</b>
                        <small>
                          {app.plugin
                            ? app.native
                              ? t('desktop.nativeApp')
                              : t('desktop.classicApp')
                            : 'Ervisio'}
                        </small>
                      </span>
                    </button>
                    <button
                      className="eos-app-more"
                      aria-label={t('desktop.appOptions', { name: app.label })}
                      onClick={(e) => {
                        const r = e.currentTarget.getBoundingClientRect();
                        setContext({
                          x: r.left,
                          y: r.top,
                          anchor: r,
                          app: app.key,
                          folder: null,
                        });
                      }}
                    >
                      <Icon name="more" />
                    </button>
                  </div>
                ))}
              </div>
              {!filtered.length && <p className="eos-empty">{t('desktop.noApps')}</p>}
              <footer className="eos-start-footer">
                <span className="eos-account">
                  <span>{(session?.name || session?.user || '?').slice(0, 1).toUpperCase()}</span>
                  <b>{session?.name || session?.user}</b>
                </span>
                <button
                  title={t('desktop.classic')}
                  onClick={() =>
                    void set('shell.mode', 'classic').catch((e) => toast.err(errorMessage(e)))
                  }
                >
                  <Icon name="columns" />
                </button>
                <button
                  title={t('desktop.signOut')}
                  onClick={() => void signOut().catch((e) => toast.err(errorMessage(e)))}
                >
                  <Icon name="logout" />
                </button>
              </footer>
            </>
          )}
          {panel === 'quick' && (
            <>
              <div className="eos-panel-heading">
                <h2>{t('desktop.quick')}</h2>
                <Icon name="cog" />
              </div>
              <Connectivity />
              <div className="eos-quick-grid">
                <button onClick={toggleDark}>
                  <Icon name={isDark ? 'moon' : 'sun'} />
                  <b>{t(isDark ? 'desktop.dark' : 'desktop.light')}</b>
                </button>
                <button
                  onClick={() => {
                    if (isUnlocked) void lock().catch((e) => toast.err(errorMessage(e)));
                    else {
                      setPanel(null);
                      open(apps.find((a) => a.key === 'settings')!);
                    }
                  }}
                >
                  <Icon name={isUnlocked ? 'unlock' : 'shield'} />
                  <b>{t(isUnlocked ? 'desktop.lockAdmin' : 'desktop.permissions')}</b>
                </button>
                <button onClick={() => createFolder(null)}>
                  <Icon name="folderplus" />
                  <b>{t('desktop.newFolder')}</b>
                </button>
                <button onClick={() => open(apps.find((a) => a.key === 'settings')!)}>
                  <Icon name="cog" />
                  <b>{t('nav.settings')}</b>
                </button>
              </div>
              <h3>{t('desktop.wallpaper')}</h3>
              <div className="eos-wallpaper-options">
                {(['bloom', 'mint', 'sunset', 'plain'] as const).map((wallpaper) => (
                  <button
                    key={wallpaper}
                    aria-pressed={data.wallpaper === wallpaper}
                    title={t(`desktop.wallpaper_${wallpaper}`)}
                    className={`eos-wall-swatch eos-wall-swatch--${wallpaper}`}
                    onClick={() => changeData((d) => ({ ...d, wallpaper }))}
                  >
                    {data.wallpaper === wallpaper && <Icon name="check" />}
                  </button>
                ))}
              </div>
              <p className="eos-panel-note">
                <Icon name="server" />
                {host?.hostname}
                <span>{host?.distro.name}</span>
              </p>
            </>
          )}
          {panel === 'notices' && (
            <>
              <div className="eos-panel-heading">
                <h2>{t('desktop.notices')}</h2>
                <button onClick={clearNotices}>{t('desktop.clearAll')}</button>
              </div>
              <div className="eos-notices">
                {notices.length ? (
                  notices.map((n) => (
                    <button
                      key={n.id}
                      onClick={() => {
                        markAllRead();
                        setPanel(null);
                        if (n.to) navigate(n.to);
                      }}
                    >
                      <Icon name={n.icon ?? 'bell'} />
                      <span>
                        <b>{n.title}</b>
                        <p>{n.detail}</p>
                        <small>{relativeTime(n.at)}</small>
                      </span>
                    </button>
                  ))
                ) : (
                  <div className="eos-empty">
                    <Icon name="bell" size={36} />
                    <p>{t('notifications.empty')}</p>
                  </div>
                )}
              </div>
              <div className="eos-calendar">
                <span>{formatDate(clock, { weekday: 'long', month: 'long', day: 'numeric' })}</span>
                <b>{formatTime(clock)}</b>
              </div>
            </>
          )}
        </div>
      )}
      {context && (
        <div
          ref={contextRef}
          className="eos-context"
          role="menu"
          style={{ left: context.x, top: context.y }}
        >
          {contextApp && (
            <>
              <button role="menuitem" onClick={() => open(contextApp)}>
                <Icon name="play" />
                {t('desktop.open')}
              </button>
              <button
                role="menuitem"
                onClick={() => {
                  changeData((d) => addShortcut(d, contextApp.key, null));
                  setContext(null);
                }}
              >
                <Icon name="plus" />
                {t('desktop.addDesktop')}
              </button>
              <button
                role="menuitem"
                onClick={() => {
                  togglePin(contextApp.key);
                  setContext(null);
                }}
              >
                <Icon name="star" />
                {t(data.pinned.includes(contextApp.key) ? 'desktop.unpin' : 'desktop.pin')}
              </button>
            </>
          )}
          {folderItem && (
            <>
              <button
                role="menuitem"
                onClick={() => {
                  openFolder(folderItem.id);
                  setContext(null);
                }}
              >
                <Icon name="files" />
                {t('desktop.open')}
              </button>
              <button role="menuitem" onClick={() => renameFolder(folderItem.id)}>
                <Icon name="edit" />
                {t('desktop.rename')}
              </button>
            </>
          )}
          {directoryItem && (
            <>
              <button role="menuitem" onClick={() => openDirectory(directoryItem.path)}>
                <Icon name="files" />
                {t('desktop.open')}
              </button>
              <button
                role="menuitem"
                onClick={() => {
                  setDirectoryEdit({ id: directoryItem.id, folder: directoryItem.folder });
                  setContext(null);
                }}
              >
                <Icon name="edit" />
                {t('desktop.editDirectoryShortcut')}
              </button>
            </>
          )}
          {(folderItem || shortcutItem || directoryItem) && (
            <>
              <button
                role="menuitem"
                onClick={() => {
                  setMoveId(context.item!);
                  setContext(null);
                }}
              >
                <Icon name="files" />
                {t('desktop.moveTo')}
              </button>
              <button
                role="menuitem"
                onClick={() => {
                  changeData((d) =>
                    folderItem
                      ? removeFolder(d, folderItem.id)
                      : directoryItem
                        ? {
                            ...d,
                            directoryShortcuts: d.directoryShortcuts.filter(
                              (s) => s.id !== context.item,
                            ),
                          }
                        : { ...d, shortcuts: d.shortcuts.filter((s) => s.id !== context.item) },
                  );
                  if (folderItem)
                    setWindows((list) => list.filter((w) => w.app !== `folder:${folderItem.id}`));
                  setContext(null);
                }}
              >
                <Icon name="trash" />
                {t('desktop.removeShortcut')}
              </button>
            </>
          )}
          {!context.item && !context.app && (
            <>
              <button role="menuitem" onClick={() => createFolder(context.folder ?? null)}>
                <Icon name="folderplus" />
                {t('desktop.newFolder')}
              </button>
              <button
                role="menuitem"
                disabled={data.directoryShortcuts.length >= 256}
                onClick={() => {
                  setDirectoryEdit({ folder: context.folder ?? null });
                  setContext(null);
                }}
              >
                <Icon name="files" />
                {t('desktop.newDirectoryShortcut')}
              </button>
              <button
                role="menuitem"
                onClick={() => {
                  setContext(null);
                  setQuery('');
                  setPanel('start');
                }}
              >
                <Icon name="grid" />
                {t('desktop.allApps')}
              </button>
              <button
                role="menuitem"
                onClick={() => {
                  setContext(null);
                  setPanel('quick');
                }}
              >
                <Icon name="palette" />
                {t('desktop.personalize')}
              </button>
            </>
          )}
        </div>
      )}
      <footer className="eos-taskbar">
        <div className="eos-taskbar-left">
          <button className="eos-taskbar-host" onClick={() => open(apps[0])}>
            <Icon name="server" />
            <span>{host?.hostname}</span>
          </button>
        </div>
        <div className="eos-taskbar-center">
          <button
            className={`eos-start-button${panel === 'start' ? ' is-on' : ''}`}
            aria-label={t('desktop.start')}
            aria-expanded={panel === 'start'}
            onClick={() => togglePanel('start')}
          >
            <BrandMark size={30} />
          </button>
          <button
            className="eos-task-search"
            aria-label={t('desktop.search')}
            onClick={() => {
              setQuery('');
              setPanel('start');
            }}
          >
            <Icon name="search" size={18} />
            <span>{t('desktop.searchShort')}</span>
          </button>
          <button
            className="eos-task-view"
            title={t('desktop.showDesktop')}
            aria-label={t('desktop.showDesktop')}
            onClick={() => {
              const any = windows.some((w) => !w.minimized);
              setWindows((list) => list.map((w) => ({ ...w, minimized: any })));
            }}
          >
            <Icon name="columns" />
          </button>
          {taskApps.map((app) => {
            const win = windows.find((w) => w.app === app.key);
            return (
              <button
                key={app.key}
                className={`eos-task-app ${app.hueClass}${win ? ' is-running' : ''}${win && win.id === active ? ' is-active' : ''}`}
                title={app.label}
                aria-label={app.label}
                aria-pressed={!!win && win.id === active}
                onClick={() => {
                  if (!win) open(app);
                  else if (active === win.id) patch(win.id, { minimized: true });
                  else focus(win.id);
                }}
                onContextMenu={(e) => menu(e, { app: app.key })}
              >
                <AppIcon app={app} size={25} />
              </button>
            );
          })}
          {windows
            .filter((w) => w.app.startsWith('folder:'))
            .map((win) => (
              <button
                key={win.id}
                className={`eos-task-app hue-file is-running${win.id === active ? ' is-active' : ''}`}
                title={data.folders.find((f) => `folder:${f.id}` === win.app)?.name}
                onClick={() => focus(win.id)}
              >
                <Icon name="files" size={25} />
              </button>
            ))}
          {ai && (
            <button
              className="eos-ai-button"
              title={t('desktop.assistant')}
              aria-label={t('desktop.assistant')}
              onClick={() => open(ai)}
            >
              <Icon name="zap" size={22} />
            </button>
          )}
        </div>
        <div className="eos-tray">
          <button
            className="eos-quick-button"
            aria-label={t('desktop.quick')}
            aria-expanded={panel === 'quick'}
            onClick={() => togglePanel('quick')}
          >
            <Icon name={isUnlocked ? 'unlock' : 'shield'} size={17} />
            <Icon name={isDark ? 'moon' : 'sun'} size={17} />
          </button>
          <button
            className="eos-clock"
            aria-label={t('desktop.notices')}
            aria-expanded={panel === 'notices'}
            onClick={() => togglePanel('notices')}
          >
            <span>{formatTime(clock)}</span>
            <small>{formatDate(clock)}</small>
            {unread > 0 && <b>{unread}</b>}
          </button>
        </div>
      </footer>
      {directoryEdit && (
        <DirectoryShortcutDialog
          initial={data.directoryShortcuts.find((item) => item.id === directoryEdit.id)}
          home={session?.home ?? '/'}
          onClose={() => setDirectoryEdit(null)}
          onSave={(value) => {
            changeData((current) => ({
              ...current,
              directoryShortcuts: directoryEdit.id
                ? current.directoryShortcuts.map((item) =>
                    item.id === directoryEdit.id ? { ...item, ...value } : item,
                  )
                : current.directoryShortcuts.length < 256
                  ? [
                      ...current.directoryShortcuts,
                      { ...value, id: crypto.randomUUID(), folder: directoryEdit.folder },
                    ]
                  : current.directoryShortcuts,
            }));
            setDirectoryEdit(null);
          }}
        />
      )}
      <Dialog
        open={!!editing}
        onClose={() => setEditing(null)}
        title={t(editing?.kind === 'rename' ? 'desktop.rename' : 'desktop.newFolder')}
        description={t('desktop.virtualHint')}
        icon="files"
        onSubmit={(e) => {
          e.preventDefault();
          completeEdit();
        }}
        footer={
          <>
            <Button onClick={() => setEditing(null)}>{t('desktop.cancel')}</Button>
            <Button type="submit" variant="primary" disabled={!name.trim()}>
              {t('desktop.save')}
            </Button>
          </>
        }
      >
        <Input
          data-autofocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={80}
          label={t('desktop.folderName')}
        />
      </Dialog>
      <Dialog
        open={!!moveId}
        onClose={() => setMoveId(null)}
        title={t('desktop.moveTo')}
        description={t('desktop.virtualHint')}
        icon="files"
      >
        <div className="eos-move-list">
          <button
            onClick={() => {
              changeData((d) => moveItem(d, moveId!, null));
              setMoveId(null);
            }}
          >
            <Icon name="grid" />
            {t('desktop.desktop')}
          </button>
          {data.folders
            .filter((f) => f.id !== moveId)
            .map((f) => (
              <button
                key={f.id}
                onClick={() => {
                  changeData((d) => moveItem(d, moveId!, f.id));
                  setMoveId(null);
                }}
              >
                <Icon name="files" />
                {f.name}
              </button>
            ))}
        </div>
      </Dialog>
      <Dialog
        open={upload !== null}
        onClose={() => {
          if (!uploadBusy) setUpload(null);
        }}
        title={t('desktop.uploadTitle', { count: upload?.length ?? 0 })}
        description={t('desktop.uploadHint')}
        icon="upload"
        onSubmit={(e) => {
          e.preventDefault();
          if (!upload || uploadBusy) return;
          setUploadBusy(true);
          setUploadError('');
          void (async () => {
            const [{ isDirLike }, { uploadInto }] = await Promise.all([
              import('../sections/files/types'),
              import('../sections/files/filesOps'),
            ]);
            const entry = await call<import('../sections/files/types').FEntry>('files.stat', {
              path: uploadDest.trim(),
            });
            if (!isDirLike(entry)) throw new Error(t('desktop.invalidDestination'));
            open(
              apps.find((a) => a.key === 'files')!,
              `/files?path=${encodeURIComponent(uploadDest.trim())}`,
            );
            await uploadInto(
              upload,
              uploadDest.trim(),
              false,
              () => undefined,
              (key, vars) => filesT(key, vars),
            );
            setUpload(null);
          })()
            .catch((error: unknown) => setUploadError(errorMessage(error)))
            .finally(() => setUploadBusy(false));
        }}
        footer={
          <>
            <Button onClick={() => setUpload(null)} disabled={uploadBusy}>
              {t('desktop.cancel')}
            </Button>
            <Button
              type="submit"
              variant="primary"
              icon="upload"
              loading={uploadBusy}
              disabled={!uploadDest.trim()}
            >
              {t('desktop.upload')}
            </Button>
          </>
        }
      >
        <Input
          data-autofocus
          value={uploadDest}
          onChange={(e) => setUploadDest(e.target.value)}
          label={t('desktop.destination')}
          mono
        />
        <p className="eos-upload-list">
          {upload
            ?.slice(0, 8)
            .map((item) => item.rel)
            .join(', ')}
          {upload && upload.length > 8 ? '…' : ''}
        </p>
        {uploadError && <p role="alert">{uploadError}</p>}
      </Dialog>
    </div>
  );
}
