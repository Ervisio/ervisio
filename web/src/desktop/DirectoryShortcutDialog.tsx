import { useEffect, useState } from 'react';
import { call, errorMessage } from '../api';
import { useT } from '../i18n';
import { Button, Dialog, Input, Select } from '../ui';
import { fcall } from '../sections/files/fapi';
import { isDirLike, type FEntry, type Remote } from '../sections/files/types';
import { remoteLocation, remoteTarget } from '../sections/files/remoteLocation';
import { validDirectoryPath, type DesktopDirectoryShortcut } from './model';

export function DirectoryShortcutDialog({
  initial,
  home,
  onClose,
  onSave,
}: {
  initial?: DesktopDirectoryShortcut;
  home: string;
  onClose(): void;
  onSave(value: { name: string; path: string }): void;
}) {
  const t = useT('shell');
  const remote = initial ? remoteTarget(initial.path) : null;
  const [connection, setConnection] = useState(remote?.conn ?? '');
  const [path, setPath] = useState(remote?.path ?? initial?.path ?? home);
  const [name, setName] = useState(initial?.name ?? '');
  const [connections, setConnections] = useState<Remote[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    void call<{ connections: Remote[] }>(
      'remote.list',
      {},
      { noUnlock: true, signal: controller.signal },
    ).then(
      (result) => setConnections(result.connections),
      () => undefined,
    );
    return () => controller.abort();
  }, []);
  const save = async () => {
    if (busy) return;
    setError('');
    let location: string;
    try {
      location = connection ? remoteLocation(connection, path.trim()) : path.trim();
      if (!validDirectoryPath(location)) throw new Error(t('desktop.invalidDirectoryShortcut'));
    } catch {
      setError(t('desktop.invalidDirectoryShortcut'));
      return;
    }
    setBusy(true);
    try {
      // Validate through the same permission-aware local/remote adapter as Explorer.
      const entry = await fcall<FEntry>('files.stat', { path: location });
      if (!isDirLike(entry)) throw new Error(t('desktop.invalidDirectoryShortcut'));
      const label = name.trim() || entry.name || location;
      onSave({
        name: label.slice(0, 80),
        path: validDirectoryPath(entry.path) ? entry.path : location,
      });
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      onClose={() => {
        if (!busy) onClose();
      }}
      title={t(initial ? 'desktop.editDirectoryShortcut' : 'desktop.newDirectoryShortcut')}
      description={t('desktop.directoryShortcutHint')}
      icon="files"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
      footer={
        <>
          <Button disabled={busy} onClick={onClose}>
            {t('desktop.cancel')}
          </Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!path.trim()}>
            {t('desktop.save')}
          </Button>
        </>
      }
    >
      <div className="eos-shortcut-fields">
        <Select
          disabled={busy}
          label={t('desktop.shortcutConnection')}
          value={connection}
          options={[
            { value: '', label: t('desktop.shortcutLocal') },
            ...(remote && !connections.some((item) => item.id === remote.conn)
              ? [{ value: remote.conn, label: t('desktop.shortcutUnavailable') }]
              : []),
            ...connections.map((item) => ({ value: item.id, label: item.name })),
          ]}
          onChange={(value) => {
            setConnection(value);
            setPath(value ? '/' : home);
          }}
        />
        <Input
          data-autofocus
          disabled={busy}
          label={t('desktop.shortcutPath')}
          value={path}
          onChange={(event) => setPath(event.target.value)}
          maxLength={4096}
          mono
          error={error || undefined}
        />
        <Input
          disabled={busy}
          label={t('desktop.shortcutName')}
          value={name}
          onChange={(event) => setName(event.target.value)}
          maxLength={80}
        />
      </div>
    </Dialog>
  );
}
