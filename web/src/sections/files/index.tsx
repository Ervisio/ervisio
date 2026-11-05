import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ApiError, downloadUrl, usePrefs, useSession } from '../../api';
import { useT, type TFn } from '../../i18n';
import { Button, ConfirmDialog, Dialog, Icon, IconButton, Menu, Sheet, toast, useIsMobile, type IconName, type MenuItem } from '../../ui';
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
import { basename, dirname, isVirtual, join, nextId } from './util';
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

type NameMode = 'folder' | 'file' | 'rename';
type DialogState =
  | { kind: 'name'; mode: NameMode; entry?: FEntry; side: Side }
  | { kind: 'edit'; entry: FEntry; admin: boolean }
  | { kind: 'chown'; entry: FEntry }
  | { kind: 'image'; entry: FEntry; admin: boolean };
interface DeleteRequest {
  entries: FEntry[];
  side: Side;
  trash: boolean;
}
type Prefs = ReturnType<typeof usePrefs>['prefs'];
type SetPref = ReturnType<typeof usePrefs>['set'];
type TrashItem = { id: string; originalPath: string; deletedAt: number };
type BatchFailure = { path: string; message: string };

const PLACES_REFRESH_MS = 60_000;
const SAVE_TABS_DELAY_MS = 500;
const DOWNLOAD_STAGGER_MS = 350;
const MAX_EDIT_SIZE = 2 << 20;

const otherOf = (s: Side): Side => (s === 'L' ? 'R' : 'L');
const placeName = (loc: string, t: TFn) => t(`place.${loc.replace(':', '')}`);

function readFilePrefs(prefs: Prefs) {
  const starredList: string[] = Array.isArray(prefs['files.starred']) ? prefs['files.starred'] : [];
  const bookmarks: string[] = Array.isArray(prefs['files.bookmarks']) ? prefs['files.bookmarks'].filter((x: unknown) => typeof x === 'string') : [];
  const remotes: Remote[] = Array.isArray(prefs['files.remotes']) ? prefs['files.remotes'].filter((r: any) => r && typeof r.name === 'string') : [];
  const showHidden = !!prefs['files.showHidden'];
  return { starredList, bookmarks, remotes, showHidden };
}

/** Applies the saved tabs once prefs are ready; the returned ref is true from then on. */
function useRestoredTabs(ready: boolean, saved: unknown, home: string, apply: (r: { tabs: TabState[]; active: string }) => void): MutableRefObject<boolean> {
  const restored = useRef(false);
  useEffect(() => {
    if (!ready || restored.current) return;
    restored.current = true;
    const r = sanitizeTabs(saved, home);
    if (r) apply(r);
  }, [ready]); // eslint-disable-line react-hooks/exhaustive-deps
  return restored;
}

/** ?path=/some/dir or ?path=/some/file (links from Overview and Logs): opens it, then drops the parameter. */
function useLinkPath(ready: boolean, open: (dir: string, select?: string) => void) {
  const [params, setParams] = useSearchParams();
  const linkPath = params.get('path');
  useEffect(() => {
    if (!ready || !linkPath || !linkPath.startsWith('/')) return;
    let live = true;
    const openOnce = (dir: string, select?: string) => {
      if (!live) return;
      open(dir, select);
      setParams((p) => {
        const n = new URLSearchParams(p);
        n.delete('path');
        return n;
      }, { replace: true });
    };
    fcall<FEntry>('files.stat', { path: linkPath }).then(
      (e) => (isDirLike(e) ? openOnce(e.path || linkPath) : openOnce(dirname(linkPath), linkPath)),
      () => openOnce(dirname(linkPath), linkPath),
    );
    return () => {
      live = false;
    };
  }, [ready, linkPath]); // eslint-disable-line react-hooks/exhaustive-deps
}

function useSaveTabs(restored: MutableRefObject<boolean>, tabs: TabState[], activeId: string, set: SetPref) {
  useEffect(() => {
    if (!restored.current) return;
    const h = window.setTimeout(() => {
      void set('files.tabs', { active: activeId, tabs: tabs.map((x) => ({ id: x.id, split: x.split, left: { loc: x.left.loc, view: x.left.view }, right: { loc: x.right.loc, view: x.right.view } })) }).catch(() => undefined);
    }, SAVE_TABS_DELAY_MS);
    return () => window.clearTimeout(h);
  }, [tabs, activeId]); // eslint-disable-line react-hooks/exhaustive-deps
}

