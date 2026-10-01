import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, downloadUrl, usePrefs, useSession } from '../../api';
import { useT } from '../../i18n';
import { Button, ConfirmDialog, Dialog, Icon, IconButton, Menu, Sheet, toast, type MenuItem } from '../../ui';
import { usePaletteActions } from '../index';
import { ChownDialog, EditorDialog, NameDialog } from './Dialogs';
import { Pane, type Shortcut } from './Pane';
import { PlacesList } from './Places';
import { SidePanel } from './SidePanel';
import { TransferBar } from './TransferBar';
import { usePaneData } from './data';
import { collectDropped } from './dropfiles';
import { emitChanged, fcall, onFilesChanged, setUnlockProbe } from './fapi';
import { canInlineImage, kindOf, looksTextual } from './kinds';
import { enqueueCopy, enqueueUpload, ensureDir } from './transfers';
import { isDirLike, type FEntry, type PaneState, type Places, type Remote, type SortState, type TabState, type View } from './types';
import { basename, dirname, isVirtual, nextId } from './util';
import './files.css';

type Side = 'L' | 'R';
const SORT0: SortState = { key: 'name', dir: 'asc' };
const none = new Set<string>();

const newPane = (loc: string, view: View = 'grid'): PaneState => ({ loc, view, admin: false });
const newTab = (home: string, loc = home): TabState => ({ id: nextId(), left: newPane(loc), right: newPane(home), split: false });

function sanitizeTabs(raw: unknown, home: string): { tabs: TabState[]; active: string } | null {
  const arr = Array.isArray(raw) ? raw : raw && typeof raw === 'object' ? (raw as any).tabs : null;
  if (!Array.isArray(arr) || arr.length === 0) return null;
  const okLoc = (l: unknown) => (typeof l === 'string' && (l.startsWith('/') || ['trash:', 'recent:', 'starred:'].includes(l)) ? l : home);
  const okView = (v: unknown): View => (v === 'list' ? 'list' : 'grid');
  const tabs: TabState[] = arr.slice(0, 12).map((x: any) => ({
    id: typeof x?.id === 'string' ? x.id : nextId(),
    left: { loc: okLoc(x?.left?.loc), view: okView(x?.left?.view), admin: false },
    right: { loc: okLoc(x?.right?.loc), view: okView(x?.right?.view), admin: false },
    split: !!x?.split,
  }));
  const active = !Array.isArray(raw) && typeof (raw as any)?.active === 'string' && tabs.some((x) => x.id === (raw as any).active) ? (raw as any).active : tabs[0].id;
  return { tabs, active };
}

interface Clip {
  paths: string[];
  cut: boolean;
}

