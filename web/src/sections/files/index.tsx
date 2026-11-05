import { useEffect, useRef, useState, type RefObject } from 'react';
import { usePrefs, useSession } from '../../api';
import { useT, type TFn } from '../../i18n';
import {
  Button,
  ConfirmDialog,
  Dialog,
  Icon,
  IconButton,
  Menu,
  Sheet,
  useIsMobile,
  type IconName,
} from '../../ui';
import { ChownDialog, EditorDialog, NameDialog } from './Dialogs';
import { Pane } from './Pane';
import { NativeComputer, NativeFilesCommandBar } from './NativeFiles';
import { PlacesList } from './Places';
import { SidePanel } from './SidePanel';
import { TransferBar } from './TransferBar';
import { fileDownloadUrl, setUnlockProbe } from './fapi';
import { useRemoteConnections } from './RemoteConnections';
import { isRemoteLocation } from './remoteLocation';
import { copyText } from './filesOps';
import {
  otherOf,
  type DeleteRequest,
  type DialogState,
  type NameMode,
  type Side,
} from './filesPageTypes';
import { useFilesMenu } from './filesMenu';
import { isDirLike, type FEntry, type TabState } from './types';
import { useFilesActions } from './useFilesActions';
import { useFilesPaneData, usePlaces } from './useFilesData';
import { useFilesPalette } from './useFilesPalette';
import { useFilesPanel } from './useFilesPanel';
import { useFilesPrefs } from './useFilesPrefs';
import { useFilesTabs } from './useFilesTabs';
import { basename, isVirtual } from './util';
import './files.css';
import './native-files.css';

const placeName = (loc: string, t: TFn) => t(`place.${loc.replace(':', '')}`);

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
      {showSplit && (
        <small>+ {isVirtual(right) ? placeName(right, t) : basename(right) || '/'}</small>
      )}
      <button
        type="button"
        className="files-tab-x"
        aria-label={t('closeTab', { name: label })}
        onClick={(e) => (e.stopPropagation(), onClose())}
      >
        <Icon name="close" />
      </button>
    </div>
  );
}