function usePlaces(): Places | null {
  const [places, setPlaces] = useState<Places | null>(null);
  const loadPlaces = useCallback(() => {
    fcall<Places>('files.places', {}).then(setPlaces, () => undefined);
  }, []);
  useEffect(() => {
    loadPlaces();
    const iv = window.setInterval(loadPlaces, PLACES_REFRESH_MS);
    const off = onFilesChanged(() => loadPlaces());
    return () => {
      window.clearInterval(iv);
      off();
    };
  }, [loadPlaces]);
  return places;
}

function fail(title: string, x: unknown) {
  if (x instanceof ApiError && x.code === 'cancelled') return;
  toast.err(title, x instanceof ApiError ? x.message : x instanceof Error ? x.message : String(x));
}

function toastFailed(failed: BatchFailure[] | undefined, title: (count: number) => string) {
  if (failed?.length) toast.err(title(failed.length), failed[0].message);
}

async function copyText(txt: string, t: TFn) {
  try {
    await navigator.clipboard.writeText(txt);
    toast.ok(t('pathCopied'));
  } catch {
    toast.err(t('err.clipboard'));
  }
}

function clickDownload(href: string, name: string) {
  const a = document.createElement('a');
  a.href = href;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
}

function downloadEntries(es: FEntry[], admin: boolean, t: TFn) {
  const files = es.filter((e) => !isDirLike(e) && e.type !== 'other');
  if (files.length === 0) return toast.info(t('err.noFolderDownload'));
  files.forEach((e, i) => window.setTimeout(() => clickDownload(downloadUrl(e.path, admin), e.name), i * DOWNLOAD_STAGGER_MS));
  if (es.length !== files.length) toast.info(t('err.foldersSkipped'));
}

/** The newest trash item of each path. */
function latestTrashIds(items: TrashItem[], paths: string[]): string[] {
  const ids: string[] = [];
  for (const p of paths) {
    const m = items.filter((i) => i.originalPath === p).sort((a, b) => b.deletedAt - a.deletedAt)[0];
    if (m) ids.push(m.id);
  }
  return ids;
}

async function undoTrash(paths: string[], t: TFn) {
  try {
    const r = await fcall<{ items: TrashItem[] }>('files.trashList', {});
    const ids = latestTrashIds(r.items, paths);
    if (ids.length) await fcall('files.restore', { ids });
    emitChanged();
  } catch (x) {
    fail(t('err.restore'), x);
  }
}

interface NameSave {
  mode: NameMode;
  dir: string;
  entry: FEntry | undefined;
  name: string;
  admin: boolean;
}

async function saveName({ mode, dir, entry, name, admin }: NameSave, t: TFn) {
  const target = join(dir, name);
  if (mode === 'folder') await fcall('files.mkdir', { path: target }, { admin });
  else if (mode === 'file') await fcall('files.create', { path: target }, { admin });
  else {
    if (target === entry!.path) return;
    await fcall('files.rename', { from: entry!.path, to: target }, { admin });
  }
  emitChanged(dir);
  if (mode !== 'rename') toast.ok(mode === 'folder' ? t('folderCreated', { name }) : t('fileCreated', { name }));
}

async function uploadInto(items: { file: File; rel: string }[], dest: string, admin: boolean, onElevate: () => void, t: TFn) {
  const made = new Set<string>();
  try {
    for (const it of items) {
      const sub = it.rel.includes('/') ? it.rel.slice(0, it.rel.lastIndexOf('/')) : '';
      if (sub && !made.has(sub)) {
        made.add(sub);
        await ensureDir(join(dest, sub), admin, onElevate);
      }
      enqueueUpload({ file: it.file, dest, rel: it.rel, admin, onElevate });
    }
  } catch (x) {
    fail(t('err.upload'), x);
  }
}