export default function FilesPage() {
  const t = useT('files');
  const { session, isUnlocked } = useSession();
  const { prefs, ready, set } = usePrefs();
  const home = session?.home || '/';
  const isRoot = !!session?.isRoot;

  const [tabs, setTabs] = useState<TabState[]>(() => [newTab(home)]);
  const [activeId, setActiveId] = useState(() => tabs[0].id);
  const [act, setAct] = useState<Side>('L');
  const restored = useRef(false);
  const [sel, setSel] = useState<Record<Side, ReadonlySet<string>>>({ L: none, R: none });
  const [query, setQuery] = useState<Record<Side, string>>({ L: '', R: '' });
  const [sort, setSort] = useState<Record<Side, SortState>>({ L: SORT0, R: SORT0 });
  const [places, setPlaces] = useState<Places | null>(null);
  const [clip, setClip] = useState<Clip | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null);
  const [dialog, setDialog] = useState<null | { kind: 'name'; mode: 'folder' | 'file' | 'rename'; entry?: FEntry; side: Side } | { kind: 'edit'; entry: FEntry; admin: boolean } | { kind: 'chown'; entry: FEntry } | { kind: 'image'; entry: FEntry; admin: boolean }>(null);
  const [del, setDel] = useState<null | { entries: FEntry[]; side: Side; trash: boolean }>(null);
  const [emptyTrash, setEmptyTrash] = useState(false);
  const [placesSheet, setPlacesSheet] = useState(false);
  const [dismissed, setDismissed] = useState('');
  const [forcePanel, setForcePanel] = useState(false);
  const [mobile, setMobile] = useState(() => typeof matchMedia === 'function' && matchMedia('(max-width: 859px)').matches);
  const fileInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (typeof matchMedia !== 'function') return;
    const mq = matchMedia('(max-width: 859px)');
    const on = () => setMobile(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);

  /* ---------- preferences ---------- */
  const starredList: string[] = Array.isArray(prefs['files.starred']) ? prefs['files.starred'] : [];
  const bookmarks: string[] = Array.isArray(prefs['files.bookmarks']) ? prefs['files.bookmarks'].filter((x: unknown) => typeof x === 'string') : [];
  const remotes: Remote[] = Array.isArray(prefs['files.remotes']) ? prefs['files.remotes'].filter((r: any) => r && typeof r.name === 'string') : [];
  const showHidden = !!prefs['files.showHidden'];
  const starred = useMemo(() => new Set(starredList), [starredList.join('\n')]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!ready || restored.current) return;
    restored.current = true;
    const r = sanitizeTabs(prefs['files.tabs'], home);
    if (r) {
      setTabs(r.tabs);
      setActiveId(r.active);
    }
  }, [ready]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!restored.current) return;
    const h = window.setTimeout(() => {
      void set('files.tabs', { active: activeId, tabs: tabs.map((x) => ({ id: x.id, split: x.split, left: { loc: x.left.loc, view: x.left.view }, right: { loc: x.right.loc, view: x.right.view } })) }).catch(() => undefined);
    }, 500);
    return () => window.clearTimeout(h);
  }, [tabs, activeId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    setUnlockProbe(() => isUnlocked || isRoot);
  }, [isUnlocked, isRoot]);

  const tab = tabs.find((x) => x.id === activeId) ?? tabs[0];
  const split = tab.split && !mobile;
  const side: Side = split ? act : 'L';
  const pane = (s: Side) => (s === 'L' ? tab.left : tab.right);
  const updPane = (s: Side, patch: Partial<PaneState>) => setTabs((ts) => ts.map((x) => (x.id === tab.id ? { ...x, [s === 'L' ? 'left' : 'right']: { ...(s === 'L' ? x.left : x.right), ...patch } } : x)));
  const clearSel = (s?: Side) => setSel((c) => (s ? { ...c, [s]: none } : { L: none, R: none }));

  /* ---------- places ---------- */
  const loadPlaces = useCallback(() => {
    fcall<Places>('files.places', {}).then(setPlaces, () => undefined);
  }, []);
  useEffect(() => {
    loadPlaces();
    const iv = window.setInterval(loadPlaces, 60_000);
    const off = onFilesChanged(() => loadPlaces());
    return () => {
      window.clearInterval(iv);
      off();
    };
  }, [loadPlaces]);

  /* ---------- data ---------- */
  const onElevateL = useCallback(() => updPaneRef.current('L', { admin: true }), []);
  const onElevateR = useCallback(() => updPaneRef.current('R', { admin: true }), []);
  const updPaneRef = useRef(updPane);
  updPaneRef.current = updPane;
  const dL = usePaneData({ loc: tab.left.loc, admin: tab.left.admin, showHidden, starred: starredList, query: query.L, onElevate: onElevateL, enabled: true });
  const dR = usePaneData({ loc: tab.right.loc, admin: tab.right.admin, showHidden, starred: starredList, query: query.R, onElevate: onElevateR, enabled: split });
  const data = (s: Side) => (s === 'L' ? dL : dR);

  const selectedEntries = (s: Side): FEntry[] => data(s).entries.filter((e) => sel[s].has(e.path));
  const targets = selectedEntries(side);

  /* ---------- navigation ---------- */
  const navigate = (s: Side, loc: string) => {
    const l = loc === 'home:' ? home : loc;
    updPane(s, { loc: l });
    setQuery((q) => ({ ...q, [s]: '' }));
    clearSel(s);
  };
  const goPlace = (loc: string) => navigate(side, loc);
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

  /* ---------- helpers ---------- */
  const fail = (title: string, x: unknown) => {
    if (x instanceof ApiError && x.code === 'cancelled') return;
    toast.err(title, x instanceof ApiError ? x.message : x instanceof Error ? x.message : String(x));
  };
  const adminOf = (s: Side) => pane(s).admin;
  const otherSide: Side = side === 'L' ? 'R' : 'L';
  const writableLoc = (s: Side) => {
    const l = pane(s).loc;
    return isVirtual(l) ? null : l;
  };
  const refreshAll = () => emitChanged();

  const copyText = async (txt: string) => {
    try {
      await navigator.clipboard.writeText(txt);
      toast.ok(t('pathCopied'));
    } catch {
      toast.err(t('err.clipboard'));
    }
  };

  /* ---------- operations ---------- */
  const download = (es: FEntry[], s: Side = side) => {
    const files = es.filter((e) => !isDirLike(e) && e.type !== 'other');
    if (files.length === 0) return toast.info(t('err.noFolderDownload'));
    files.forEach((e, i) =>
      window.setTimeout(() => {
        const a = document.createElement('a');
        a.href = downloadUrl(e.path, adminOf(s));
        a.download = e.name;
        document.body.appendChild(a);
        a.click();
        a.remove();
      }, i * 350),
    );
    if (es.length !== files.length) toast.info(t('err.foldersSkipped'));
  };

  const undoTrash = async (paths: string[]) => {
    try {
      const r = await fcall<{ items: { id: string; originalPath: string; deletedAt: number }[] }>('files.trashList', {});
      const ids: string[] = [];
      for (const p of paths) {
        const m = r.items.filter((i) => i.originalPath === p).sort((a, b) => b.deletedAt - a.deletedAt)[0];
        if (m) ids.push(m.id);
      }
      if (ids.length) await fcall('files.restore', { ids });
      refreshAll();
    } catch (x) {
      fail(t('err.restore'), x);
    }
  };

  const doTrash = async (es: FEntry[], s: Side) => {
    const paths = es.map((e) => e.path);
    try {
      const r = await fcall<{ deleted: string[]; failed: { path: string; message: string }[] }>('files.delete', { paths, trash: true }, { admin: adminOf(s) });
      clearSel(s);
      emitChanged();
      if ((r.deleted ?? []).length) toast.undo(t('trashed', { count: (r.deleted ?? []).length }), t('undo'), () => void undoTrash(r.deleted));
      if ((r.failed ?? []).length) toast.err(t('err.someNotDeleted', { count: (r.failed ?? []).length }), r.failed![0].message);
    } catch (x) {
      fail(t('err.trash'), x);
    }
  };

  const doPermanent = async (es: FEntry[], s: Side, fromTrash: boolean) => {
    try {
      if (fromTrash) await fcall('files.trashDelete', { ids: es.map((e) => e.trashId).filter(Boolean) }, { admin: adminOf(s) });
      else await fcall('files.delete', { paths: es.map((e) => e.path), trash: false }, { admin: adminOf(s) });
      clearSel(s);
      emitChanged();
      toast.ok(t('deleted', { count: es.length }));
    } catch (x) {
      fail(t('err.delete'), x);
      throw x;
    }
  };

  const requestDelete = (es: FEntry[], s: Side, permanent = false) => {
    if (es.length === 0) return;
    const inTrash = pane(s).loc === 'trash:';
    if (inTrash || permanent || pane(s).admin || isRoot) setDel({ entries: es, side: s, trash: inTrash });
    else void doTrash(es, s);
  };

  const restore = async (es: FEntry[], s: Side) => {
    try {
      const r = await fcall<{ restored: string[]; failed: { path: string; message: string }[] }>('files.restore', { ids: es.map((e) => e.trashId).filter(Boolean) }, { admin: adminOf(s) });
      clearSel(s);
      emitChanged();
      if ((r.restored ?? []).length) toast.ok(t('restored', { count: (r.restored ?? []).length }));
      if ((r.failed ?? []).length) toast.err(t('err.someNotRestored', { count: (r.failed ?? []).length }), r.failed![0].message);
    } catch (x) {
      fail(t('err.restore'), x);
    }
  };

  const doEmptyTrash = async () => {
    try {
      await fcall('files.trashEmpty', {}, { admin: adminOf(side) });
      emitChanged();
      toast.ok(t('trashEmptied'));
    } catch (x) {
      fail(t('err.emptyTrash'), x);
      throw x;
    }
  };

  const submitName = async (mode: 'folder' | 'file' | 'rename', s: Side, entry: FEntry | undefined, name: string) => {
    const dir = mode === 'rename' ? dirname(entry!.path) : writableLoc(s);
    if (!dir) return;
    const target = dir === '/' ? '/' + name : dir + '/' + name;
    const admin = adminOf(s);
    if (mode === 'folder') await fcall('files.mkdir', { path: target }, { admin });
    else if (mode === 'file') await fcall('files.create', { path: target }, { admin });
    else {
      if (target === entry!.path) return;
      await fcall('files.rename', { from: entry!.path, to: target }, { admin });
    }
    emitChanged(dir);
    if (mode !== 'rename') toast.ok(mode === 'folder' ? t('folderCreated', { name }) : t('fileCreated', { name }));
  };

  const paste = (s: Side) => {
    const dest = writableLoc(s);
    if (!clip || !dest) return;
    enqueueCopy({ from: clip.paths, to: dest, move: clip.cut, admin: adminOf(s), onElevate: () => updPane(s, { admin: true }) });
    if (clip.cut) setClip(null);
  };
  const copyTo = (es: FEntry[], from: Side, to: Side, move: boolean) => {
    const dest = writableLoc(to);
    if (!dest) return toast.info(t('err.noTargetFolder'));
    enqueueCopy({ from: es.map((e) => e.path), to: dest, move, admin: adminOf(from) || adminOf(to), onElevate: () => updPane(to, { admin: true }) });
    clearSel(from);
  };
  const dropMove = (paths: string[], dest: string, copy: boolean, s: Side) => {
    const same = paths.every((p) => dirname(p) === dest);
    if (same && !copy) return;
    enqueueCopy({ from: paths, to: dest, move: !copy, admin: adminOf(s), onElevate: () => updPane(s, { admin: true }) });
  };

  const uploadFiles = async (items: { file: File; rel: string }[], dest: string, s: Side) => {
    const admin = adminOf(s);
    const made = new Set<string>();
    try {
      for (const it of items) {
        const sub = it.rel.includes('/') ? it.rel.slice(0, it.rel.lastIndexOf('/')) : '';
        if (sub && !made.has(sub)) {
          made.add(sub);
          await ensureDir(dest === '/' ? '/' + sub : dest + '/' + sub, admin, () => updPane(s, { admin: true }));
        }
        enqueueUpload({ file: it.file, dest, rel: it.rel, admin, onElevate: () => updPane(s, { admin: true }) });
      }
    } catch (x) {
      fail(t('err.upload'), x);
    }
  };
  const onDropFiles = async (dt: DataTransfer, dest: string, s: Side) => {
    try {
      const items = await collectDropped(dt);
      if (items.length === 0) return toast.info(t('err.nothingDropped'));
      await uploadFiles(items, dest, s);
    } catch (x) {
      fail(t('err.upload'), x);
    }
  };

  const toggleStar = async (e: FEntry) => {
    const next = starred.has(e.path) ? starredList.filter((p) => p !== e.path) : [...starredList, e.path];
    await set('files.starred', next).catch((x) => fail(t('err.prefs'), x));
    toast.ok(starred.has(e.path) ? t('unstarred', { name: e.name }) : t('starredDone', { name: e.name }));
  };
  const addBookmark = async (path: string) => {
    if (bookmarks.includes(path)) return toast.info(t('bookmarkExists'));
    await set('files.bookmarks', [...bookmarks, path]).catch((x) => fail(t('err.prefs'), x));
    toast.ok(t('bookmarked', { name: basename(path) || '/' }));
  };
  const removeBookmark = (path: string) => void set('files.bookmarks', bookmarks.filter((b) => b !== path)).catch((x) => fail(t('err.prefs'), x));

  const openEntry = (e: FEntry, s: Side) => {
    if (pane(s).loc === 'trash:') return;
    if (isDirLike(e)) return navigate(s, e.path);
    const admin = adminOf(s);
    if (kindOf(e) === 'img' && canInlineImage(e.name)) return setDialog({ kind: 'image', entry: e, admin });
    if (looksTextual(e) && e.size <= 2 << 20) return setDialog({ kind: 'edit', entry: e, admin });
    download([e], s);
  };

  /* ---------- keyboard ---------- */
  const shortcut = (s: Side, k: Shortcut) => {
    const es = selectedEntries(s);
    const loc = pane(s).loc;
    switch (k) {
      case 'delete':
        return requestDelete(es, s);
      case 'permDelete':
        return requestDelete(es, s, true);
      case 'rename':
        if (es.length === 1 && loc !== 'trash:') setDialog({ kind: 'name', mode: 'rename', entry: es[0], side: s });
        return;
      case 'copy':
      case 'cut':
        if (es.length && loc !== 'trash:') {
          setClip({ paths: es.map((e) => e.path), cut: k === 'cut' });
          toast.info(k === 'cut' ? t('clip.cut', { count: es.length }) : t('clip.copied', { count: es.length }));
        }
        return;
      case 'paste':
        return paste(s);
      case 'open':
        return es.length === 1 ? openEntry(es[0], s) : undefined;
      case 'up':
        if (!isVirtual(loc) && loc !== '/') navigate(s, dirname(loc));
        return;
      case 'newFolder':
        if (writableLoc(s)) setDialog({ kind: 'name', mode: 'folder', side: s });
        return;
      case 'refresh':
        return refreshAll();
      case 'details':
        return setForcePanel(true);
    }
  };

  /* ---------- menus ---------- */
  const newItems = (s: Side): MenuItem[] => {
    const can = !!writableLoc(s);
    return [
      { id: 'nf', label: t('new.folder'), icon: 'files', kbd: 'Ctrl+Shift+N', disabled: !can, onSelect: () => setDialog({ kind: 'name', mode: 'folder', side: s }) },
      { id: 'nt', label: t('new.file'), icon: 'file', disabled: !can, onSelect: () => setDialog({ kind: 'name', mode: 'file', side: s }) },
      { id: 'up', label: t('new.upload'), icon: 'upload', disabled: !can, onSelect: () => fileInput.current?.click() },
    ];
  };

  const openMenu = (s: Side, e: FEntry | null, x: number, y: number) => {
    const loc = pane(s).loc;
    const es = e ? (sel[s].has(e.path) ? selectedEntries(s) : [e]) : [];
    const one = es.length === 1 ? es[0] : null;
    const items: MenuItem[] = [];
    if (loc === 'trash:') {
      if (e) {
        items.push({ id: 'restore', label: t('act.restore'), icon: 'undo', onSelect: () => void restore(es, s) });
        items.push({ id: 'det', label: t('act.details'), icon: 'info', onSelect: () => setForcePanel(true) });
        items.push({ id: 'del', label: t('act.deleteForever'), icon: 'trash', danger: true, kbd: 'Del', onSelect: () => requestDelete(es, s) });
      } else items.push({ id: 'empty', label: t('act.emptyTrash'), icon: 'trash', danger: true, disabled: data(s).entries.length === 0, onSelect: () => setEmptyTrash(true) });
    } else if (e && one) {
      const dirLike = isDirLike(one);
      items.push({ id: 'open', label: dirLike ? t('act.open') : looksTextual(one) ? t('act.edit') : t('act.openFile'), icon: dirLike ? 'files' : 'eye', kbd: 'Enter', onSelect: () => openEntry(one, s) });
      if (dirLike && split) items.push({ id: 'openo', label: t('act.openOther'), icon: 'columns', onSelect: () => navigate(s === 'L' ? 'R' : 'L', one.path) });
      if (dirLike) items.push({ id: 'opent', label: t('act.openTab'), icon: 'plus', onSelect: () => openTab(one.path) });
      if (isVirtual(loc) || query[s]) items.push({ id: 'show', label: t('act.showInFolder'), icon: 'search', onSelect: () => navigate(s, dirname(one.path)) });
      if (!dirLike) items.push({ id: 'dl', label: t('act.download'), icon: 'download', onSelect: () => download([one], s) });
      items.push({ id: 'ren', label: t('act.rename'), icon: 'edit', kbd: 'F2', onSelect: () => shortcut(s, 'rename') });
      items.push({ id: 'cp', label: t('act.copy'), icon: 'copy', kbd: 'Ctrl+C', onSelect: () => shortcut(s, 'copy') });
      items.push({ id: 'ct', label: t('act.cut'), kbd: 'Ctrl+X', onSelect: () => shortcut(s, 'cut') });
      if (dirLike && clip) items.push({ id: 'pi', label: t('act.pasteInto'), onSelect: () => enqueueCopy({ from: clip.paths, to: one.path, move: clip.cut, admin: adminOf(s), onElevate: () => updPane(s, { admin: true }) }) });
      if (split) {
        const o: Side = s === 'L' ? 'R' : 'L';
        const dest = writableLoc(o);
        items.push({ id: 'c2', label: t(s === 'L' ? 'act.copyRight' : 'act.copyLeft'), disabled: !dest, onSelect: () => copyTo(es, s, o, false) });
        items.push({ id: 'm2', label: t(s === 'L' ? 'act.moveRight' : 'act.moveLeft'), disabled: !dest, onSelect: () => copyTo(es, s, o, true) });
      }
      items.push({ id: 'star', label: starred.has(one.path) ? t('act.unstar') : t('act.star'), icon: 'star', onSelect: () => void toggleStar(one) });
      if (dirLike) items.push({ id: 'bm', label: t('act.bookmark'), icon: 'files', onSelect: () => void addBookmark(one.path) });
      items.push({ id: 'path', label: t('act.copyPath'), icon: 'link', onSelect: () => void copyText(one.path) });
      items.push({ id: 'det', label: t('act.details'), icon: 'info', onSelect: () => setForcePanel(true) });
      items.push({ id: 'del', label: t('act.trash'), icon: 'trash', danger: true, kbd: 'Del', onSelect: () => requestDelete(es, s) });
    } else if (e) {
      items.push({ id: 'cp', label: t('act.copy'), icon: 'copy', kbd: 'Ctrl+C', onSelect: () => shortcut(s, 'copy') });
      items.push({ id: 'ct', label: t('act.cut'), kbd: 'Ctrl+X', onSelect: () => shortcut(s, 'cut') });
      if (es.every((x) => !isDirLike(x))) items.push({ id: 'dl', label: t('act.download'), icon: 'download', onSelect: () => download(es, s) });
      if (split) {
        const o: Side = s === 'L' ? 'R' : 'L';
        items.push({ id: 'c2', label: t(s === 'L' ? 'act.copyRight' : 'act.copyLeft'), disabled: !writableLoc(o), onSelect: () => copyTo(es, s, o, false) });
        items.push({ id: 'm2', label: t(s === 'L' ? 'act.moveRight' : 'act.moveLeft'), disabled: !writableLoc(o), onSelect: () => copyTo(es, s, o, true) });
      }
      items.push({ id: 'del', label: t('act.trashMany', { count: es.length }), icon: 'trash', danger: true, kbd: 'Del', onSelect: () => requestDelete(es, s) });
    } else {
      const can = !!writableLoc(s);
      items.push(...newItems(s));
      items.push({ id: 'paste', label: t('act.paste'), kbd: 'Ctrl+V', disabled: !clip || !can, onSelect: () => paste(s) });
      items.push({ id: 'all', label: t('act.selectAll'), kbd: 'Ctrl+A', onSelect: () => setSel((c) => ({ ...c, [s]: new Set(data(s).entries.map((x) => x.path)) })) });
      items.push({ id: 'hid', label: showHidden ? t('hideHidden') : t('showHidden'), icon: showHidden ? 'eyeoff' : 'eye', onSelect: () => void set('files.showHidden', !showHidden) });
      if (can && loc !== '/') items.push({ id: 'bm', label: t('act.bookmarkHere'), icon: 'files', onSelect: () => void addBookmark(loc) });
      items.push({ id: 'ref', label: t('act.refresh'), icon: 'refresh', kbd: 'F5', onSelect: refreshAll });
    }
    setMenu({ x, y, items });
  };

  /* ---------- palette ---------- */
  usePaletteActions('files', [
    { id: 'files.home', title: t('palette.home'), hint: t('title'), icon: 'home', hue: 'file', run: () => goPlace(home) },
    { id: 'files.trash', title: t('palette.trash'), hint: t('title'), icon: 'trash', hue: 'file', run: () => goPlace('trash:') },
    { id: 'files.newfolder', title: t('palette.newFolder'), hint: t('title'), icon: 'files', hue: 'file', run: () => shortcut(side, 'newFolder') },
    { id: 'files.split', title: t('palette.split'), hint: t('title'), icon: 'columns', hue: 'file', run: () => setTabs((ts) => ts.map((x) => (x.id === tab.id ? { ...x, split: !x.split } : x))) },
  ]);

  /* ---------- side panel ---------- */
  const panelEntries = targets;
  const selKey = panelEntries.map((e) => e.path).join('\n');
  useEffect(() => {
    if (panelEntries.length === 0) setForcePanel(false);
  }, [panelEntries.length]);
  const panelOpen = panelEntries.length > 0 && (mobile ? forcePanel : split ? forcePanel : dismissed !== selKey);

  /* ---------- render ---------- */
  const inTrash = pane(side).loc === 'trash:';
  const otherDest = split ? writableLoc(otherSide) : null;
  const onSelected = (s: Side) => (v: Set<string>) => {
    setSel((c) => ({ ...c, [s]: v }));
    if (v.size === 0) setForcePanel(false);
  };

  const renderPane = (s: Side) => (
    <Pane
      key={s}
      label={split ? t(s === 'L' ? 'left' : 'right') : undefined}
      active={side === s}
      state={pane(s)}
      data={data(s)}
      query={query[s]}
      onQuery={(q) => {
        setQuery((c) => ({ ...c, [s]: q }));
        clearSel(s);
      }}
      sort={sort[s]}
      onSort={(v) => setSort((c) => ({ ...c, [s]: v }))}
      selected={sel[s]}
      onSelected={onSelected(s)}
      starred={starred}
      onFocus={() => setAct(s)}
      onNavigate={(l) => navigate(s, l)}
      onView={(v) => updPane(s, { view: v })}
      onOpen={(e) => openEntry(e, s)}
      onContext={(e, x, y) => openMenu(s, e, x, y)}
      onDropFiles={(dt, dest) => void onDropFiles(dt, dest, s)}
      onDropMove={(paths, dest, copy) => dropMove(paths, dest, copy, s)}
      onShortcut={(k) => shortcut(s, k)}
      onLeaveAdmin={() => updPane(s, { admin: false })}
      onRetryAdmin={() => (data(s).error?.code === 'needs_admin' ? updPane(s, { admin: true }) : data(s).reload())}
      dragPaths={(e) => (sel[s].has(e.path) ? [...sel[s]] : [e.path])}
    />
  );

  const bulk = targets.length > 0 && !dialog && !del && (
    <div className="files-bulk" role="toolbar" aria-label={t('bulk.label')}>
      <b>{t('selectedCount', { count: targets.length })}</b>
      {inTrash ? (
        <button type="button" onClick={() => void restore(targets, side)}>
          <Icon name="undo" />
          {t('act.restore')}
        </button>
      ) : (
        <>
          {targets.every((e) => !isDirLike(e)) && (
            <button type="button" onClick={() => download(targets)}>
              <Icon name="download" />
              {t('act.download')}
            </button>
          )}
          {split ? (
            <>
              <button type="button" disabled={!otherDest} onClick={() => copyTo(targets, side, otherSide, false)}>
                <Icon name="copy" />
                {t(side === 'L' ? 'act.copyRight' : 'act.copyLeft')}
              </button>
              <button type="button" disabled={!otherDest} onClick={() => copyTo(targets, side, otherSide, true)}>
                <Icon name="chevron" style={{ transform: `rotate(${side === 'L' ? -90 : 90}deg)` }} />
                {t(side === 'L' ? 'act.moveRight' : 'act.moveLeft')}
              </button>
            </>
          ) : (
            <>
              <button type="button" onClick={() => shortcut(side, 'copy')}>
                <Icon name="copy" />
                {t('act.copy')}
              </button>
              <button type="button" onClick={() => shortcut(side, 'cut')}>
                <Icon name="chevron" style={{ transform: 'rotate(-90deg)' }} />
                {t('act.cut')}
              </button>
            </>
          )}
        </>
      )}
      <button type="button" className="is-danger" onClick={() => requestDelete(targets, side)}>
        <Icon name="trash" />
        {inTrash ? t('act.deleteForever') : t('act.trash')}
      </button>
      <button type="button" className="files-bulk-x" aria-label={t('bulk.clear')} onClick={() => clearSel(side)}>
        <Icon name="close" />
      </button>
    </div>
  );

  const placesProps = { data: places, home, loc: pane(side).loc, bookmarks, remotes, onGo: goPlace, onRemoveBookmark: removeBookmark, onNew: newItems(side) };
  const delNames = del ? (del.entries.length === 1 ? del.entries[0].name : '') : '';

  return (
    <div className="files-root hue-file">
      {!mobile && (
        <aside className="files-places" aria-label={t('places')}>
          <PlacesList {...placesProps} />
        </aside>
      )}
      <div className="files-body">
        <div className="files-tabs" role="tablist" aria-label={t('tabs')}>
          {mobile && <IconButton icon="menu" label={t('places')} onClick={() => setPlacesSheet(true)} />}
          {tabs.map((x) => {
            const l = x.left.loc;
            const label = isVirtual(l) ? t(`place.${l.replace(':', '')}`) : l === home ? t('place.home') : basename(l) || '/';
            return (
              <div
                key={x.id}
                role="tab"
                aria-selected={x.id === tab.id}
                tabIndex={0}
                className={`files-tab${x.id === tab.id ? ' is-on' : ''}`}
                onClick={() => switchTab(x.id)}
                onAuxClick={(e) => e.button === 1 && closeTab(x.id)}
                onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && switchTab(x.id)}
              >
                <Icon name={x.left.admin ? 'lock' : isVirtual(l) ? (l === 'trash:' ? 'trash' : l === 'recent:' ? 'clock' : 'star') : l === home ? 'home' : 'files'} />
                <span className="files-tab-n">{label}</span>
                {x.split && !mobile && <small>+ {isVirtual(x.right.loc) ? t(`place.${x.right.loc.replace(':', '')}`) : basename(x.right.loc) || '/'}</small>}
                <button type="button" className="files-tab-x" aria-label={t('closeTab', { name: label })} onClick={(e) => (e.stopPropagation(), closeTab(x.id))}>
                  <Icon name="close" />
                </button>
              </div>
            );
          })}
          <IconButton icon="plus" label={t('newTab')} onClick={() => openTab()} />
          <div className="files-sp" />
          <IconButton icon={showHidden ? 'eye' : 'eyeoff'} label={showHidden ? t('hideHidden') : t('showHidden')} onClick={() => void set('files.showHidden', !showHidden)} />
          {!mobile && (
            <Button className={`files-splitb${tab.split ? ' is-on' : ''}`} icon="columns" aria-pressed={tab.split} onClick={() => setTabs((ts) => ts.map((x) => (x.id === tab.id ? { ...x, split: !x.split } : x)))}>
              {t('split')}
            </Button>
          )}
        </div>
        <div className={`files-duo${split ? ' is-split' : ''}`}>
          {renderPane('L')}
          {split && renderPane('R')}
        </div>
        {bulk}
        <TransferBar />
      </div>
      {panelOpen && (
        <SidePanel
          entries={panelEntries}
          admin={adminOf(side)}
          inline={!split && !mobile}
          inTrash={inTrash}
          starred={panelEntries.length === 1 && starred.has(panelEntries[0].path)}
          canChown={!!session?.canSudo}
          onClose={() => {
            setDismissed(selKey);
            setForcePanel(false);
          }}
          onEdit={(e) => setDialog({ kind: 'edit', entry: e, admin: adminOf(side) })}
          onDownload={(es) => download(es)}
          onCopyPath={(e) => void copyText(e.path)}
          onTrash={(es) => requestDelete(es, side)}
          onRename={(e) => setDialog({ kind: 'name', mode: 'rename', entry: e, side })}
          onStar={(e) => void toggleStar(e)}
          onChown={(e) => setDialog({ kind: 'chown', entry: e })}
        />
      )}
      {mobile && (
        <Sheet open={placesSheet} onClose={() => setPlacesSheet(false)} title={t('places')}>
          <div className="files-places files-places--sheet">
            <PlacesList {...placesProps} onPicked={() => setPlacesSheet(false)} />
          </div>
        </Sheet>
      )}
      <input
        ref={fileInput}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          const dest = writableLoc(side);
          const fs = Array.from(e.target.files ?? []);
          e.target.value = '';
          if (dest && fs.length) void uploadFiles(fs.map((f) => ({ file: f, rel: f.name })), dest, side);
        }}
      />
      {menu && <Menu items={menu.items} anchor={{ x: menu.x, y: menu.y }} onClose={() => setMenu(null)} aria-label={t('menu')} />}
      {dialog?.kind === 'name' && (
        <NameDialog
          title={dialog.mode === 'folder' ? t('new.folder') : dialog.mode === 'file' ? t('new.file') : t('act.rename')}
          label={dialog.mode === 'rename' ? t('newName') : t('name')}
          initial={dialog.mode === 'rename' ? dialog.entry!.name : dialog.mode === 'folder' ? t('new.folderDefault') : t('new.fileDefault')}
          confirm={dialog.mode === 'rename' ? t('act.rename') : t('create')}
          onClose={() => setDialog(null)}
          onSubmit={(name) => submitName(dialog.mode, dialog.side, dialog.entry, name)}
        />
      )}
      {dialog?.kind === 'edit' && <EditorDialog entry={dialog.entry} admin={dialog.admin} onClose={() => setDialog(null)} />}
      {dialog?.kind === 'chown' && <ChownDialog entry={dialog.entry} onClose={() => setDialog(null)} />}
      {dialog?.kind === 'image' && (
        <Dialog open size="lg" onClose={() => setDialog(null)} title={dialog.entry.name} icon="image" footer={<Button onClick={() => setDialog(null)}>{t('edit.close')}</Button>}>
          <div className="files-lightbox">
            <img src={downloadUrl(dialog.entry.path, dialog.admin, true)} alt={dialog.entry.name} />
          </div>
        </Dialog>
      )}
      <ConfirmDialog
        open={!!del}
        onClose={() => setDel(null)}
        onConfirm={() => doPermanent(del!.entries, del!.side, del!.trash)}
        title={del ? (del.trash ? t('confirm.foreverTitle', { count: del.entries.length }) : t('confirm.deleteTitle', { count: del.entries.length })) : ''}
        description={del ? (del.trash ? t('confirm.foreverText') : pane(del.side).admin || isRoot ? t('confirm.adminText') : t('confirm.permanentText')) : ''}
        confirmLabel={t('act.deleteForever')}
        confirmText={del && (pane(del.side).admin || isRoot) && !del.trash ? delNames || t('confirm.word') : undefined}
      >
        {del && del.entries.length > 1 && <p className="files-muted">{del.entries.slice(0, 5).map((e) => e.name).join(', ')}{del.entries.length > 5 ? '…' : ''}</p>}
      </ConfirmDialog>
      <ConfirmDialog open={emptyTrash} onClose={() => setEmptyTrash(false)} onConfirm={doEmptyTrash} title={t('confirm.emptyTitle')} description={t('confirm.emptyText', { count: data(side).entries.length })} confirmLabel={t('act.emptyTrash')} />
    </div>
  );
}
