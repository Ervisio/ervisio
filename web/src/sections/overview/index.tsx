import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent as RPointerEvent,
  type RefObject,
} from 'react';
import { useSearchParams } from 'react-router-dom';
import { usePrefs, useSession } from '../../api';
import { useT, type TFn } from '../../i18n';
import { usePlugins, type PluginWidgetInfo } from '../../plugins';
import { Button, ConfirmDialog, Icon, Page, Sheet, toast, useMediaQuery } from '../../ui';
import { requestConfirm, runAction, useConfirmRequest } from './actions';
import { OutputDialog } from './OutputDialog';
import { Library, type LibItem } from './Library';
import { SettingsDialog } from './SettingsDialog';
import { RefreshSelect } from '../../lib/RefreshSelect';
import { refreshNow, useSetRefreshInterval } from '../../lib/refresh';
import { useAlerts, useHost } from './data';
import {
  defaultLayout,
  setWindowsDefaults,
  normalizeLayout,
  snapCols,
  stepCols,
  uid,
  type DashAction,
  type Widget,
} from './model';
import { WidgetBody, widgetTitle, type WidgetCtx } from './widgets';
import './overview.css';

const GAP = 14;
const GRID_COLUMNS = 12;
const DRAG_THRESHOLD = 5;
const EDGE_SCROLL_ZONE = 70;
const EDGE_SCROLL_STEP = 14;

function move<T>(list: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || to >= list.length) return list;
  const next = list.slice();
  const [x] = next.splice(from, 1);
  next.splice(to, 0, x);
  return next;
}

interface DragUi {
  kind: 'widget' | 'new';
  id?: string;
  overId?: string;
  overEnd?: boolean;
}

interface DragRun {
  kind: 'widget' | 'new';
  id?: string;
  item?: LibItem;
  el: HTMLElement;
  sx: number;
  sy: number;
  px: number;
  py: number;
  started: boolean;
  ghost?: HTMLElement;
  overId?: string;
  overEnd?: boolean;
  raf?: number;
}

type WidgetKeyAction = 'grow' | 'shrink' | 'prev' | 'next' | 'settings' | 'remove';

const WIDGET_KEYS = new Map<string, WidgetKeyAction>([
  ['ArrowLeft', 'prev'],
  ['ArrowUp', 'prev'],
  ['ArrowRight', 'next'],
  ['ArrowDown', 'next'],
  ['Enter', 'settings'],
  [' ', 'settings'],
  ['Delete', 'remove'],
  ['Backspace', 'remove'],
]);

function widgetKeyAction(e: KeyboardEvent): WidgetKeyAction | undefined {
  if (e.shiftKey && e.key === 'ArrowRight') return 'grow';
  if (e.shiftKey && e.key === 'ArrowLeft') return 'shrink';
  return WIDGET_KEYS.get(e.key);
}

function isInside(el: Element | null | undefined, x: number, y: number): boolean {
  if (!el) return false;
  const r = el.getBoundingClientRect();
  return x >= r.left && x <= r.right && y >= r.top && y <= r.bottom;
}

function widgetAt(grid: HTMLElement | null, x: number, y: number): string | undefined {
  for (const el of grid?.querySelectorAll<HTMLElement>('[data-wid]') ?? []) {
    if (isInside(el, x, y)) return el.dataset.wid;
  }
  return undefined;
}

function createGhost(el: HTMLElement): HTMLElement {
  const r = el.getBoundingClientRect();
  const g = el.cloneNode(true) as HTMLElement;
  g.removeAttribute('data-wid');
  g.classList.add('ov-ghost');
  Object.assign(g.style, {
    position: 'fixed',
    left: `${r.left}px`,
    top: `${r.top}px`,
    width: `${r.width}px`,
    height: `${r.height}px`,
    margin: '0',
    zIndex: '1000',
    pointerEvents: 'none',
  });
  document.body.appendChild(g);
  return g;
}

// Scrolls while the pointer of a running drag sits near the top or bottom edge.
function startEdgeScroll(dragRun: RefObject<DragRun | null>, scroller: HTMLElement | null): number {
  const tick = () => {
    const cur = dragRun.current;
    if (!cur || !cur.started) return;
    const host = scroller ?? document.scrollingElement;
    if (host) {
      const rect = scroller
        ? scroller.getBoundingClientRect()
        : { top: 0, bottom: window.innerHeight };
      if (cur.py < rect.top + EDGE_SCROLL_ZONE) host.scrollTop -= EDGE_SCROLL_STEP;
      else if (cur.py > rect.bottom - EDGE_SCROLL_ZONE) host.scrollTop += EDGE_SCROLL_STEP;
    }
    cur.raf = requestAnimationFrame(tick);
  };
  return requestAnimationFrame(tick);
}

