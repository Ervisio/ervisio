import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import type { TFn } from '../../i18n';
import { toast } from '../../ui';
import { collectDropped } from './dropfiles';
import { emitChanged } from './fapi';
import type { DialogState, NameMode, Side } from './filesPageTypes';
import { downloadEntries, fail, saveName, uploadInto } from './filesOps';
import { canInlineImage, kindOf, looksTextual } from './kinds';
import type { Shortcut } from './Pane';
import { isDirLike, type FEntry } from './types';
import { dirname, isVirtual } from './util';
import { useFilesClipboard } from './useFilesClipboard';
import { useFilesDelete } from './useFilesDelete';
import type { FilesPanel } from './useFilesPanel';
import type { FilesTabs } from './useFilesTabs';
import { isRemoteLocation } from './remoteLocation';
import { preferredEditor, type EditorMode } from '../editor/document';

const MAX_EDIT_SIZE = 2 << 20;

type UploadItem = { file: File; rel: string };

interface ActionsArgs {
  t: TFn;
  isRoot: boolean;
  tabs: FilesTabs;
  selectedEntries(s: Side): FEntry[];
  panel: FilesPanel;
}

/** Everything the user can do to entries: open, edit, upload, download, clipboard, delete, shortcuts. */
export function useFilesActions({ t, isRoot, tabs, selectedEntries, panel }: ActionsArgs) {
  const route = useNavigate();
  const { side, pane, adminOf, elevate, writableLoc, navigate } = tabs;
  const [dialog, setDialog] = useState<DialogState | null>(null);
  const clipboard = useFilesClipboard(t, tabs);
  const removal = useFilesDelete(t, isRoot, tabs);

  const refreshAll = () => emitChanged();
  const download = (es: FEntry[], s: Side = side) => downloadEntries(es, adminOf(s), t);

  const submitName = async (mode: NameMode, s: Side, entry: FEntry | undefined, name: string) => {
    const dir = mode === 'rename' ? dirname(entry!.path) : writableLoc(s);
    if (!dir) return;
    await saveName({ mode, dir, entry, name, admin: adminOf(s) }, t);
  };

  const uploadFiles = (items: UploadItem[], dest: string, s: Side) =>
    uploadInto(items, dest, adminOf(s), elevate(s), t);
  const onDropFiles = async (dt: DataTransfer, dest: string, s: Side) => {
    try {
      const items = await collectDropped(dt);
      if (items.length === 0) return toast.info(t('err.nothingDropped'));
      await uploadFiles(items, dest, s);
    } catch (x) {
      fail(t('err.upload'), x);
    }
  };
  const uploadPicked = (files: File[]) => {
    const dest = writableLoc(side);
    if (dest && files.length)
      void uploadFiles(
        files.map((f) => ({ file: f, rel: f.name })),
        dest,
        side,
      );
  };

  const launch = (to: string) => {
    window.dispatchEvent(
      new CustomEvent('ervisio:shell-action', { detail: { action: 'open-app', to } }),
    );
    route(to);
  };
  const openEditor = (e: FEntry, s: Side, mode: EditorMode = preferredEditor(e.name)) => {
    const path = mode === 'code' ? '/editor' : '/notepad';
    launch(
      `${path}?path=${encodeURIComponent(e.path)}${adminOf(s) && !isRemoteLocation(e.path) ? '&admin=1' : ''}`,
    );
  };
  const openTerminal = (path: string, s: Side) => {
    if (isVirtual(path) || isRemoteLocation(path)) return;
    launch(
      `/terminal?cwd=${encodeURIComponent(path)}${adminOf(s) ? '&admin=1' : ''}&launch=${crypto.randomUUID()}`,
    );
  };
  const openEntry = (e: FEntry, s: Side) => {
    if (pane(s).loc === 'trash:') return;
    if (isDirLike(e)) return navigate(s, e.path);
    const admin = adminOf(s);
    if (kindOf(e) === 'img' && canInlineImage(e.name))
      return setDialog({ kind: 'image', entry: e, admin });
    if (looksTextual(e) && e.size <= MAX_EDIT_SIZE) return openEditor(e, s);
    download([e], s);
  };

  const shortcut = (s: Side, k: Shortcut) => {
    const es = selectedEntries(s);
    const loc = pane(s).loc;
    switch (k) {
      case 'delete':
        return removal.requestDelete(es, s);
      case 'permDelete':
        return removal.requestDelete(es, s, true);
      case 'rename':
        if (es.length === 1 && loc !== 'trash:')
          setDialog({ kind: 'name', mode: 'rename', entry: es[0], side: s });
        return;
      case 'copy':
      case 'cut':
        if (es.length && loc !== 'trash:') clipboard.clipSelection(es, k === 'cut');
        return;
      case 'paste':
        return clipboard.paste(s);
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
        return panel.show();
    }
  };

  return {
    ...clipboard,
    ...removal,
    dialog,
    setDialog,
    closeDialog: () => setDialog(null),
    refreshAll,
    download,
    submitName,
    onDropFiles,
    uploadPicked,
    openEntry,
    openEditor,
    openTerminal,
    shortcut,
  };
}

export type FilesActions = ReturnType<typeof useFilesActions>;