interface MenuOps {
  restore(): void;
  details(): void;
  remove(): void;
  emptyTrash(): void;
  open(e: FEntry): void;
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

function trashMenu({ t, paneEmpty, ops }: MenuContext, onEntry: boolean): MenuItem[] {
  if (!onEntry) return [{ id: 'empty', label: t('act.emptyTrash'), icon: 'trash', danger: true, disabled: paneEmpty, onSelect: ops.emptyTrash }];
  return [
    { id: 'restore', label: t('act.restore'), icon: 'undo', onSelect: ops.restore },
    { id: 'det', label: t('act.details'), icon: 'info', onSelect: ops.details },
    { id: 'del', label: t('act.deleteForever'), icon: 'trash', danger: true, kbd: 'Del', onSelect: ops.remove },
  ];
}

function clipItems({ t, ops }: MenuContext): MenuItem[] {
  return [
    { id: 'cp', label: t('act.copy'), icon: 'copy', kbd: 'Ctrl+C', onSelect: () => ops.shortcut('copy') },
    { id: 'ct', label: t('act.cut'), kbd: 'Ctrl+X', onSelect: () => ops.shortcut('cut') },
  ];
}

function splitCopyItems({ t, side, split, canWrite, ops }: MenuContext): MenuItem[] {
  if (!split) return [];
  const o = otherOf(side);
  const disabled = !canWrite(o);
  return [
    { id: 'c2', label: t(side === 'L' ? 'act.copyRight' : 'act.copyLeft'), disabled, onSelect: () => ops.copyTo(o, false) },
    { id: 'm2', label: t(side === 'L' ? 'act.moveRight' : 'act.moveLeft'), disabled, onSelect: () => ops.copyTo(o, true) },
  ];
}

function entryOpenItems(ctx: MenuContext, one: FEntry): MenuItem[] {
  const { t, side, split, ops } = ctx;
  const dirLike = isDirLike(one);
  const items: MenuItem[] = [{ id: 'open', label: dirLike ? t('act.open') : looksTextual(one) ? t('act.edit') : t('act.openFile'), icon: dirLike ? 'files' : 'eye', kbd: 'Enter', onSelect: () => ops.open(one) }];
  if (dirLike && split) items.push({ id: 'openo', label: t('act.openOther'), icon: 'columns', onSelect: () => ops.navigate(otherOf(side), one.path) });
  if (dirLike) items.push({ id: 'opent', label: t('act.openTab'), icon: 'plus', onSelect: () => ops.openTab(one.path) });
  if (isVirtual(ctx.loc) || ctx.searching) items.push({ id: 'show', label: t('act.showInFolder'), icon: 'search', onSelect: () => ops.navigate(side, dirname(one.path)) });
  if (!dirLike) items.push({ id: 'dl', label: t('act.download'), icon: 'download', onSelect: () => ops.download([one]) });
  return items;
}

function entryMenu(ctx: MenuContext, one: FEntry): MenuItem[] {
  const { t, clip, ops } = ctx;
  const dirLike = isDirLike(one);
  const items = entryOpenItems(ctx, one);
  items.push({ id: 'ren', label: t('act.rename'), icon: 'edit', kbd: 'F2', onSelect: () => ops.shortcut('rename') });
  items.push(...clipItems(ctx));
  if (dirLike && clip) items.push({ id: 'pi', label: t('act.pasteInto'), onSelect: () => ops.pasteInto(one.path) });
  items.push(...splitCopyItems(ctx));
  items.push({ id: 'star', label: ctx.starred.has(one.path) ? t('act.unstar') : t('act.star'), icon: 'star', onSelect: () => ops.toggleStar(one) });
  if (dirLike) items.push({ id: 'bm', label: t('act.bookmark'), icon: 'files', onSelect: () => ops.bookmark(one.path) });
  items.push({ id: 'path', label: t('act.copyPath'), icon: 'link', onSelect: () => ops.copyPath(one.path) });
  items.push({ id: 'det', label: t('act.details'), icon: 'info', onSelect: ops.details });
  items.push({ id: 'del', label: t('act.trash'), icon: 'trash', danger: true, kbd: 'Del', onSelect: ops.remove });
  return items;
}

function selectionMenu(ctx: MenuContext): MenuItem[] {
  const { t, entries, ops } = ctx;
  const items = clipItems(ctx);
  if (entries.every((x) => !isDirLike(x))) items.push({ id: 'dl', label: t('act.download'), icon: 'download', onSelect: () => ops.download(entries) });
  items.push(...splitCopyItems(ctx));
  items.push({ id: 'del', label: t('act.trashMany', { count: entries.length }), icon: 'trash', danger: true, kbd: 'Del', onSelect: ops.remove });
  return items;
}

function backgroundMenu({ t, side, loc, clip, showHidden, newItems, canWrite, ops }: MenuContext): MenuItem[] {
  const can = canWrite(side);
  const items: MenuItem[] = [
    ...newItems,
    { id: 'paste', label: t('act.paste'), kbd: 'Ctrl+V', disabled: !clip || !can, onSelect: ops.paste },
    { id: 'all', label: t('act.selectAll'), kbd: 'Ctrl+A', onSelect: ops.selectAll },
    { id: 'hid', label: showHidden ? t('hideHidden') : t('showHidden'), icon: showHidden ? 'eyeoff' : 'eye', onSelect: ops.toggleHidden },
  ];
  if (can && loc !== '/') items.push({ id: 'bm', label: t('act.bookmarkHere'), icon: 'files', onSelect: () => ops.bookmark(loc) });
  items.push({ id: 'ref', label: t('act.refresh'), icon: 'refresh', kbd: 'F5', onSelect: ops.refresh });
  return items;
}

function tabLabel(loc: string, home: string, t: TFn): string {
  if (isVirtual(loc)) return placeName(loc, t);
  return loc === home ? t('place.home') : basename(loc) || '/';
}

function tabIcon(x: TabState, home: string): IconName {
  const l = x.left.loc;
  if (x.left.admin) return 'lock';
  if (!isVirtual(l)) return l === home ? 'home' : 'files';
  return l === 'trash:' ? 'trash' : l === 'recent:' ? 'clock' : 'star';
}

interface FileTabProps {
  tab: TabState;
  home: string;
  isOn: boolean;
  showSplit: boolean;
  onSwitch(): void;
  onClose(): void;
}

function FileTab({ tab, home, isOn, showSplit, onSwitch, onClose }: FileTabProps) {
  const t = useT('files');
  const label = tabLabel(tab.left.loc, home, t);
  const right = tab.right.loc;
  return (
    <div
      role="tab"
      aria-selected={isOn}
      tabIndex={0}
      className={`files-tab${isOn ? ' is-on' : ''}`}
      onClick={onSwitch}
      onAuxClick={(e) => e.button === 1 && onClose()}
      onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && onSwitch()}
    >
      <Icon name={tabIcon(tab, home)} />
      <span className="files-tab-n">{label}</span>
      {showSplit && <small>+ {isVirtual(right) ? placeName(right, t) : basename(right) || '/'}</small>}
      <button type="button" className="files-tab-x" aria-label={t('closeTab', { name: label })} onClick={(e) => (e.stopPropagation(), onClose())}>
        <Icon name="close" />
      </button>
    </div>
  );
}