function trackResize(
  grid: HTMLElement,
  el: HTMLElement,
  startX: number,
  onCols: (cols: number) => void,
  onEnd: () => void,
) {
  const colW = (grid.getBoundingClientRect().width + GAP) / GRID_COLUMNS;
  const startW = el.getBoundingClientRect().width;
  const handleMove = (ev: PointerEvent) => onCols((startW + (ev.clientX - startX) + GAP) / colW);
  const handleUp = () => {
    window.removeEventListener('pointermove', handleMove);
    window.removeEventListener('pointerup', handleUp);
    window.removeEventListener('pointercancel', handleUp);
    document.body.classList.remove('ov-dragging');
    onEnd();
  };
  document.body.classList.add('ov-dragging');
  window.addEventListener('pointermove', handleMove);
  window.addEventListener('pointerup', handleUp);
  window.addEventListener('pointercancel', handleUp);
}

function dashboardSummary(t: TFn, alerts: { severity: string }[] | null, host: string): string {
  if (alerts === null) return t('summary.checking', { host });
  const issues = alerts.filter((a) => a.severity !== 'ok').length;
  return issues === 0 ? t('summary.ok', { host }) : t('summary.issues', { host, count: issues });
}

function pluginTitleOf(widget: Widget | null, plugins: PluginWidgetInfo[]): string | undefined {
  if (widget?.type !== 'plugin') return undefined;
  return plugins.find(
    (p) => p.plugin === widget.settings?.plugin && p.id === widget.settings?.widget,
  )?.title;
}

function widgetClass(w: Widget, dragUi: DragUi | null): string {
  const dragged = dragUi?.id === w.id ? ' ov-w--drag' : '';
  const target = dragUi?.overId === w.id && dragUi.kind === 'new' ? ' ov-w--target' : '';
  return `ov-w ov-c${w.cols}${dragged}${target}`;
}

interface WidgetDragOptions {
  gridRef: RefObject<HTMLDivElement | null>;
  dropRef: RefObject<HTMLDivElement | null>;
  draftRef: RefObject<Widget[] | null>;
  moveWidget: (id: string, to: number, say?: boolean) => void;
  addItem: (item: LibItem, at?: number) => void;
  announceMoved: (id: string) => void;
}

