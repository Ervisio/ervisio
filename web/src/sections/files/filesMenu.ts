import { useState, type RefObject } from 'react';
import type { TFn } from '../../i18n';
import type { MenuItem } from '../../ui';
import type { PaneData } from './data';
import { otherOf, type Clip, type Side } from './filesPageTypes';
import { copyText } from './filesOps';
import { looksTextual } from './kinds';
import type { Shortcut } from './Pane';
import { isDirLike, type FEntry } from './types';
import { dirname, isVirtual } from './util';
import type { FilesActions } from './useFilesActions';
import type { FilesPanel } from './useFilesPanel';
import type { FilesPrefs } from './useFilesPrefs';
import type { FilesTabs } from './useFilesTabs';
import { isRemoteLocation } from './remoteLocation';
import type { EditorMode } from '../editor/document';

interface MenuOps {
  restore(): void;
  details(): void;
  remove(): void;
  emptyTrash(): void;
  open(e: FEntry): void;
  openEditor(e: FEntry, mode: EditorMode): void;
  terminal(path: string): void;
  folderEditor(path: string): void;
  navigate(s: Side, loc: string): void;
  openTab(loc: string): void;
  download(es: FEntry[]): void;
  shortcut(k: Shortcut): void;
  pasteInto(dest: string): void;
  copyTo(to: Side, move: boolean): void;
  toggleStar(e: FEntry): void;
  bookmark(path: string): void;
  copyPath(path: string): void;
  paste(): void;
  selectAll(): void;
  toggleHidden(): void;
  refresh(): void;
}

interface MenuContext {
  native?: boolean;
  t: TFn;
  side: Side;
  loc: string;
  entries: FEntry[];
  split: boolean;
  clip: Clip | null;
  searching: boolean;
  showHidden: boolean;
  starred: ReadonlySet<string>;
  paneEmpty: boolean;
  newItems: MenuItem[];
  canWrite(s: Side): boolean;
  ops: MenuOps;
}

function contextMenu(ctx: MenuContext, onEntry: boolean): MenuItem[] {
  if (ctx.loc === 'trash:') return trashMenu(ctx, onEntry);
  if (onEntry && ctx.entries.length === 1) return entryMenu(ctx, ctx.entries[0]);
  if (onEntry) return selectionMenu(ctx);
  return backgroundMenu(ctx);
}

function trashMenu({ t, paneEmpty, ops, native }: MenuContext, onEntry: boolean): MenuItem[] {
  if (!onEntry)
    return [
      {
        id: 'empty',
        label: t('act.emptyTrash'),
        icon: 'trash',
        danger: true,
        disabled: paneEmpty,
        onSelect: ops.emptyTrash,
      },
    ];
  return [
    { id: 'restore', label: t('act.restore'), icon: 'undo', onSelect: ops.restore },
    {
      id: 'det',
      label: t(native ? 'act.properties' : 'act.details'),
      icon: 'info',
      onSelect: ops.details,
    },
    {
      id: 'del',
      label: t('act.deleteForever'),
      icon: 'trash',
      danger: true,
      kbd: 'Del',
      onSelect: ops.remove,
    },
  ];
}

function clipItems({ t, ops }: MenuContext): MenuItem[] {
  return [
    {
      id: 'cp',
      label: t('act.copy'),
      icon: 'copy',
      kbd: 'Ctrl+C',
      onSelect: () => ops.shortcut('copy'),
    },
    { id: 'ct', label: t('act.cut'), kbd: 'Ctrl+X', onSelect: () => ops.shortcut('cut') },
  ];
}

function splitCopyItems({ t, side, split, canWrite, ops }: MenuContext): MenuItem[] {
  if (!split) return [];
  const o = otherOf(side);
  const disabled = !canWrite(o);
  return [
    {
      id: 'c2',
      label: t(side === 'L' ? 'act.copyRight' : 'act.copyLeft'),
      disabled,
      onSelect: () => ops.copyTo(o, false),
    },
    {
      id: 'm2',
      label: t(side === 'L' ? 'act.moveRight' : 'act.moveLeft'),
      disabled,
      onSelect: () => ops.copyTo(o, true),
    },
  ];
}

