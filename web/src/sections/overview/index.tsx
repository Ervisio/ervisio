import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent as RPointerEvent,
  type RefObject,
} from 'react';
import { usePrefs, useSession } from '../../api';
import { useT, type TFn } from '../../i18n';
import { usePlugins, type PluginWidgetInfo } from '../../plugins';
import { Button, ConfirmDialog, Icon, Page, Sheet, useMediaQuery } from '../../ui';
import { runAction } from './actions';
import { OutputDialog } from './OutputDialog';
import { Library, type LibItem } from './Library';
import { SettingsDialog } from './SettingsDialog';
import { RefreshSelect } from '../../lib/RefreshSelect';
import { refreshNow, useSetRefreshInterval } from '../../lib/refresh';
import { useAlerts, useHost } from './data';
import { setWindowsDefaults, normalizeLayout, type DashAction, type Widget } from './model';
import { WidgetBody, type WidgetCtx } from './widgets';
import {
  useActionConfirm,
  useDashboardDraft,
  useWidgetEdits,
  useWidgetSettings,
} from './overviewEditor';
import './overview.css';

const DRAG_THRESHOLD = 5;
const EDGE_SCROLL_ZONE = 70;
const EDGE_SCROLL_STEP = 14;

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

interface WidgetItemProps {
  t: TFn;
  w: Widget;
  index: number;
  total: number;
  name: string;
  editing: boolean;
  dragUi: DragUi | null;
  ctx: WidgetCtx;
  onPointerDown: (e: RPointerEvent<HTMLDivElement>) => void;
  onKeyDown: (e: KeyboardEvent<HTMLDivElement>) => void;
  onSettings: () => void;
  onRemove: () => void;
  onResizeDown: (e: RPointerEvent<HTMLElement>) => void;
}

function WidgetItem({
  t,
  w,
  index,
  total,
  name,
  editing,
  dragUi,
  ctx,
  onPointerDown,
  onKeyDown,
  onSettings,
  onRemove,
  onResizeDown,
}: WidgetItemProps) {
  return (
    <div
      role="listitem"
      data-wid={w.id}
      className={widgetClass(w, dragUi)}
      tabIndex={editing ? 0 : undefined}
      aria-roledescription={editing ? t('edit.roleDesc') : undefined}
      aria-label={editing ? t('edit.widgetLabel', { name, pos: index + 1, total }) : undefined}
      onPointerDown={editing ? onPointerDown : undefined}
      onKeyDown={editing ? onKeyDown : undefined}
    >
      {editing && (
        <WidgetTools
          t={t}
          name={name}
          onSettings={onSettings}
          onRemove={onRemove}
          onResizeDown={onResizeDown}
        />
      )}
      <div className="ov-w-body" {...(editing ? ({ inert: '' } as object) : {})}>
        <WidgetBody w={w} ctx={ctx} />
      </div>
    </div>
  );
}

function DropZone({
  t,
  dropRef,
  isOver,
}: {
  t: TFn;
  dropRef: RefObject<HTMLDivElement>;
  isOver: boolean;
}) {
  return (
    <div ref={dropRef} className={`ov-drop${isOver ? ' ov-drop--on' : ''}`}>
      <div>
        <Icon name="plus" />
        {t('edit.drop')}
      </div>
    </div>
  );
}

function OverviewActions({ t, onEdit }: { t: TFn; onEdit: () => void }) {
  const tc = useT('common');
  const setRefresh = useSetRefreshInterval();
  return (
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
      <Button icon="edit" onClick={onEdit}>
        {t('personalize')}
      </Button>
    </>
  );
}

interface ActionConfirmDialogProps {
  t: TFn;
  action: DashAction | null;
  onClose: () => void;
}

function ActionConfirmDialog({ t, action, onClose }: ActionConfirmDialogProps) {
  return (
    <ConfirmDialog
      open={!!action}
      onClose={onClose}
      onConfirm={() => {
        if (action) void runAction(action, t);
      }}
      danger={false}
      icon="play"
      title={t('action.confirmTitle', { name: action?.label ?? '' })}
      description={
        action
          ? `${action.argv.join(' ')}${action.admin ? ` (${t('action.asAdmin')})` : ''}`
          : undefined
      }
      confirmLabel={t('action.run')}
    />
  );
}

