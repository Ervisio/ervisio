import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent as RPointerEvent,
  type RefObject,
} from 'react';
import { useSearchParams } from 'react-router-dom';
import type { usePrefs } from '../../api';
import type { TFn } from '../../i18n';
import { toast } from '../../ui';
import { requestConfirm, runAction, useConfirmRequest } from './actions';
import type { LibItem } from './Library';
import { defaultLayout, snapCols, stepCols, uid, type DashAction, type Widget } from './model';
import { widgetTitle } from './widgets';

const GAP = 14;
const GRID_COLUMNS = 12;

type SetPref = ReturnType<typeof usePrefs>['set'];
type UpdateDraft = (fn: (l: Widget[]) => Widget[]) => void;

function move<T>(list: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || to >= list.length) return list;
  const next = list.slice();
  const [x] = next.splice(from, 1);
  next.splice(to, 0, x);
  return next;
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

// Dashboard actions, with a confirm step for the ones that ask for it.
export function useActionConfirm(t: TFn) {
  const [confirm, setConfirm] = useState<DashAction | null>(null);
  const confirmReq = useConfirmRequest();
  useEffect(() => {
    if (confirmReq) {
      setConfirm(confirmReq);
      requestConfirm(null);
    }
  }, [confirmReq]);

  const run = useCallback(
    (a: DashAction) => {
      if (a.confirm) setConfirm(a);
      else void runAction(a, t);
    },
    [t],
  );
  return { confirm, closeConfirm: () => setConfirm(null), run };
}

// Edit-mode working copy of the layout; `?personalize` opens it.
export function useDashboardDraft(saved: Widget[], setPref: SetPref, t: TFn) {
  const [params, setParams] = useSearchParams();
  const [draft, setDraft] = useState<Widget[] | null>(null);
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const [isDiscardOpen, setDiscardOpen] = useState(false);
  const editing = draft !== null;

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
  const cancel = () => (dirty ? setDiscardOpen(true) : setDraft(null));
  const save = async () => {
    if (!draft) return;
    try {
      await setPref('dashboard', { version: 1, widgets: draft });
      toast.ok(t('saved'));
      setDraft(null);
    } catch (e) {
      toast.err(t('saveFailed'), e instanceof Error ? e.message : undefined);
    }
  };
  const update: UpdateDraft = useCallback((fn) => setDraft((d) => (d ? fn(d) : d)), []);

  return {
    draftRef,
    editing,
    list: draft ?? saved,
    startEdit,
    cancel,
    save,
    update,
    discard: () => setDraft(null),
    reset: () => setDraft(defaultLayout().widgets),
    isDiscardOpen,
    closeDiscard: () => setDiscardOpen(false),
  };
}

// Settings dialog target; outside edit mode a save persists straight away.
export function useWidgetSettings(
  list: Widget[],
  saved: Widget[],
  editing: boolean,
  update: UpdateDraft,
  setPref: SetPref,
) {
  const [settingsId, setSettingsId] = useState<string | null>(null);
  const settingsWidget = list.find((w) => w.id === settingsId) ?? null;
  const saveSettings = (s: Widget['settings']) => {
    const id = settingsId;
    setSettingsId(null);
    if (!id) return;
    if (editing) update((l) => l.map((w) => (w.id === id ? { ...w, settings: s } : w)));
    else
      void setPref('dashboard', {
        version: 1,
        widgets: saved.map((w) => (w.id === id ? { ...w, settings: s } : w)),
      });
  };
  return { settingsWidget, openSettings: setSettingsId, saveSettings };
}

interface WidgetEditsOptions {
  t: TFn;
  draftRef: RefObject<Widget[] | null>;
  update: UpdateDraft;
  reset: () => void;
  openSettings: (id: string | null) => void;
  closeLibrary: () => void;
}

// Draft edits (move, resize, add, remove) with screen reader announcements and focus follow-up.
export function useWidgetEdits({
  t,
  draftRef,
  update,
  reset,
  openSettings,
  closeLibrary,
}: WidgetEditsOptions) {
  const gridRef = useRef<HTMLDivElement>(null);
  const [announce, setAnnounce] = useState('');
  const [focusReq, setFocusReq] = useState<{ id: string; n: number } | null>(null);
  const focusWidget = (id: string) => setFocusReq({ id, n: Date.now() });
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
    if (next) focusWidget(next.id);
  };
  const addItem = (item: LibItem, at?: number) => {
    const w: Widget = { id: uid(), type: item.type, cols: item.cols, settings: item.settings?.() };
    update((l) => {
      const n = l.slice();
      n.splice(at === undefined || at < 0 ? n.length : at, 0, w);
      return n;
    });
    setAnnounce(t('edit.added', { name: item.title }));
    focusWidget(w.id);
    if (item.configure) openSettings(w.id);
    closeLibrary();
  };
  const resetLayout = () => {
    reset();
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
        focusWidget(w.id);
        break;
      case 'next':
        moveWidget(w.id, Math.min(len - 1, i + 1));
        focusWidget(w.id);
        break;
      case 'settings':
        openSettings(w.id);
        break;
      case 'remove':
        removeWidget(w.id);
        break;
    }
  };

  return {
    gridRef,
    announce,
    nameOf,
    moveWidget,
    removeWidget,
    addItem,
    resetLayout,
    announceMoved,
    onResizeDown,
    onWidgetKey,
  };
}