function entryOpenItems(ctx: MenuContext, one: FEntry): MenuItem[] {
  const { t, side, split, ops } = ctx;
  const dirLike = isDirLike(one);
  const items: MenuItem[] = [
    {
      id: 'open',
      label: dirLike ? t('act.open') : looksTextual(one) ? t('act.edit') : t('act.openFile'),
      icon: dirLike ? 'files' : 'eye',
      kbd: 'Enter',
      onSelect: () => ops.open(one),
    },
  ];
  if (dirLike && split)
    items.push({
      id: 'openo',
      label: t('act.openOther'),
      icon: 'columns',
      onSelect: () => ops.navigate(otherOf(side), one.path),
    });
  if (dirLike)
    items.push({
      id: 'opent',
      label: t('act.openTab'),
      icon: 'plus',
      onSelect: () => ops.openTab(one.path),
    });
  if (!isRemoteLocation(one.path))
    items.push({
      id: 'terminal',
      label: t('act.terminalHere'),
      icon: 'terminal',
      onSelect: () => ops.terminal(dirLike ? one.path : dirname(one.path)),
    });
  if (dirLike)
    items.push({
      id: 'folder-code',
      label: t('act.openFolderCode'),
      icon: 'code',
      onSelect: () => ops.folderEditor(one.path),
    });
  if (!dirLike && looksTextual(one))
    items.push(
      {
        id: 'notepad',
        label: t('act.openNotepad'),
        icon: 'file',
        onSelect: () => ops.openEditor(one, 'text'),
      },
      {
        id: 'code',
        label: t('act.openCodeEditor'),
        icon: 'code',
        onSelect: () => ops.openEditor(one, 'code'),
      },
    );
  if (isVirtual(ctx.loc) || ctx.searching)
    items.push({
      id: 'show',
      label: t('act.showInFolder'),
      icon: 'search',
      onSelect: () => ops.navigate(side, dirname(one.path)),
    });
  if (!dirLike)
    items.push({
      id: 'dl',
      label: t('act.download'),
      icon: 'download',
      onSelect: () => ops.download([one]),
    });
  return items;
}

function entryMenu(ctx: MenuContext, one: FEntry): MenuItem[] {
  const { t, clip, ops } = ctx;
  const dirLike = isDirLike(one);
  const items = entryOpenItems(ctx, one);
  items.push({
    id: 'ren',
    label: t('act.rename'),
    icon: 'edit',
    kbd: 'F2',
    onSelect: () => ops.shortcut('rename'),
  });
  items.push(...clipItems(ctx));
  if (dirLike && clip)
    items.push({ id: 'pi', label: t('act.pasteInto'), onSelect: () => ops.pasteInto(one.path) });
  items.push(...splitCopyItems(ctx));
  items.push({
    id: 'star',
    label: ctx.starred.has(one.path) ? t('act.unstar') : t('act.star'),
    icon: 'star',
    onSelect: () => ops.toggleStar(one),
  });
  if (dirLike)
    items.push({
      id: 'bm',
      label: t('act.bookmark'),
      icon: 'files',
      onSelect: () => ops.bookmark(one.path),
    });
  items.push({
    id: 'path',
    label: t('act.copyPath'),
    icon: 'link',
    onSelect: () => ops.copyPath(one.path),
  });
  items.push({
    id: 'det',
    label: t(ctx.native ? 'act.properties' : 'act.details'),
    icon: 'info',
    onSelect: ops.details,
  });
  items.push({
    id: 'del',
    label: t('act.trash'),
    icon: 'trash',
    danger: true,
    kbd: 'Del',
    onSelect: ops.remove,
  });
  return items;
}

function selectionMenu(ctx: MenuContext): MenuItem[] {
  const { t, entries, ops } = ctx;
  const items = clipItems(ctx);
  if (entries.every((x) => !isDirLike(x)))
    items.push({
      id: 'dl',
      label: t('act.download'),
      icon: 'download',
      onSelect: () => ops.download(entries),
    });
  items.push(...splitCopyItems(ctx));
  if (ctx.native)
    items.push({ id: 'det', label: t('act.properties'), icon: 'info', onSelect: ops.details });
  items.push({
    id: 'del',
    label: t('act.trashMany', { count: entries.length }),
    icon: 'trash',
    danger: true,
    kbd: 'Del',
    onSelect: ops.remove,
  });
  return items;
}