export default function OverviewPage() {
  const t = useT('overview');
  const { prefs, set } = usePrefs();
  const { session } = useSession();
  setWindowsDefaults(session?.os === 'windows');
  const host = useHost();
  const { alerts } = useAlerts();
  const { widgets: pluginWidgets } = usePlugins();
  const wide = useMediaQuery('(min-width: 1180px)');

  const saved = useMemo(() => normalizeLayout(prefs.dashboard).widgets, [prefs.dashboard]);
  const { confirm, closeConfirm, run } = useActionConfirm(t);
  const dash = useDashboardDraft(saved, set, t);
  const { editing, list } = dash;
  const [libOpen, setLibOpen] = useState(false);
  const dropRef = useRef<HTMLDivElement>(null);
  const settings = useWidgetSettings(list, saved, editing, dash.update, set);
  const edits = useWidgetEdits({
    t,
    draftRef: dash.draftRef,
    update: dash.update,
    reset: dash.reset,
    openSettings: settings.openSettings,
    closeLibrary: () => setLibOpen(false),
  });
  const { dragUi, onWidgetPointerDown, onLibPointerDown } = useWidgetDrag({
    gridRef: edits.gridRef,
    dropRef,
    draftRef: dash.draftRef,
    moveWidget: edits.moveWidget,
    addItem: edits.addItem,
    announceMoved: edits.announceMoved,
  });
  const { openSettings } = settings;
  const ctx: WidgetCtx = useMemo(
    () => ({ editing, run, configure: (id) => openSettings(id) }),
    [editing, run, openSettings],
  );

  const name = session?.name?.trim().split(/\s+/)[0] || session?.user || '';
  const summary = dashboardSummary(t, alerts, host?.hostname || t('summary.thisMachine'));
  const library = (
    <Library
      onAdd={(i) => edits.addItem(i)}
      onPointerDown={onLibPointerDown}
      onReset={edits.resetLayout}
      className={wide ? '' : 'ov-lib--sheet'}
    />
  );

  return (
    <Page
      hue="ov"
      title={editing ? undefined : t('greeting', { name })}
      subtitle={editing ? undefined : summary}
      actions={editing ? undefined : <OverviewActions t={t} onEdit={dash.startEdit} />}
    >
      <div className={`ov${editing ? ' ov--editing' : ''}${editing && wide ? ' ov--lib' : ''}`}>
        <div className="ov-main">
          {editing && (
            <EditBar
              t={t}
              wide={wide}
              onAdd={() => setLibOpen(true)}
              onCancel={dash.cancel}
              onSave={() => void dash.save()}
            />
          )}
          <div className="ov-canvas">
            <div ref={edits.gridRef} className="ov-grid" role="list">
              {list.map((w, i) => (
                <WidgetItem
                  key={w.id}
                  t={t}
                  w={w}
                  index={i}
                  total={list.length}
                  name={edits.nameOf(w)}
                  editing={editing}
                  dragUi={dragUi}
                  ctx={ctx}
                  onPointerDown={(e) => onWidgetPointerDown(w, e)}
                  onKeyDown={(e) => edits.onWidgetKey(w, i, e)}
                  onSettings={() => openSettings(w.id)}
                  onRemove={() => edits.removeWidget(w.id)}
                  onResizeDown={(e) => edits.onResizeDown(w, e)}
                />
              ))}
              {editing && <DropZone t={t} dropRef={dropRef} isOver={!!dragUi?.overEnd} />}
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
        {edits.announce}
      </div>

      <SettingsDialog
        widget={settings.settingsWidget}
        pluginTitle={pluginTitleOf(settings.settingsWidget, pluginWidgets)}
        onClose={() => openSettings(null)}
        onSave={settings.saveSettings}
      />
      <OutputDialog />
      <ActionConfirmDialog t={t} action={confirm} onClose={closeConfirm} />
      <ConfirmDialog
        open={dash.isDiscardOpen}
        onClose={dash.closeDiscard}
        onConfirm={dash.discard}
        title={t('edit.discardTitle')}
        description={t('edit.discardText')}
        confirmLabel={t('edit.discard')}
        cancelLabel={t('edit.keepEditing')}
      />
    </Page>
  );
}