interface TabBarProps {
  tabs: TabState[];
  activeId: string;
  home: string;
  mobile: boolean;
  split: boolean;
  showHidden: boolean;
  onSwitch(id: string): void;
  onClose(id: string): void;
  onNewTab(): void;
  onPlaces(): void;
  onToggleHidden(): void;
  onToggleSplit(): void;
}

function TabBar({ tabs, activeId, home, mobile, split, showHidden, onSwitch, onClose, onNewTab, onPlaces, onToggleHidden, onToggleSplit }: TabBarProps) {
  const t = useT('files');
  return (
    <div className="files-tabs" role="tablist" aria-label={t('tabs')}>
      {mobile && <IconButton icon="menu" label={t('places')} onClick={onPlaces} />}
      {tabs.map((x) => (
        <FileTab key={x.id} tab={x} home={home} isOn={x.id === activeId} showSplit={x.split && !mobile} onSwitch={() => onSwitch(x.id)} onClose={() => onClose(x.id)} />
      ))}
      <IconButton icon="plus" label={t('newTab')} onClick={onNewTab} />
      <div className="files-sp" />
      <IconButton icon={showHidden ? 'eye' : 'eyeoff'} label={showHidden ? t('hideHidden') : t('showHidden')} onClick={onToggleHidden} />
      {!mobile && (
        <Button className={`files-splitb${split ? ' is-on' : ''}`} icon="columns" aria-pressed={split} onClick={onToggleSplit}>
          {t('split')}
        </Button>
      )}
    </div>
  );
}

interface BulkBarProps {
  targets: FEntry[];
  side: Side;
  split: boolean;
  inTrash: boolean;
  canCopyOther: boolean;
  onRestore(): void;
  onDownload(): void;
  onCopyTo(move: boolean): void;
  onClip(k: 'copy' | 'cut'): void;
  onDelete(): void;
  onClear(): void;
}