function backgroundMenu({
  t,
  side,
  loc,
  clip,
  showHidden,
  newItems,
  canWrite,
  ops,
}: MenuContext): MenuItem[] {
  const can = canWrite(side);
  const items: MenuItem[] = [
    ...newItems,
    {
      id: 'paste',
      label: t('act.paste'),
      kbd: 'Ctrl+V',
      disabled: !clip || !can,
      onSelect: ops.paste,
    },
    { id: 'all', label: t('act.selectAll'), kbd: 'Ctrl+A', onSelect: ops.selectAll },
    {
      id: 'hid',
      label: showHidden ? t('hideHidden') : t('showHidden'),
      icon: showHidden ? 'eyeoff' : 'eye',
      onSelect: ops.toggleHidden,
    },
  ];
  if (!isVirtual(loc) && !isRemoteLocation(loc))
    items.push({
      id: 'terminal',
      label: t('act.terminalHere'),
      icon: 'terminal',
      onSelect: () => ops.terminal(loc),
    });
  if (!isVirtual(loc))
    items.push({
      id: 'folder-code',
      label: t('act.openFolderCode'),
      icon: 'code',
      onSelect: () => ops.folderEditor(loc),
    });
  if (can && loc !== '/')
    items.push({
      id: 'bm',
      label: t('act.bookmarkHere'),
      icon: 'files',
      onSelect: () => ops.bookmark(loc),
    });
  items.push({
    id: 'ref',
    label: t('act.refresh'),
    icon: 'refresh',
    kbd: 'F5',
    onSelect: ops.refresh,
  });
  return items;
}

interface FilesMenuArgs {
  native?: boolean;
  t: TFn;
  tabs: FilesTabs;
  data(s: Side): PaneData;
  selectedEntries(s: Side): FEntry[];
  prefs: FilesPrefs;
  actions: FilesActions;
  panel: FilesPanel;
  fileInput: RefObject<HTMLInputElement>;
}

/** The right-click menu of a pane, and the "new" items it shares with the places list. */
export function useFilesMenu(args: FilesMenuArgs) {
  const { t, tabs, data, selectedEntries, prefs, actions, panel, fileInput } = args;
  const { split, sel, setSel, query, pane, writableLoc } = tabs;
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null);

  const newItems = (s: Side): MenuItem[] => {
    const can = !!writableLoc(s);
    return [
      {
        id: 'nf',
        label: t('new.folder'),
        icon: 'files',
        kbd: 'Ctrl+Shift+N',
        disabled: !can,
        onSelect: () => actions.setDialog({ kind: 'name', mode: 'folder', side: s }),
      },
      {
        id: 'nt',
        label: t('new.file'),
        icon: 'file',
        disabled: !can,
        onSelect: () => actions.setDialog({ kind: 'name', mode: 'file', side: s }),
      },
      {
        id: 'up',
        label: t('new.upload'),
        icon: 'upload',
        disabled: !can,
        onSelect: () => fileInput.current?.click(),
      },
    ];
  };

  const menuOps = (s: Side, es: FEntry[]): MenuOps => ({
    restore: () => void actions.restore(es, s),
    details: () => panel.show(es),
    remove: () => actions.requestDelete(es, s),
    emptyTrash: actions.askEmptyTrash,
    open: (e) => actions.openEntry(e, s),
    openEditor: (e, mode) => actions.openEditor(e, s, mode),
    terminal: (path) => actions.openTerminal(path, s),
    folderEditor: (path) => actions.openFolderEditor(path, s),
    navigate: tabs.navigate,
    openTab: tabs.openTab,
    download: (list) => actions.download(list, s),
    shortcut: (k) => actions.shortcut(s, k),
    pasteInto: (dest) => actions.pasteInto(dest, s),
    copyTo: (to, move) => actions.copyTo(es, s, to, move),
    toggleStar: (e) => void prefs.toggleStar(e),
    bookmark: (path) => void prefs.addBookmark(path),
    copyPath: (path) => void copyText(path, t),
    paste: () => actions.paste(s),
    selectAll: () => setSel((c) => ({ ...c, [s]: new Set(data(s).entries.map((x) => x.path)) })),
    toggleHidden: prefs.toggleHidden,
    refresh: actions.refreshAll,
  });

  const openMenu = (s: Side, e: FEntry | null, x: number, y: number) => {
    const loc = pane(s).loc;
    const es = e ? (sel[s].has(e.path) ? selectedEntries(s) : [e]) : [];
    const ctx: MenuContext = {
      native: args.native,
      t,
      side: s,
      loc,
      entries: es,
      split,
      clip: actions.clip,
      searching: !!query[s],
      showHidden: prefs.showHidden,
      starred: prefs.starred,
      paneEmpty: data(s).entries.length === 0,
      newItems: loc === 'trash:' || e ? [] : newItems(s),
      canWrite: (o) => !!writableLoc(o),
      ops: menuOps(s, es),
    };
    setMenu({ x, y, items: contextMenu(ctx, !!e) });
  };

  return { menu, closeMenu: () => setMenu(null), openMenu, newItems };
}
