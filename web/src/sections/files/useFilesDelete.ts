import { useRef, useState } from 'react';
import type { TFn } from '../../i18n';
import { toast } from '../../ui';
import { emitChanged, fcall } from './fapi';
import type { BatchFailure, DeleteRequest, Side } from './filesPageTypes';
import { fail, toastFailed, undoTrash, type TrashReceipt } from './filesOps';
import type { FEntry } from './types';
import type { FilesTabs } from './useFilesTabs';
import { isRemoteLocation } from './remoteLocation';

/** Trash, permanent delete (behind a confirmation), restore and empty trash. */
export function useFilesDelete(t: TFn, _isRoot: boolean, tabs: FilesTabs) {
  const { pane, adminOf, clearSel, side, elevate } = tabs;
  const undoStack = useRef<TrashReceipt[]>([]);
  const undoBusy = useRef(false);
  const undo = async (receipt = undoStack.current.at(-1)) => {
    if (!receipt || undoBusy.current || !undoStack.current.includes(receipt)) return;
    undoBusy.current = true;
    try {
      if (await undoTrash(receipt, t))
        undoStack.current = undoStack.current.filter((item) => item !== receipt);
    } finally {
      undoBusy.current = false;
    }
  };
  const [del, setDel] = useState<DeleteRequest | null>(null);
  const [emptyTrash, setEmptyTrash] = useState(false);

  const doTrash = async (es: FEntry[], s: Side) => {
    const paths = es.map((e) => e.path);
    let admin = adminOf(s);
    try {
      const r = await fcall<{
        deleted: string[];
        failed: BatchFailure[];
        trashed: { id: string; path: string }[];
      }>(
        'files.delete',
        { paths, trash: true },
        {
          admin,
          onElevate: () => {
            admin = true;
            elevate(s)();
          },
        },
      );
      clearSel(s);
      emitChanged();
      if ((r.trashed ?? []).length) {
        const receipt: TrashReceipt = { ids: r.trashed.map((item) => item.id), admin };
        undoStack.current = [...undoStack.current.slice(-19), receipt];
        receipt.toastId = toast.undo(
          t('trashed', { count: (r.deleted ?? []).length }),
          t('undo'),
          () => void undo(receipt),
        );
      }
      toastFailed(r.failed, (count) => t('err.someNotDeleted', { count }));
    } catch (x) {
      fail(t('err.trash'), x);
    }
  };

  const doPermanent = async (es: FEntry[], s: Side, fromTrash: boolean) => {
    try {
      let deleted = es.length;
      if (fromTrash)
        await fcall(
          'files.trashDelete',
          { ids: es.map((e) => e.trashId).filter(Boolean) },
          { admin: adminOf(s) },
        );
      else {
        const result = await fcall<{ deleted: string[]; failed: BatchFailure[] }>(
          'files.delete',
          { paths: es.map((e) => e.path), trash: false },
          { admin: adminOf(s) },
        );
        deleted = result.deleted?.length ?? 0;
        if (result.failed?.length) {
          emitChanged();
          throw new Error(
            `${t('err.someNotDeleted', { count: result.failed.length })}: ${result.failed[0].message}`,
          );
        }
      }
      clearSel(s);
      emitChanged();
      if (deleted) toast.ok(t('deleted', { count: deleted }));
    } catch (x) {
      fail(t('err.delete'), x);
      throw x;
    }
  };

  const requestDelete = (es: FEntry[], s: Side, permanent = false) => {
    if (es.length === 0) return;
    const inTrash = pane(s).loc === 'trash:';
    if (inTrash || permanent || isRemoteLocation(pane(s).loc))
      setDel({ entries: es, side: s, trash: inTrash });
    else void doTrash(es, s);
  };

  const restore = async (es: FEntry[], s: Side) => {
    try {
      const r = await fcall<{ restored: string[]; failed: BatchFailure[] }>(
        'files.restore',
        { ids: es.map((e) => e.trashId).filter(Boolean) },
        { admin: adminOf(s) },
      );
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

  return {
    del,
    closeDelete: () => setDel(null),
    confirmDelete: () => doPermanent(del!.entries, del!.side, del!.trash),
    isDeleteStrict: false,
    undo,
    emptyTrash,
    askEmptyTrash: () => setEmptyTrash(true),
    closeEmptyTrash: () => setEmptyTrash(false),
    doEmptyTrash,
    requestDelete,
    restore,
  };
}
