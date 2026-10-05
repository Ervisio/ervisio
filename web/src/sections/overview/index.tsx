import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent as RPointerEvent } from 'react';
import { useSearchParams } from 'react-router-dom';
import { usePrefs, useSession } from '../../api';
import { useT } from '../../i18n';
import { usePlugins } from '../../plugins';
import { Button, ConfirmDialog, Icon, Page, Sheet, toast, useMediaQuery } from '../../ui';
import { requestConfirm, runAction, useConfirmRequest } from './actions';
import { OutputDialog } from './OutputDialog';
import { Library, type LibItem } from './Library';
import { SettingsDialog } from './SettingsDialog';
import { RefreshSelect } from '../../lib/RefreshSelect';
import { refreshNow, useSetRefreshInterval } from '../../lib/refresh';
import { useAlerts, useHost } from './data';
import { defaultLayout, setWindowsDefaults, normalizeLayout, snapCols, stepCols, uid, type DashAction, type Widget } from './model';
import { WidgetBody, widgetTitle, type WidgetCtx } from './widgets';
import './overview.css';

const GAP = 14;

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
  const [dragUi, setDragUi] = useState<DragUi | null>(null);
  const gridRef = useRef<HTMLDivElement>(null);
  const dropRef = useRef<HTMLDivElement>(null);
  const dragRun = useRef<DragRun | null>(null);

  const confirmReq = useConfirmRequest();
  useEffect(() => {
    if (confirmReq) {
      setConfirm(confirmReq);
      requestConfirm(null);
    }
  }, [confirmReq]);

  const startEdit = useCallback(() => setDraft(JSON.parse(JSON.stringify(saved)) as Widget[]), [saved]);
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

  /* ----- draft edits ----- */
  const update = useCallback((fn: (l: Widget[]) => Widget[]) => setDraft((d) => (d ? fn(d) : d)), []);
  const nameOf = useCallback((w: Widget) => widgetTitle(w, t), [t]);

  const moveWidget = (id: string, to: number, say = true) => {
    const cur = draftRef.current;
    if (!cur) return;
    const from = cur.findIndex((w) => w.id === id);
    if (from < 0 || to < 0 || to >= cur.length || from === to) return;
    update((l) => move(l, from, to));
    if (say) setAnnounce(t('edit.moved', { name: nameOf(cur[from]), pos: to + 1, total: cur.length }));
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

  useLayoutEffect(() => {
    if (!focusReq) return;
    const el = gridRef.current?.querySelector<HTMLElement>(`[data-wid="${focusReq.id}"]`);
    if (el) {
      el.focus({ preventScroll: true });
      el.scrollIntoView({ block: 'nearest', behavior: 'auto' });
    }
  }, [focusReq]);

  /* ----- pointer drag: widgets and library items ----- */
  const hit = (x: number, y: number): string | undefined => {
    for (const el of gridRef.current?.querySelectorAll<HTMLElement>('[data-wid]') ?? []) {
      const r = el.getBoundingClientRect();
      if (x >= r.left && x <= r.right && y >= r.top && y <= r.bottom) return el.dataset.wid;
    }
    return undefined;
  };
  const inside = (el: Element | null | undefined, x: number, y: number) => {
    if (!el) return false;
    const r = el.getBoundingClientRect();
    return x >= r.left && x <= r.right && y >= r.top && y <= r.bottom;
  };

  const endDrag = useRef<() => void>(() => undefined);
  const onMove = useRef<(e: PointerEvent) => void>(() => undefined);
  const onUp = useRef<(e: PointerEvent) => void>(() => undefined);

  onMove.current = (e) => {
    const d = dragRun.current;
    if (!d) return;
    d.px = e.clientX;
    d.py = e.clientY;
    if (!d.started) {
      if (Math.hypot(e.clientX - d.sx, e.clientY - d.sy) < 5) return;
      d.started = true;
      const r = d.el.getBoundingClientRect();
      const g = d.el.cloneNode(true) as HTMLElement;
      g.removeAttribute('data-wid');
      g.classList.add('ov-ghost');
      Object.assign(g.style, { position: 'fixed', left: `${r.left}px`, top: `${r.top}px`, width: `${r.width}px`, height: `${r.height}px`, margin: '0', zIndex: '1000', pointerEvents: 'none' });
      document.body.appendChild(g);
      d.ghost = g;
      document.body.classList.add('ov-dragging');
      setDragUi({ kind: d.kind, id: d.id });
      const scroller = d.el.closest('.app-content') as HTMLElement | null;
      const tick = () => {
        const cur = dragRun.current;
        if (!cur || !cur.started) return;
        const host = scroller ?? document.scrollingElement;
        if (host) {
          const rect = scroller ? scroller.getBoundingClientRect() : { top: 0, bottom: window.innerHeight };
          if (cur.py < rect.top + 70) host.scrollTop -= 14;
          else if (cur.py > rect.bottom - 70) host.scrollTop += 14;
        }
        cur.raf = requestAnimationFrame(tick);
      };
      d.raf = requestAnimationFrame(tick);
    }
    if (d.ghost) d.ghost.style.transform = `translate(${e.clientX - d.sx}px, ${e.clientY - d.sy}px)`;
    const over = hit(e.clientX, e.clientY);
    if (d.kind === 'widget') {
      if (d.id && over && over !== d.id) {
        const idx = draftRef.current?.findIndex((w) => w.id === over) ?? -1;
        moveWidget(d.id, idx, false);
      }
    } else {
      const overEnd = !over && (inside(dropRef.current, e.clientX, e.clientY) || inside(gridRef.current, e.clientX, e.clientY));
      if (over !== d.overId || overEnd !== d.overEnd) {
        d.overId = over;
        d.overEnd = overEnd;
        setDragUi({ kind: 'new', overId: over, overEnd });
      }
    }
  };

  endDrag.current = () => {
    const d = dragRun.current;
    window.removeEventListener('pointermove', pm);
    window.removeEventListener('pointerup', pu);
    window.removeEventListener('pointercancel', pc);
    window.removeEventListener('keydown', pk);
    if (d?.raf) cancelAnimationFrame(d.raf);
    d?.ghost?.remove();
    document.body.classList.remove('ov-dragging');
    dragRun.current = null;
    setDragUi(null);
  };
  onUp.current = (e) => {
    const d = dragRun.current;
    if (!d) return;
    const dropOver = d.overId;
    const dropEnd = d.overEnd;
    const ok = e.type === 'pointerup';
    endDrag.current();
    if (!ok) return;
    if (d.kind === 'new' && d.item) {
      if (!d.started) addItem(d.item); // a plain click adds at the end
      else if (dropOver) addItem(d.item, draftRef.current?.findIndex((w) => w.id === dropOver));
      else if (dropEnd) addItem(d.item);
    } else if (d.kind === 'widget' && d.started && d.id) {
      const cur = draftRef.current;
      const w = cur?.find((x) => x.id === d.id);
      if (cur && w) setAnnounce(t('edit.moved', { name: nameOf(w), pos: cur.indexOf(w) + 1, total: cur.length }));
    }
  };
  // stable listener identities, so they can be removed
  const pm = useCallback((e: PointerEvent) => onMove.current(e), []);
  const pu = useCallback((e: PointerEvent) => onUp.current(e), []);
  const pc = pu;
  const pk = useCallback((e: globalThis.KeyboardEvent) => {
    if (e.key === 'Escape') endDrag.current();
  }, []);
  useEffect(() => () => endDrag.current(), []);

  const beginDrag = (run: Omit<DragRun, 'started' | 'px' | 'py'>, e: RPointerEvent) => {
    if (dragRun.current) return;
    if (e.pointerType === 'mouse' && e.button !== 0) return;
    dragRun.current = { ...run, started: false, px: e.clientX, py: e.clientY };
    window.addEventListener('pointermove', pm);
    window.addEventListener('pointerup', pu);
    window.addEventListener('pointercancel', pc);
    window.addEventListener('keydown', pk);
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

  const onResizeDown = (w: Widget, e: RPointerEvent<HTMLElement>) => {
    e.preventDefault();
    e.stopPropagation();
    const grid = gridRef.current;
    const el = (e.currentTarget.closest('[data-wid]') as HTMLElement) ?? null;
    if (!grid || !el) return;
    const colW = (grid.getBoundingClientRect().width + GAP) / 12;
    const startW = el.getBoundingClientRect().width;
    const sx = e.clientX;
    const mv = (ev: PointerEvent) => resizeWidget(w.id, (startW + (ev.clientX - sx) + GAP) / colW);
    const up = () => {
      window.removeEventListener('pointermove', mv);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('pointercancel', up);
      document.body.classList.remove('ov-dragging');
      const cur = draftRef.current?.find((x) => x.id === w.id);
      if (cur) setAnnounce(t('edit.resized', { name: nameOf(cur), cols: cur.cols }));
    };
    document.body.classList.add('ov-dragging');
    window.addEventListener('pointermove', mv);
    window.addEventListener('pointerup', up);
    window.addEventListener('pointercancel', up);
  };

  const onWidgetKey = (w: Widget, i: number, e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget) return;
    const len = draftRef.current?.length ?? 0;
    if (e.shiftKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
      e.preventDefault();
      resizeWidget(w.id, stepCols(w.cols, e.key === 'ArrowRight' ? 1 : -1), true);
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault();
      moveWidget(w.id, i - 1);
      setFocusReq({ id: w.id, n: Date.now() });
    } else if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault();
      moveWidget(w.id, Math.min(len - 1, i + 1));
      setFocusReq({ id: w.id, n: Date.now() });
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      setSettingsId(w.id);
    } else if (e.key === 'Delete' || e.key === 'Backspace') {
      e.preventDefault();
      removeWidget(w.id);
    }
  };

  /* ----- actions ----- */
  const run = useCallback(
    (a: DashAction) => {
      if (a.confirm) setConfirm(a);
      else void runAction(a, t);
    },
    [t],
  );
  const ctx: WidgetCtx = useMemo(() => ({ editing, run, configure: (id) => setSettingsId(id) }), [editing, run]);

  const settingsWidget = list.find((w) => w.id === settingsId) ?? null;
  const pluginTitle = settingsWidget?.type === 'plugin' ? pluginWidgets.find((p) => p.plugin === settingsWidget.settings?.plugin && p.id === settingsWidget.settings?.widget)?.title : undefined;

  /* ----- greeting ----- */
  const name = session?.name?.trim().split(/\s+/)[0] || session?.user || '';
  const hostName = host?.hostname || 'this machine';
  const issues = (alerts ?? []).filter((a) => a.severity !== 'ok').length;
  const summary = alerts === null ? t('summary.checking', { host: hostName }) : issues === 0 ? t('summary.ok', { host: hostName }) : t('summary.issues', { host: hostName, count: issues });

  const library = <Library onAdd={(i) => addItem(i)} onPointerDown={onLibPointerDown} onReset={resetLayout} className={wide ? '' : 'ov-lib--sheet'} />;

  return (
    <Page
      hue="ov"
      title={editing ? undefined : t('greeting', { name })}
      subtitle={editing ? undefined : summary}
      actions={editing ? undefined : (
        <>
          <span className="ov-refresh">
            <RefreshSelect label={tc('refreshRate.label')} onPick={(ms) => void setRefresh(ms)} />
            <Button iconOnly icon="refresh" variant="ghost" aria-label={tc('refreshRate.now')} title={tc('refreshRate.now')} onClick={refreshNow} />
          </span>
          <Button icon="edit" onClick={startEdit}>{t('personalize')}</Button>
        </>
      )}
    >
      <div className={`ov${editing ? ' ov--editing' : ''}${editing && wide ? ' ov--lib' : ''}`}>
        <div className="ov-main">
          {editing && (
            <div className="ov-editbar" role="region" aria-label={t('edit.title')}>
              <Icon name="edit" />
              <b>{t('edit.title')}</b>
              <span>{t('edit.hint')}</span>
              <div className="ov-editbar-r">
                {!wide && <Button size="sm" icon="plus" variant="ghost" onClick={() => setLibOpen(true)}>{t('edit.addWidget')}</Button>}
                <Button size="sm" variant="ghost" onClick={cancel}>{t('cancel')}</Button>
                <Button size="sm" className="ov-save" onClick={() => void save()}>{t('edit.save')}</Button>
              </div>
            </div>
          )}
          <div className="ov-canvas">
          <div ref={gridRef} className="ov-grid" role="list">
            {list.map((w, i) => (
              <div
                key={w.id}
                role="listitem"
                data-wid={w.id}
                className={`ov-w ov-c${w.cols}${dragUi?.id === w.id ? ' ov-w--drag' : ''}${dragUi?.overId === w.id && dragUi.kind === 'new' ? ' ov-w--target' : ''}`}
                tabIndex={editing ? 0 : undefined}
                aria-roledescription={editing ? t('edit.roleDesc') : undefined}
                aria-label={editing ? t('edit.widgetLabel', { name: nameOf(w), pos: i + 1, total: list.length }) : undefined}
                onPointerDown={editing ? (e) => onWidgetPointerDown(w, e) : undefined}
                onKeyDown={editing ? (e) => onWidgetKey(w, i, e) : undefined}
              >
                {editing && (
                  <>
                    <div className="ov-tools">
                      <button type="button" className="ov-grip" data-grip aria-label={t('edit.drag')} title={t('edit.drag')}>
                        <Icon name="grip" />
                      </button>
                      <button type="button" data-nodrag aria-label={t('edit.settings', { name: nameOf(w) })} title={t('edit.settingsShort')} onClick={() => setSettingsId(w.id)}>
                        <Icon name="edit" />
                      </button>
                      <button type="button" data-nodrag aria-label={t('edit.remove', { name: nameOf(w) })} title={t('edit.removeShort')} onClick={() => removeWidget(w.id)}>
                        <Icon name="plus" style={{ transform: 'rotate(45deg)' }} />
                      </button>
                    </div>
                    <span className="ov-res" data-nodrag onPointerDown={(e) => onResizeDown(w, e)} aria-hidden="true" />
                  </>
                )}
                <div className="ov-w-body" {...(editing ? ({ inert: '' } as object) : {})}>
                  <WidgetBody w={w} ctx={ctx} />
                </div>
              </div>
            ))}
            {editing && (
              <div ref={dropRef} className={`ov-drop${dragUi?.overEnd ? ' ov-drop--on' : ''}`}>
                <div><Icon name="plus" />{t('edit.drop')}</div>
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

      <div className="ov-sr" role="status" aria-live="polite">{announce}</div>

      <SettingsDialog
        widget={settingsWidget}
        pluginTitle={pluginTitle}
        onClose={() => setSettingsId(null)}
        onSave={(s) => {
          const id = settingsId;
          setSettingsId(null);
          if (!id) return;
          if (editing) update((l) => l.map((w) => (w.id === id ? { ...w, settings: s } : w)));
          else void set('dashboard', { version: 1, widgets: saved.map((w) => (w.id === id ? { ...w, settings: s } : w)) });
        }}
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
        description={confirm ? `${confirm.argv.join(' ')}${confirm.admin ? ` (${t('action.asAdmin')})` : ''}` : undefined}
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