function BulkBar({ targets, side, split, inTrash, canCopyOther, onRestore, onDownload, onCopyTo, onClip, onDelete, onClear }: BulkBarProps) {
  const t = useT('files');
  return (
    <div className="files-bulk" role="toolbar" aria-label={t('bulk.label')}>
      <b>{t('selectedCount', { count: targets.length })}</b>
      {inTrash ? (
        <button type="button" onClick={onRestore}>
          <Icon name="undo" />
          {t('act.restore')}
        </button>
      ) : (
        <>
          {targets.every((e) => !isDirLike(e)) && (
            <button type="button" onClick={onDownload}>
              <Icon name="download" />
              {t('act.download')}
            </button>
          )}
          {split ? <BulkCopyToOther side={side} disabled={!canCopyOther} onCopyTo={onCopyTo} /> : <BulkClip onClip={onClip} />}
        </>
      )}
      <button type="button" className="is-danger" onClick={onDelete}>
        <Icon name="trash" />
        {inTrash ? t('act.deleteForever') : t('act.trash')}
      </button>
      <button type="button" className="files-bulk-x" aria-label={t('bulk.clear')} onClick={onClear}>
        <Icon name="close" />
      </button>
    </div>
  );
}

function BulkCopyToOther({ side, disabled, onCopyTo }: { side: Side; disabled: boolean; onCopyTo(move: boolean): void }) {
  const t = useT('files');
  return (
    <>
      <button type="button" disabled={disabled} onClick={() => onCopyTo(false)}>
        <Icon name="copy" />
        {t(side === 'L' ? 'act.copyRight' : 'act.copyLeft')}
      </button>
      <button type="button" disabled={disabled} onClick={() => onCopyTo(true)}>
        <Icon name="chevron" style={{ transform: `rotate(${side === 'L' ? -90 : 90}deg)` }} />
        {t(side === 'L' ? 'act.moveRight' : 'act.moveLeft')}
      </button>
    </>
  );
}

function BulkClip({ onClip }: { onClip(k: 'copy' | 'cut'): void }) {
  const t = useT('files');
  return (
    <>
      <button type="button" onClick={() => onClip('copy')}>
        <Icon name="copy" />
        {t('act.copy')}
      </button>
      <button type="button" onClick={() => onClip('cut')}>
        <Icon name="chevron" style={{ transform: 'rotate(-90deg)' }} />
        {t('act.cut')}
      </button>
    </>
  );
}

interface FileDialogProps {
  dialog: DialogState | null;
  onClose(): void;
  onSubmitName(mode: NameMode, side: Side, entry: FEntry | undefined, name: string): Promise<void>;
}

function FileDialog({ dialog, onClose, onSubmitName }: FileDialogProps) {
  const t = useT('files');
  if (dialog?.kind === 'name')
    return (
      <NameDialog
        title={dialog.mode === 'folder' ? t('new.folder') : dialog.mode === 'file' ? t('new.file') : t('act.rename')}
        label={dialog.mode === 'rename' ? t('newName') : t('name')}
        initial={dialog.mode === 'rename' ? dialog.entry!.name : dialog.mode === 'folder' ? t('new.folderDefault') : t('new.fileDefault')}
        confirm={dialog.mode === 'rename' ? t('act.rename') : t('create')}
        onClose={onClose}
        onSubmit={(name) => onSubmitName(dialog.mode, dialog.side, dialog.entry, name)}
      />
    );
  if (dialog?.kind === 'edit') return <EditorDialog entry={dialog.entry} admin={dialog.admin} onClose={onClose} />;
  if (dialog?.kind === 'chown') return <ChownDialog entry={dialog.entry} onClose={onClose} />;
  if (dialog?.kind === 'image')
    return (
      <Dialog open size="lg" onClose={onClose} title={dialog.entry.name} icon="image" footer={<Button onClick={onClose}>{t('edit.close')}</Button>}>
        <div className="files-lightbox">
          <img src={downloadUrl(dialog.entry.path, dialog.admin, true)} alt={dialog.entry.name} />
        </div>
      </Dialog>
    );
  return null;
}

interface DeleteConfirmProps {
  del: DeleteRequest | null;
  /** The pane is elevated or the user is root: the name must be typed to confirm. */
  strict: boolean;
  onClose(): void;
  onConfirm(): Promise<void>;
}