interface TabBarProps {
  native?: boolean;
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

function TabBar({
  native,
  tabs,
  activeId,
  home,
  mobile,
  split,
  showHidden,
  onSwitch,
  onClose,
  onNewTab,
  onPlaces,
  onToggleHidden,
  onToggleSplit,
}: TabBarProps) {
  const t = useT('files');
  return (
    <div className="files-tabs" role="tablist" aria-label={t('tabs')}>
      {(mobile || native) && <IconButton icon="menu" label={t('places')} onClick={onPlaces} />}
      {tabs.map((x) => (
        <FileTab
          key={x.id}
          tab={x}
          home={home}
          isOn={x.id === activeId}
          showSplit={x.split && !mobile}
          onSwitch={() => onSwitch(x.id)}
          onClose={() => onClose(x.id)}
        />
      ))}
      <IconButton icon="plus" label={t('newTab')} onClick={onNewTab} />
      <div className="files-sp" />
      <IconButton
        icon={showHidden ? 'eye' : 'eyeoff'}
        label={showHidden ? t('hideHidden') : t('showHidden')}
        onClick={onToggleHidden}
      />
      {!mobile && (
        <Button
          className={`files-splitb${split ? ' is-on' : ''}`}
          icon="columns"
          aria-pressed={split}
          onClick={onToggleSplit}
        >
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

function BulkBar({
  targets,
  side,
  split,
  inTrash,
  canCopyOther,
  onRestore,
  onDownload,
  onCopyTo,
  onClip,
  onDelete,
  onClear,
}: BulkBarProps) {
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
          {split ? (
            <BulkCopyToOther side={side} disabled={!canCopyOther} onCopyTo={onCopyTo} />
          ) : (
            <BulkClip onClip={onClip} />
          )}
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

function BulkCopyToOther({
  side,
  disabled,
  onCopyTo,
}: {
  side: Side;
  disabled: boolean;
  onCopyTo(move: boolean): void;
}) {
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
        title={
          dialog.mode === 'folder'
            ? t('new.folder')
            : dialog.mode === 'file'
              ? t('new.file')
              : t('act.rename')
        }
        label={dialog.mode === 'rename' ? t('newName') : t('name')}
        initial={
          dialog.mode === 'rename'
            ? dialog.entry!.name
            : dialog.mode === 'folder'
              ? t('new.folderDefault')
              : t('new.fileDefault')
        }
        confirm={dialog.mode === 'rename' ? t('act.rename') : t('create')}
        onClose={onClose}
        onSubmit={(name) => onSubmitName(dialog.mode, dialog.side, dialog.entry, name)}
      />
    );
  if (dialog?.kind === 'edit')
    return <EditorDialog entry={dialog.entry} admin={dialog.admin} onClose={onClose} />;
  if (dialog?.kind === 'chown') return <ChownDialog entry={dialog.entry} onClose={onClose} />;
  if (dialog?.kind === 'image')
    return (
      <Dialog
        open
        size="lg"
        onClose={onClose}
        title={dialog.entry.name}
        icon="image"
        footer={<Button onClick={onClose}>{t('edit.close')}</Button>}
      >
        <div className="files-lightbox">
          <img
            src={fileDownloadUrl(dialog.entry.path, dialog.admin, true)}
            alt={dialog.entry.name}
          />
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
      title={
        del
          ? del.trash
            ? t('confirm.foreverTitle', { count: del.entries.length })
            : t('confirm.deleteTitle', { count: del.entries.length })
          : ''
      }
      description={
        del
          ? del.trash
            ? t('confirm.foreverText')
            : strict
              ? t('confirm.adminText')
              : t('confirm.permanentText')
          : ''
      }
      confirmLabel={t('act.deleteForever')}
      confirmText={del && strict && !del.trash ? delNames || t('confirm.word') : undefined}
    >
      {del && del.entries.length > 1 && (
        <p className="files-muted">
          {del.entries
            .slice(0, 5)
            .map((e) => e.name)
            .join(', ')}
          {del.entries.length > 5 ? '…' : ''}
        </p>
      )}
    </ConfirmDialog>
  );
}

function UploadInput({
  inputRef,
  onPick,
}: {
  inputRef: RefObject<HTMLInputElement>;
  onPick(files: File[]): void;
}) {
  return (
    <input
      ref={inputRef}
      type="file"
      multiple
      hidden
      onChange={(e) => {
        const fs = Array.from(e.target.files ?? []);
        e.target.value = '';
        onPick(fs);
      }}
    />
  );
}

export default function FilesPage() {
  const t = useT('files');
  const { session, host, isUnlocked } = useSession();
  const { prefs: accountPrefs } = usePrefs();
  const native = (accountPrefs['shell.mode'] ?? host?.defaultShell) === 'ervisios';
  const [computer, setComputer] = useState(false);
  const prefs = useFilesPrefs(t);
  const home = session?.home || '/';
  const isRoot = !!session?.isRoot;
  const [placesSheet, setPlacesSheet] = useState(false);
  const mobile = useIsMobile();
  const fileInput = useRef<HTMLInputElement>(null);
  const tabs = useFilesTabs({
    home,
    mobile,
    ready: prefs.ready,
    savedTabs: prefs.savedTabs,
    set: prefs.set,
  });
  const { tab, split, side, sel, query, sort, pane, adminOf, updPane, clearSel } = tabs;

  useEffect(() => {
    setUnlockProbe(() => isUnlocked || isRoot);
  }, [isUnlocked, isRoot]);

  const places = usePlaces();
  const { data, selectedEntries } = useFilesPaneData(tabs, prefs.showHidden, prefs.starredList);
  const targets = selectedEntries(side);
  const panel = useFilesPanel(
    targets,
    mobile,
    split,
    native,
    `${tab.id}:${side}:${pane(side).loc}:${adminOf(side)}:${computer}`,
  );
  const actions = useFilesActions({ t, isRoot, tabs, selectedEntries, panel });
  const menu = useFilesMenu({
    t,
    tabs,
    data,
    selectedEntries,
    prefs,
    actions,
    panel,
    fileInput,
    native,
  });
  const goPlace = (loc: string) => {
    setComputer(false);
    tabs.navigate(side, loc);
  };
  const remotes = useRemoteConnections(goPlace, pane(side).loc);
  useFilesPalette({
    t,
    home,
    goPlace,
    newFolder: () => actions.shortcut(side, 'newFolder'),
    toggleSplit: tabs.toggleSplit,
  });

  const otherSide = otherOf(side);
  const inTrash = pane(side).loc === 'trash:';
  const onSelected = (s: Side) => (v: Set<string>) => {
    tabs.setSel((c) => ({ ...c, [s]: v }));
    if (v.size === 0) panel.hide();
  };

  const renderPane = (s: Side) => (
    <Pane
      key={s}
      native={native}
      navigationKey={`${tab.id}:${s}`}
      label={split ? t(s === 'L' ? 'left' : 'right') : undefined}
      active={side === s}
      state={pane(s)}
      data={data(s)}
      query={query[s]}
      onQuery={(q) => {
        tabs.setQuery((c) => ({ ...c, [s]: q }));
        clearSel(s);
      }}
      sort={sort[s]}
      onSort={(v) => tabs.setSort((c) => ({ ...c, [s]: v }))}
      selected={sel[s]}
      onSelected={onSelected(s)}
      starred={prefs.starred}
      onFocus={() => tabs.setAct(s)}
      onNavigate={(l) => tabs.navigate(s, l)}
      onView={(v) => updPane(s, { view: v })}
      onOpen={(e) => actions.openEntry(e, s)}
      onContext={(e, x, y) => menu.openMenu(s, e, x, y)}
      onDropFiles={(dt, dest) => void actions.onDropFiles(dt, dest, s)}
      onDropMove={(paths, dest, copy) => actions.dropMove(paths, dest, copy, s)}
      onShortcut={(k) => actions.shortcut(s, k)}
      onLeaveAdmin={() => updPane(s, { admin: false })}
      onRetryAdmin={() =>
        data(s).error?.code === 'needs_admin' ? updPane(s, { admin: true }) : data(s).reload()
      }
      dragPaths={(e) => (sel[s].has(e.path) ? [...sel[s]] : [e.path])}
    />
  );

  const placesProps = {
    native,
    computer,
    onComputer: () => {
      clearSel();
      setComputer(true);
    },
    data: places,
    home,
    loc: pane(side).loc,
    bookmarks: prefs.bookmarks,
    remotes: remotes.connections,
    onRemote: remotes.open,
    onAddRemote: remotes.add,
    onEditRemote: remotes.edit,
    onForgetRemote: remotes.forget,
    onGo: goPlace,
    onRemoveBookmark: prefs.removeBookmark,
    onNew: menu.newItems(side),
  };

  return (
    <div className={`files-root hue-file${native ? ' files-native' : ''}`}>
      {!mobile && !native && (
        <aside className="files-places" aria-label={t('places')}>
          <PlacesList {...placesProps} />
        </aside>
      )}
      <div className="files-body">
        <TabBar
          native={native}
          tabs={tabs.tabs}
          activeId={tab.id}
          home={home}
          mobile={mobile}
          split={tab.split}
          showHidden={prefs.showHidden}
          onSwitch={(id) => {
            setComputer(false);
            tabs.switchTab(id);
          }}
          onClose={tabs.closeTab}
          onNewTab={() => {
            setComputer(false);
            tabs.openTab();
          }}
          onPlaces={() => setPlacesSheet(true)}
          onToggleHidden={prefs.toggleHidden}
          onToggleSplit={tabs.toggleSplit}
        />
        {native && (
          <NativeFilesCommandBar
            targets={targets}
            writable={!computer && !!tabs.writableLoc(side)}
            trash={inTrash && !computer}
            clipboard={!!actions.clip}
            computer={computer}
            newItems={menu.newItems(side)}
            onShortcut={(key) => actions.shortcut(side, key)}
            onRestore={() => void actions.restore(targets, side)}
            onEmptyTrash={actions.askEmptyTrash}
            onUpload={() => fileInput.current?.click()}
          />
        )}
        <div className={native ? 'files-native-workspace' : 'files-workspace'}>
          {native && !mobile && (
            <aside className="files-places" aria-label={t('places')}>
              <PlacesList {...placesProps} />
            </aside>
          )}
          {native && computer ? (
            <NativeComputer
              places={places}
              home={home}
              bookmarks={prefs.bookmarks}
              onGo={goPlace}
            />
          ) : (
            <div className={`files-duo${split ? ' is-split' : ''}`}>
              {renderPane('L')}
              {split && renderPane('R')}
            </div>
          )}
        </div>
        {native && (
          <div className="files-native-status">
            <span>
              {computer
                ? t('native.thisPC')
                : t('native.itemCount', { count: data(side).entries.length })}
            </span>
            <span>
              {targets.length > 0 ? t('selectedCount', { count: targets.length }) : session?.user}
            </span>
          </div>
        )}
        {targets.length > 0 && !actions.dialog && !actions.del && (
          <BulkBar
            targets={targets}
            side={side}
            split={split}
            inTrash={inTrash}
            canCopyOther={split && !!tabs.writableLoc(otherSide)}
            onRestore={() => void actions.restore(targets, side)}
            onDownload={() => actions.download(targets)}
            onCopyTo={(move) => actions.copyTo(targets, side, otherSide, move)}
            onClip={(k) => actions.shortcut(side, k)}
            onDelete={() => actions.requestDelete(targets, side)}
            onClear={() => clearSel(side)}
          />
        )}
        <TransferBar />
      </div>
      {panel.isOpen && (
        <SidePanel
          native={native}
          entries={panel.entries}
          admin={adminOf(side)}
          inline={!split && !mobile}
          inTrash={inTrash}
          starred={panel.entries.length === 1 && prefs.starred.has(panel.entries[0].path)}
          canChown={!!session?.canSudo && !isRemoteLocation(pane(side).loc)}
          onClose={panel.dismiss}
          onEdit={(e) => actions.openEditor(e, side)}
          onDownload={(es) => actions.download(es)}
          onCopyPath={(e) => void copyText(e.path, t)}
          onTrash={(es) => actions.requestDelete(es, side)}
          onRename={(e) => actions.setDialog({ kind: 'name', mode: 'rename', entry: e, side })}
          onStar={(e) => void prefs.toggleStar(e)}
          onChown={(e) => actions.setDialog({ kind: 'chown', entry: e })}
        />
      )}
      {(mobile || native) && (
        <Sheet open={placesSheet} onClose={() => setPlacesSheet(false)} title={t('places')}>
          <div className="files-places files-places--sheet">
            <PlacesList {...placesProps} onPicked={() => setPlacesSheet(false)} />
          </div>
        </Sheet>
      )}
      <UploadInput inputRef={fileInput} onPick={actions.uploadPicked} />
      {remotes.dialogs}
      {menu.menu && (
        <Menu
          items={menu.menu.items}
          anchor={{ x: menu.menu.x, y: menu.menu.y }}
          onClose={menu.closeMenu}
          aria-label={t('menu')}
        />
      )}
      <FileDialog
        dialog={actions.dialog}
        onClose={actions.closeDialog}
        onSubmitName={actions.submitName}
      />
      <DeleteConfirm
        del={actions.del}
        strict={actions.isDeleteStrict}
        onClose={actions.closeDelete}
        onConfirm={actions.confirmDelete}
      />
      <ConfirmDialog
        open={actions.emptyTrash}
        onClose={actions.closeEmptyTrash}
        onConfirm={actions.doEmptyTrash}
        title={t('confirm.emptyTitle')}
        description={t('confirm.emptyText', { count: data(side).entries.length })}
        confirmLabel={t('act.emptyTrash')}
      />
    </div>
  );
}
