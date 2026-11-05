import { useState } from 'react';
import type { TFn } from '../../i18n';
import { toast } from '../../ui';
import { emitChanged, fcall } from './fapi';
import type { BatchFailure, DeleteRequest, Side } from './filesPageTypes';
import { fail, toastFailed, undoTrash } from './filesOps';
import type { FEntry } from './types';
import type { FilesTabs } from './useFilesTabs';
import { isRemoteLocation } from './remoteLocation';

/** Trash, permanent delete (behind a confirmation), restore and empty trash. */
export function useFilesDelete(t: TFn, isRoot: boolean, tabs: FilesTabs) {
  const { pane, adminOf, clearSel, side } = tabs;
  const [del, setDel] = useState<DeleteRequest | null>(null);
  const [emptyTrash, setEmptyTrash] = useState(false);

  const doTrash = async (es: FEntry[], s: Side) => {
    const paths = es.map((e) => e.path);
    try {
      const r = await fcall<{ deleted: string[]; failed: BatchFailure[] }>(
        'files.delete',
        { paths, trash: true },
        { admin: adminOf(s) },
      );
      clearSel(s);
      emitChanged();
      if ((r.deleted ?? []).length)
        toast.undo(
          t('trashed', { count: (r.deleted ?? []).length }),
          t('undo'),
          () => void undoTrash(r.deleted, t),
        );
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
    if (inTrash || permanent || isRemoteLocation(pane(s).loc) || pane(s).admin || isRoot)
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
    isDeleteStrict: !!del && (pane(del.side).admin || isRoot),
    emptyTrash,
    askEmptyTrash: () => setEmptyTrash(true),
    closeEmptyTrash: () => setEmptyTrash(false),
    doEmptyTrash,
    requestDelete,
    restore,
  };
}