function DeleteConfirm({ del, strict, onClose, onConfirm }: DeleteConfirmProps) {
  const t = useT('files');
  const delNames = del ? (del.entries.length === 1 ? del.entries[0].name : '') : '';
  return (
    <ConfirmDialog
      open={!!del}
      onClose={onClose}
      onConfirm={onConfirm}
      title={del ? (del.trash ? t('confirm.foreverTitle', { count: del.entries.length }) : t('confirm.deleteTitle', { count: del.entries.length })) : ''}
      description={del ? (del.trash ? t('confirm.foreverText') : strict ? t('confirm.adminText') : t('confirm.permanentText')) : ''}
      confirmLabel={t('act.deleteForever')}
      confirmText={del && strict && !del.trash ? delNames || t('confirm.word') : undefined}
    >
      {del && del.entries.length > 1 && <p className="files-muted">{del.entries.slice(0, 5).map((e) => e.name).join(', ')}{del.entries.length > 5 ? '…' : ''}</p>}
    </ConfirmDialog>
  );
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
  const [sel, setSel] = useState<Record<Side, ReadonlySet<string>>>({ L: none, R: none });
  const [query, setQuery] = useState<Record<Side, string>>({ L: '', R: '' });
  const [sort, setSort] = useState<Record<Side, SortState>>({ L: SORT0, R: SORT0 });
  const [clip, setClip] = useState<Clip | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number; items: MenuItem[] } | null>(null);
  const [dialog, setDialog] = useState<DialogState | null>(null);
  const [del, setDel] = useState<DeleteRequest | null>(null);
  const [emptyTrash, setEmptyTrash] = useState(false);
  const [placesSheet, setPlacesSheet] = useState(false);
  const [dismissed, setDismissed] = useState('');
  const [forcePanel, setForcePanel] = useState(false);
  const mobile = useIsMobile();
  const fileInput = useRef<HTMLInputElement>(null);

  const { starredList, bookmarks, remotes, showHidden } = readFilePrefs(prefs);
  const starred = useMemo(() => new Set(starredList), [starredList.join('\n')]); // eslint-disable-line react-hooks/exhaustive-deps

  const restored = useRestoredTabs(ready, prefs['files.tabs'], home, (r) => {
    setTabs(r.tabs);
    setActiveId(r.active);
  });
  useLinkPath(ready, (dir, select) => {
    const nt = newTab(home, dir);
    setTabs((ts) => {
      const cur = ts.find((x) => x.left.loc === dir && !x.split);
      if (cur) {
        setActiveId(cur.id);
        return ts;
      }
      setActiveId(nt.id);
      return [...ts, nt].slice(-12);
    });
    setAct('L');
    setQuery({ L: '', R: '' });
    setSel({ L: select ? new Set([select]) : none, R: none });
  });
  useSaveTabs(restored, tabs, activeId, set);

  useEffect(() => {
    setUnlockProbe(() => isUnlocked || isRoot);
  }, [isUnlocked, isRoot]);

  const tab = tabs.find((x) => x.id === activeId) ?? tabs[0];
  const split = tab.split && !mobile;
  const side: Side = split ? act : 'L';
  const pane = (s: Side) => (s === 'L' ? tab.left : tab.right);
  const updPane = (s: Side, patch: Partial<PaneState>) => setTabs((ts) => ts.map((x) => (x.id === tab.id ? { ...x, [s === 'L' ? 'left' : 'right']: { ...(s === 'L' ? x.left : x.right), ...patch } } : x)));
  const elevate = (s: Side) => () => updPane(s, { admin: true });
  const toggleSplit = () => setTabs((ts) => ts.map((x) => (x.id === tab.id ? { ...x, split: !x.split } : x)));
  const clearSel = (s?: Side) => setSel((c) => (s ? { ...c, [s]: none } : { L: none, R: none }));

  const places = usePlaces();

  const onElevateL = useCallback(() => updPaneRef.current('L', { admin: true }), []);
  const onElevateR = useCallback(() => updPaneRef.current('R', { admin: true }), []);
  const updPaneRef = useRef(updPane);
  updPaneRef.current = updPane;
  const dL = usePaneData({ loc: tab.left.loc, admin: tab.left.admin, showHidden, starred: starredList, query: query.L, onElevate: onElevateL, enabled: true });
  const dR = usePaneData({ loc: tab.right.loc, admin: tab.right.admin, showHidden, starred: starredList, query: query.R, onElevate: onElevateR, enabled: split });
  const data = (s: Side) => (s === 'L' ? dL : dR);

  const selectedEntries = (s: Side): FEntry[] => data(s).entries.filter((e) => sel[s].has(e.path));
  const targets = selectedEntries(side);

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

  const adminOf = (s: Side) => pane(s).admin;
  const otherSide = otherOf(side);
  const writableLoc = (s: Side) => {
    const l = pane(s).loc;
    return isVirtual(l) ? null : l;
  };
  const refreshAll = () => emitChanged();

  const download = (es: FEntry[], s: Side = side) => downloadEntries(es, adminOf(s), t);

  const doTrash = async (es: FEntry[], s: Side) => {
    const paths = es.map((e) => e.path);
    try {
      const r = await fcall<{ deleted: string[]; failed: BatchFailure[] }>('files.delete', { paths, trash: true }, { admin: adminOf(s) });
      clearSel(s);
      emitChanged();
      if ((r.deleted ?? []).length) toast.undo(t('trashed', { count: (r.deleted ?? []).length }), t('undo'), () => void undoTrash(r.deleted, t));
      toastFailed(r.failed, (count) => t('err.someNotDeleted', { count }));
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
      const r = await fcall<{ restored: string[]; failed: BatchFailure[] }>('files.restore', { ids: es.map((e) => e.trashId).filter(Boolean) }, { admin: adminOf(s) });
      clearSel(s);
      emitChanged();
      if ((r.restored ?? []).length) toast.ok(t('restored', { count: (r.restored ?? []).length }));
      toastFailed(r.failed, (count) => t('err.someNotRestored', { count }));
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

  const submitName = async (mode: NameMode, s: Side, entry: FEntry | undefined, name: string) => {
    const dir = mode === 'rename' ? dirname(entry!.path) : writableLoc(s);
    if (!dir) return;
    await saveName({ mode, dir, entry, name, admin: adminOf(s) }, t);
  };

  const enqueueClip = (c: Clip, dest: string, s: Side) => enqueueCopy({ from: c.paths, to: dest, move: c.cut, admin: adminOf(s), onElevate: elevate(s) });
  const paste = (s: Side) => {
    const dest = writableLoc(s);
    if (!clip || !dest) return;
    enqueueClip(clip, dest, s);
    if (clip.cut) setClip(null);
  };
  const copyTo = (es: FEntry[], from: Side, to: Side, move: boolean) => {
    const dest = writableLoc(to);
    if (!dest) return toast.info(t('err.noTargetFolder'));
    enqueueCopy({ from: es.map((e) => e.path), to: dest, move, admin: adminOf(from) || adminOf(to), onElevate: elevate(to) });
    clearSel(from);
  };
  const dropMove = (paths: string[], dest: string, copy: boolean, s: Side) => {
    const same = paths.every((p) => dirname(p) === dest);
    if (same && !copy) return;
    enqueueCopy({ from: paths, to: dest, move: !copy, admin: adminOf(s), onElevate: elevate(s) });
  };

  const uploadFiles = (items: { file: File; rel: string }[], dest: string, s: Side) => uploadInto(items, dest, adminOf(s), elevate(s), t);
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
  const toggleHidden = () => void set('files.showHidden', !showHidden);

  const openEntry = (e: FEntry, s: Side) => {
    if (pane(s).loc === 'trash:') return;
    if (isDirLike(e)) return navigate(s, e.path);
    const admin = adminOf(s);
    if (kindOf(e) === 'img' && canInlineImage(e.name)) return setDialog({ kind: 'image', entry: e, admin });
    if (looksTextual(e) && e.size <= MAX_EDIT_SIZE) return setDialog({ kind: 'edit', entry: e, admin });
    download([e], s);
  };

  const clipSelection = (es: FEntry[], cut: boolean) => {
    setClip({ paths: es.map((e) => e.path), cut });
    toast.info(cut ? t('clip.cut', { count: es.length }) : t('clip.copied', { count: es.length }));
  };

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
        if (es.length && loc !== 'trash:') clipSelection(es, k === 'cut');
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

  const newItems = (s: Side): MenuItem[] => {
    const can = !!writableLoc(s);
    return [
      { id: 'nf', label: t('new.folder'), icon: 'files', kbd: 'Ctrl+Shift+N', disabled: !can, onSelect: () => setDialog({ kind: 'name', mode: 'folder', side: s }) },
      { id: 'nt', label: t('new.file'), icon: 'file', disabled: !can, onSelect: () => setDialog({ kind: 'name', mode: 'file', side: s }) },
      { id: 'up', label: t('new.upload'), icon: 'upload', disabled: !can, onSelect: () => fileInput.current?.click() },
    ];
  };

  const menuOps = (s: Side, es: FEntry[]): MenuOps => ({
    restore: () => void restore(es, s),
    details: () => setForcePanel(true),
    remove: () => requestDelete(es, s),
    emptyTrash: () => setEmptyTrash(true),
    open: (e) => openEntry(e, s),
    navigate,
    openTab,
    download: (list) => download(list, s),
    shortcut: (k) => shortcut(s, k),
    pasteInto: (dest) => {
      if (clip) enqueueClip(clip, dest, s);
    },
    copyTo: (to, move) => copyTo(es, s, to, move),
    toggleStar: (e) => void toggleStar(e),
    bookmark: (path) => void addBookmark(path),
    copyPath: (path) => void copyText(path, t),
    paste: () => paste(s),
    selectAll: () => setSel((c) => ({ ...c, [s]: new Set(data(s).entries.map((x) => x.path)) })),
    toggleHidden,
    refresh: refreshAll,
  });

  const openMenu = (s: Side, e: FEntry | null, x: number, y: number) => {
    const loc = pane(s).loc;
    const es = e ? (sel[s].has(e.path) ? selectedEntries(s) : [e]) : [];
    const ctx: MenuContext = {
      t,
      side: s,
      loc,
      entries: es,
      split,
      clip,
      searching: !!query[s],
      showHidden,
      starred,
      paneEmpty: data(s).entries.length === 0,
      newItems: loc === 'trash:' || e ? [] : newItems(s),
      canWrite: (o) => !!writableLoc(o),
      ops: menuOps(s, es),
    };
    setMenu({ x, y, items: contextMenu(ctx, !!e) });
  };

  usePaletteActions('files', [
    { id: 'files.home', title: t('palette.home'), hint: t('title'), icon: 'home', hue: 'file', run: () => goPlace(home) },
    { id: 'files.trash', title: t('palette.trash'), hint: t('title'), icon: 'trash', hue: 'file', run: () => goPlace('trash:') },
    { id: 'files.newfolder', title: t('palette.newFolder'), hint: t('title'), icon: 'files', hue: 'file', run: () => shortcut(side, 'newFolder') },
    { id: 'files.split', title: t('palette.split'), hint: t('title'), icon: 'columns', hue: 'file', run: toggleSplit },
  ]);

  const panelEntries = targets;
  const selKey = panelEntries.map((e) => e.path).join('\n');
  useEffect(() => {
    if (panelEntries.length === 0) setForcePanel(false);
  }, [panelEntries.length]);
  const panelOpen = panelEntries.length > 0 && (mobile ? forcePanel : split ? forcePanel : dismissed !== selKey);

  const inTrash = pane(side).loc === 'trash:';
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

  const placesProps = { data: places, home, loc: pane(side).loc, bookmarks, remotes, onGo: goPlace, onRemoveBookmark: removeBookmark, onNew: newItems(side) };

  return (
    <div className="files-root hue-file">
      {!mobile && (
        <aside className="files-places" aria-label={t('places')}>
          <PlacesList {...placesProps} />
        </aside>
      )}
      <div className="files-body">
        <TabBar
          tabs={tabs}
          activeId={tab.id}
          home={home}
          mobile={mobile}
          split={tab.split}
          showHidden={showHidden}
          onSwitch={switchTab}
          onClose={closeTab}
          onNewTab={() => openTab()}
          onPlaces={() => setPlacesSheet(true)}
          onToggleHidden={toggleHidden}
          onToggleSplit={toggleSplit}
        />
        <div className={`files-duo${split ? ' is-split' : ''}`}>
          {renderPane('L')}
          {split && renderPane('R')}
        </div>
        {targets.length > 0 && !dialog && !del && (
          <BulkBar
            targets={targets}
            side={side}
            split={split}
            inTrash={inTrash}
            canCopyOther={split && !!writableLoc(otherSide)}
            onRestore={() => void restore(targets, side)}
            onDownload={() => download(targets)}
            onCopyTo={(move) => copyTo(targets, side, otherSide, move)}
            onClip={(k) => shortcut(side, k)}
            onDelete={() => requestDelete(targets, side)}
            onClear={() => clearSel(side)}
          />
        )}
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
          onCopyPath={(e) => void copyText(e.path, t)}
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
      <FileDialog dialog={dialog} onClose={() => setDialog(null)} onSubmitName={submitName} />
      <DeleteConfirm del={del} strict={!!del && (pane(del.side).admin || isRoot)} onClose={() => setDel(null)} onConfirm={() => doPermanent(del!.entries, del!.side, del!.trash)} />
      <ConfirmDialog open={emptyTrash} onClose={() => setEmptyTrash(false)} onConfirm={doEmptyTrash} title={t('confirm.emptyTitle')} description={t('confirm.emptyText', { count: data(side).entries.length })} confirmLabel={t('act.emptyTrash')} />
    </div>
  );
}