// Pointer drag for widgets and library items.
function useWidgetDrag({
  gridRef,
  dropRef,
  draftRef,
  moveWidget,
  addItem,
  announceMoved,
}: WidgetDragOptions) {
  const [dragUi, setDragUi] = useState<DragUi | null>(null);
  const dragRun = useRef<DragRun | null>(null);
  const endDrag = useRef<() => void>(() => undefined);
  const onMove = useRef<(e: PointerEvent) => void>(() => undefined);
  const onUp = useRef<(e: PointerEvent) => void>(() => undefined);

  const reorderOver = (d: DragRun, over: string | undefined) => {
    if (!d.id || !over || over === d.id) return;
    const idx = draftRef.current?.findIndex((w) => w.id === over) ?? -1;
    moveWidget(d.id, idx, false);
  };

  onMove.current = (e) => {
    const d = dragRun.current;
    if (!d) return;
    d.px = e.clientX;
    d.py = e.clientY;
    if (!d.started) {
      if (Math.hypot(e.clientX - d.sx, e.clientY - d.sy) < DRAG_THRESHOLD) return;
      d.started = true;
      d.ghost = createGhost(d.el);
      document.body.classList.add('ov-dragging');
      setDragUi({ kind: d.kind, id: d.id });
      d.raf = startEdgeScroll(dragRun, d.el.closest('.app-content') as HTMLElement | null);
    }
    if (d.ghost)
      d.ghost.style.transform = `translate(${e.clientX - d.sx}px, ${e.clientY - d.sy}px)`;
    const over = widgetAt(gridRef.current, e.clientX, e.clientY);
    if (d.kind === 'widget') {
      reorderOver(d, over);
      return;
    }
    const overEnd =
      !over &&
      (isInside(dropRef.current, e.clientX, e.clientY) ||
        isInside(gridRef.current, e.clientX, e.clientY));
    if (over !== d.overId || overEnd !== d.overEnd) {
      d.overId = over;
      d.overEnd = overEnd;
      setDragUi({ kind: 'new', overId: over, overEnd });
    }
  };

  endDrag.current = () => {
    const d = dragRun.current;
    window.removeEventListener('pointermove', handlePointerMove);
    window.removeEventListener('pointerup', handlePointerUp);
    window.removeEventListener('pointercancel', handlePointerCancel);
    window.removeEventListener('keydown', handleEscape);
    if (d?.raf) cancelAnimationFrame(d.raf);
    d?.ghost?.remove();
    document.body.classList.remove('ov-dragging');
    dragRun.current = null;
    setDragUi(null);
  };
  const dropNewItem = (
    item: LibItem,
    started: boolean,
    dropOver: string | undefined,
    dropEnd?: boolean,
  ) => {
    if (!started)
      addItem(item); // a plain click adds at the end
    else if (dropOver)
      addItem(
        item,
        draftRef.current?.findIndex((w) => w.id === dropOver),
      );
    else if (dropEnd) addItem(item);
  };
  onUp.current = (e) => {
    const d = dragRun.current;
    if (!d) return;
    const dropOver = d.overId;
    const dropEnd = d.overEnd;
    const ok = e.type === 'pointerup';
    endDrag.current();
    if (!ok) return;
    if (d.kind === 'new' && d.item) dropNewItem(d.item, d.started, dropOver, dropEnd);
    else if (d.kind === 'widget' && d.started && d.id) announceMoved(d.id);
  };
  // stable listener identities, so they can be removed
  const handlePointerMove = useCallback((e: PointerEvent) => onMove.current(e), []);
  const handlePointerUp = useCallback((e: PointerEvent) => onUp.current(e), []);
  const handlePointerCancel = handlePointerUp;
  const handleEscape = useCallback((e: globalThis.KeyboardEvent) => {
    if (e.key === 'Escape') endDrag.current();
  }, []);
  useEffect(() => () => endDrag.current(), []);

  const beginDrag = (run: Omit<DragRun, 'started' | 'px' | 'py'>, e: RPointerEvent) => {
    if (dragRun.current) return;
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    dragRun.current = { ...run, started: false, px: e.clientX, py: e.clientY };
    window.addEventListener('pointermove', handlePointerMove);
    window.addEventListener('pointerup', handlePointerUp);
    window.addEventListener('pointercancel', handlePointerCancel);
    window.addEventListener('keydown', handleEscape);
  };

  const onWidgetPointerDown = (w: Widget, e: RPointerEvent<HTMLDivElement>) => {
    const tg = e.target as HTMLElement;
    if (tg.closest('[data-nodrag]')) return;
    if (e.pointerType === 'touch' && !tg.closest('[data-grip]')) return;
    if (e.pointerType !== 'touch') e.preventDefault();
    beginDrag({ kind: 'widget', id: w.id, el: e.currentTarget, sx: e.clientX, sy: e.clientY }, e);
    if (e.pointerType !== 'touch') e.currentTarget.focus({ preventScroll: true });
  };
  const onLibPointerDown = (item: LibItem, e: RPointerEvent<HTMLElement>) => {
    if (e.pointerType === 'touch') return; // touch uses the click handler below
    e.preventDefault();
    beginDrag({ kind: 'new', item, el: e.currentTarget, sx: e.clientX, sy: e.clientY }, e);
  };

  return { dragUi, onWidgetPointerDown, onLibPointerDown };
}

interface EditBarProps {
  t: TFn;
  wide: boolean;
  onAdd: () => void;
  onCancel: () => void;
  onSave: () => void;
}

function EditBar({ t, wide, onAdd, onCancel, onSave }: EditBarProps) {
  return (
    <div className="ov-editbar" role="region" aria-label={t('edit.title')}>
      <Icon name="edit" />
      <b>{t('edit.title')}</b>
      <span>{t('edit.hint')}</span>
      <div className="ov-editbar-r">
        {!wide && (
          <Button size="sm" icon="plus" variant="ghost" onClick={onAdd}>
            {t('edit.addWidget')}
          </Button>
        )}
        <Button size="sm" variant="ghost" onClick={onCancel}>
          {t('cancel')}
        </Button>
        <Button size="sm" className="ov-save" onClick={onSave}>
          {t('edit.save')}
        </Button>
      </div>
    </div>
  );
}

interface WidgetToolsProps {
  t: TFn;
  name: string;
  onSettings: () => void;
  onRemove: () => void;
  onResizeDown: (e: RPointerEvent<HTMLElement>) => void;
}

function WidgetTools({ t, name, onSettings, onRemove, onResizeDown }: WidgetToolsProps) {
  return (
    <>
      <div className="ov-tools">
        <button
          type="button"
          className="ov-grip"
          data-grip
          aria-label={t('edit.drag')}
          title={t('edit.drag')}
        >
          <Icon name="grip" />
        </button>
        <button
          type="button"
          data-nodrag
          aria-label={t('edit.settings', { name })}
          title={t('edit.settingsShort')}
          onClick={onSettings}
        >
          <Icon name="edit" />
        </button>
        <button
          type="button"
          data-nodrag
          aria-label={t('edit.remove', { name })}
          title={t('edit.removeShort')}
          onClick={onRemove}
        >
          <Icon name="plus" style={{ transform: 'rotate(45deg)' }} />
        </button>
      </div>
      <span className="ov-res" data-nodrag onPointerDown={onResizeDown} aria-hidden="true" />
    </>
  );
}

export default function OverviewPage() {
  const t = useT('overview');
  const tc = useT('common');
  const setRefresh = useSetRefreshInterval();
  const { prefs, set } = usePrefs();
  const { session } = useSession();
  setWindowsDefaults(session?.os === 'windows');
  const host = useHost();
  const { alerts } = useAlerts();
  const { widgets: pluginWidgets } = usePlugins();
  const wide = useMediaQuery('(min-width: 1180px)');
  const [params, setParams] = useSearchParams();

  const saved = useMemo(() => normalizeLayout(prefs.dashboard).widgets, [prefs.dashboard]);
  const [draft, setDraft] = useState<Widget[] | null>(null);
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const editing = draft !== null;
  const list = draft ?? saved;

  const [settingsId, setSettingsId] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<DashAction | null>(null);
  const [discard, setDiscard] = useState(false);
  const [libOpen, setLibOpen] = useState(false);
  const [announce, setAnnounce] = useState('');
  const [focusReq, setFocusReq] = useState<{ id: string; n: number } | null>(null);
  const gridRef = useRef<HTMLDivElement>(null);
  const dropRef = useRef<HTMLDivElement>(null);

  const confirmReq = useConfirmRequest();
  useEffect(() => {
    if (confirmReq) {
      setConfirm(confirmReq);
      requestConfirm(null);
    }
  }, [confirmReq]);

  const startEdit = useCallback(
    () => setDraft(JSON.parse(JSON.stringify(saved)) as Widget[]),
    [saved],
  );
  useEffect(() => {
    if (params.get('personalize')) {
      startEdit();
      const next = new URLSearchParams(params);
      next.delete('personalize');
      setParams(next, { replace: true });
    }
  }, [params, setParams, startEdit]);

  const dirty = editing && JSON.stringify(draft) !== JSON.stringify(saved);
  const cancel = () => (dirty ? setDiscard(true) : setDraft(null));
  const save = async () => {
    if (!draft) return;
    try {
      await set('dashboard', { version: 1, widgets: draft });
      toast.ok(t('saved'));
      setDraft(null);
    } catch (e) {
      toast.err(t('saveFailed'), e instanceof Error ? e.message : undefined);
    }
  };

  const update = useCallback(
    (fn: (l: Widget[]) => Widget[]) => setDraft((d) => (d ? fn(d) : d)),
    [],
  );
  const nameOf = useCallback((w: Widget) => widgetTitle(w, t), [t]);

  const moveWidget = (id: string, to: number, say = true) => {
    const cur = draftRef.current;
    if (!cur) return;
    const from = cur.findIndex((w) => w.id === id);
    if (from < 0 || to < 0 || to >= cur.length || from === to) return;
    update((l) => move(l, from, to));
    if (say)
      setAnnounce(t('edit.moved', { name: nameOf(cur[from]), pos: to + 1, total: cur.length }));
  };
  const resizeWidget = (id: string, cols: number, say = false) => {
    const c = snapCols(cols);
    const cur = draftRef.current?.find((w) => w.id === id);
    if (!cur || cur.cols === c) return;
    update((l) => l.map((w) => (w.id === id ? { ...w, cols: c } : w)));
    if (say) setAnnounce(t('edit.resized', { name: nameOf(cur), cols: c }));
  };
  const removeWidget = (id: string) => {
    const cur = draftRef.current;
    const w = cur?.find((x) => x.id === id);
    if (!cur || !w) return;
    const i = cur.indexOf(w);
    update((l) => l.filter((x) => x.id !== id));
    setAnnounce(t('edit.removed', { name: nameOf(w) }));
    const next = cur[i + 1] ?? cur[i - 1];
    if (next) setFocusReq({ id: next.id, n: Date.now() });
  };
  const addItem = (item: LibItem, at?: number) => {
    const w: Widget = { id: uid(), type: item.type, cols: item.cols, settings: item.settings?.() };
    update((l) => {
      const n = l.slice();
      n.splice(at === undefined || at < 0 ? n.length : at, 0, w);
      return n;
    });
    setAnnounce(t('edit.added', { name: item.title }));
    setFocusReq({ id: w.id, n: Date.now() });
    if (item.configure) setSettingsId(w.id);
    setLibOpen(false);
  };
  const resetLayout = () => {
    setDraft(defaultLayout().widgets);
    setAnnounce(t('edit.reset'));
  };
  const announceMoved = (id: string) => {
    const cur = draftRef.current;
    const w = cur?.find((x) => x.id === id);
    if (cur && w)
      setAnnounce(t('edit.moved', { name: nameOf(w), pos: cur.indexOf(w) + 1, total: cur.length }));
  };
  const announceResized = (id: string) => {
    const cur = draftRef.current?.find((x) => x.id === id);
    if (cur) setAnnounce(t('edit.resized', { name: nameOf(cur), cols: cur.cols }));
  };

  useLayoutEffect(() => {
    if (!focusReq) return;
    const el = gridRef.current?.querySelector<HTMLElement>(`[data-wid="${focusReq.id}"]`);
    if (el) {
      el.focus({ preventScroll: true });
      el.scrollIntoView({ block: 'nearest', behavior: 'auto' });
    }
  }, [focusReq]);

  const { dragUi, onWidgetPointerDown, onLibPointerDown } = useWidgetDrag({
    gridRef,
    dropRef,
    draftRef,
    moveWidget,
    addItem,
    announceMoved,
  });

  const onResizeDown = (w: Widget, e: RPointerEvent<HTMLElement>) => {
    e.preventDefault();
    e.stopPropagation();
    const grid = gridRef.current;
    const el = (e.currentTarget.closest('[data-wid]') as HTMLElement) ?? null;
    if (!grid || !el) return;
    trackResize(
      grid,
      el,
      e.clientX,
      (cols) => resizeWidget(w.id, cols),
      () => announceResized(w.id),
    );
  };

  const onWidgetKey = (w: Widget, i: number, e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget) return;
    const action = widgetKeyAction(e);
    if (!action) return;
    e.preventDefault();
    const len = draftRef.current?.length ?? 0;
    switch (action) {
      case 'grow':
      case 'shrink':
        resizeWidget(w.id, stepCols(w.cols, action === 'grow' ? 1 : -1), true);
        break;
      case 'prev':
        moveWidget(w.id, i - 1);
        setFocusReq({ id: w.id, n: Date.now() });
        break;
      case 'next':
        moveWidget(w.id, Math.min(len - 1, i + 1));
        setFocusReq({ id: w.id, n: Date.now() });
        break;
      case 'settings':
        setSettingsId(w.id);
        break;
      case 'remove':
        removeWidget(w.id);
        break;
    }
  };

  const run = useCallback(
    (a: DashAction) => {
      if (a.confirm) setConfirm(a);
      else void runAction(a, t);
    },
    [t],
  );
  const ctx: WidgetCtx = useMemo(
    () => ({ editing, run, configure: (id) => setSettingsId(id) }),
    [editing, run],
  );

  const settingsWidget = list.find((w) => w.id === settingsId) ?? null;
  const pluginTitle = pluginTitleOf(settingsWidget, pluginWidgets);
  const saveSettings = (s: Widget['settings']) => {
    const id = settingsId;
    setSettingsId(null);
    if (!id) return;
    if (editing) update((l) => l.map((w) => (w.id === id ? { ...w, settings: s } : w)));
    else
      void set('dashboard', {
        version: 1,
        widgets: saved.map((w) => (w.id === id ? { ...w, settings: s } : w)),
      });
  };

  const name = session?.name?.trim().split(/\s+/)[0] || session?.user || '';
  const summary = dashboardSummary(t, alerts, host?.hostname || 'this machine');

  const library = (
    <Library
      onAdd={(i) => addItem(i)}
      onPointerDown={onLibPointerDown}
      onReset={resetLayout}
      className={wide ? '' : 'ov-lib--sheet'}
    />
  );

  return (
    <Page
      hue="ov"
      title={editing ? undefined : t('greeting', { name })}
      subtitle={editing ? undefined : summary}
      actions={
        editing ? undefined : (
          <>
            <span className="ov-refresh">
              <RefreshSelect label={tc('refreshRate.label')} onPick={(ms) => void setRefresh(ms)} />
              <Button
                iconOnly
                icon="refresh"
                variant="ghost"
                aria-label={tc('refreshRate.now')}
                title={tc('refreshRate.now')}
                onClick={refreshNow}
              />
            </span>
            <Button icon="edit" onClick={startEdit}>
              {t('personalize')}
            </Button>
          </>
        )
      }
    >
      <div className={`ov${editing ? ' ov--editing' : ''}${editing && wide ? ' ov--lib' : ''}`}>
        <div className="ov-main">
          {editing && (
            <EditBar
              t={t}
              wide={wide}
              onAdd={() => setLibOpen(true)}
              onCancel={cancel}
              onSave={() => void save()}
            />
          )}
          <div className="ov-canvas">
            <div ref={gridRef} className="ov-grid" role="list">
              {list.map((w, i) => (
                <div
                  key={w.id}
                  role="listitem"
                  data-wid={w.id}
                  className={widgetClass(w, dragUi)}
                  tabIndex={editing ? 0 : undefined}
                  aria-roledescription={editing ? t('edit.roleDesc') : undefined}
                  aria-label={
                    editing
                      ? t('edit.widgetLabel', { name: nameOf(w), pos: i + 1, total: list.length })
                      : undefined
                  }
                  onPointerDown={editing ? (e) => onWidgetPointerDown(w, e) : undefined}
                  onKeyDown={editing ? (e) => onWidgetKey(w, i, e) : undefined}
                >
                  {editing && (
                    <WidgetTools
                      t={t}
                      name={nameOf(w)}
                      onSettings={() => setSettingsId(w.id)}
                      onRemove={() => removeWidget(w.id)}
                      onResizeDown={(e) => onResizeDown(w, e)}
                    />
                  )}
                  <div className="ov-w-body" {...(editing ? ({ inert: '' } as object) : {})}>
                    <WidgetBody w={w} ctx={ctx} />
                  </div>
                </div>
              ))}
              {editing && (
                <div ref={dropRef} className={`ov-drop${dragUi?.overEnd ? ' ov-drop--on' : ''}`}>
                  <div>
                    <Icon name="plus" />
                    {t('edit.drop')}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
        {editing && wide && library}
      </div>

      {editing && !wide && (
        <Sheet open={libOpen} onClose={() => setLibOpen(false)} title={t('library.title')}>
          {library}
        </Sheet>
      )}

      <div className="ov-sr" role="status" aria-live="polite">
        {announce}
      </div>

      <SettingsDialog
        widget={settingsWidget}
        pluginTitle={pluginTitle}
        onClose={() => setSettingsId(null)}
        onSave={saveSettings}
      />
      <OutputDialog />
      <ConfirmDialog
        open={!!confirm}
        onClose={() => setConfirm(null)}
        onConfirm={() => {
          if (confirm) void runAction(confirm, t);
        }}
        danger={false}
        icon="play"
        title={t('action.confirmTitle', { name: confirm?.label ?? '' })}
        description={
          confirm
            ? `${confirm.argv.join(' ')}${confirm.admin ? ` (${t('action.asAdmin')})` : ''}`
            : undefined
        }
        confirmLabel={t('action.run')}
      />
      <ConfirmDialog
        open={discard}
        onClose={() => setDiscard(false)}
        onConfirm={() => setDraft(null)}
        title={t('edit.discardTitle')}
        description={t('edit.discardText')}
        confirmLabel={t('edit.discard')}
        cancelLabel={t('edit.keepEditing')}
      />
    </Page>
  );
}
