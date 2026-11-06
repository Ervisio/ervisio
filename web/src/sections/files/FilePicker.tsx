import { useEffect, useState } from 'react';
import { call, errorMessage, useSession } from '../../api';
import { useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import { Button, Dialog, Icon, Input } from '../../ui';
import { fcall } from './fapi';
import { dirname, join, validFileName, withPath } from './util';
import { isDirLike, type FEntry, type RawEntry } from './types';
import { remoteLocation } from './remoteLocation';
import './file-picker.css';

/** Shared server-side chooser; callers retain validation and overwrite confirmation. */
export function FilePicker({
  mode,
  title,
  initialDirectory,
  initialName = '',
  admin = false,
  busy = false,
  error = '',
  onChoose,
  onCancel,
}: {
  mode: 'file' | 'folder' | 'save';
  title: string;
  initialDirectory: string;
  initialName?: string;
  admin?: boolean;
  busy?: boolean;
  error?: string;
  onChoose(path: string): void;
  onCancel(): void;
}) {
  const t = useT('files');
  const { session } = useSession();
  const [directory, setDirectory] = useState(initialDirectory || session?.home || '/');
  const [address, setAddress] = useState(directory);
  const [name, setName] = useState(initialName);
  const [selected, setSelected] = useState<FEntry | null>(null);
  const [entries, setEntries] = useState<FEntry[]>([]);
  const [query, setQuery] = useState('');
  const [hidden, setHidden] = useState(false);
  const [loading, setLoading] = useState(true);
  const [failure, setFailure] = useState('');
  const [warning, setWarning] = useState('');
  const [remotes, setRemotes] = useState<{ id: string; name: string; path?: string }[]>([]);
  useEffect(() => {
    const controller = new AbortController();
    void call<{ connections: typeof remotes }>(
      'remote.list',
      {},
      { noUnlock: true, signal: controller.signal },
    ).then(
      (value) => setRemotes(value.connections),
      () => {},
    );
    return () => controller.abort();
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setFailure('');
    setWarning('');
    setSelected(null);
    setAddress(directory);
    setQuery('');
    void fcall<{ path: string; entries: RawEntry[]; truncated?: boolean }>(
      'files.list',
      { path: directory, showHidden: hidden },
      { admin, signal: controller.signal },
    )
      .then(
        (value) => {
          if (controller.signal.aborted) return;
          setEntries(value.entries.map((entry) => withPath(directory, entry)));
        if (value.truncated) setWarning(t('listTruncated'));
        },
        (e) => {
          if (!controller.signal.aborted) {
            setEntries([]);
            setFailure(errorMessage(e));
          }
        },
      )
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [directory, hidden, admin, t]);
  const target =
    mode === 'save'
      ? validFileName(name.trim())
        ? join(directory, name.trim())
        : ''
      : mode === 'folder'
        ? selected && isDirLike(selected)
          ? selected.path
          : directory
        : selected && !isDirLike(selected)
          ? selected.path
          : '';
  const choose = (entry: FEntry) => {
    setSelected(entry);
    if (mode === 'save' && !isDirLike(entry)) setName(entry.name);
  };
  const rows = entries
    .filter((entry) => entry.name.toLocaleLowerCase().includes(query.toLocaleLowerCase()))
    .sort(
      (a, b) =>
        Number(isDirLike(b)) - Number(isDirLike(a)) ||
        a.name.localeCompare(b.name, undefined, { numeric: true }),
    );
  return (
    <Dialog
      open
      title={title}
      icon="files"
      className="file-picker"
      onClose={() => !busy && onCancel()}
      onSubmit={(event) => {
        event.preventDefault();
        if (target && !loading && !failure && !busy) onChoose(target);
      }}
      footer={
        <>
          <span className="file-picker-target" title={target}>
            {target}
          </span>
          <Button disabled={busy} onClick={onCancel}>
            {t('cancel')}
          </Button>
          <Button
            type="submit"
            variant="primary"
            loading={busy}
            disabled={!target || loading || !!failure}
          >
            {t(mode === 'save' ? 'save' : 'picker.choose')}
          </Button>
        </>
      }
    >
      <div className="file-picker-address">
        <Button
          icon="chevronup"
          aria-label={t('picker.up')}
          disabled={busy || dirname(directory) === directory}
          onClick={() => setDirectory(dirname(directory))}
        />
        <Input
          aria-label={t('picker.location')}
          value={address}
          onChange={(event) => setAddress(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault();
              setDirectory(address.trim());
            }
          }}
        />
        <Button disabled={busy} onClick={() => setDirectory(address.trim())}>
          {t('picker.go')}
        </Button>
      </div>
      <div className="file-picker-body">
        <nav aria-label={t('places')}>
          <button type="button" onClick={() => setDirectory(session?.home || '/')}>
            <Icon name="home" />
            {t('place.home')}
          </button>
          <button type="button" onClick={() => setDirectory('/')}>
            <Icon name="server" />
            {t('place.root')}
          </button>
          {remotes.map((remote) => (
            <button
              type="button"
              key={remote.id}
              title={remote.name}
              onClick={() => setDirectory(remoteLocation(remote.id, remote.path || '/'))}
            >
              <Icon name="server" />
              {remote.name}
            </button>
          ))}
        </nav>
        <div className="file-picker-main">
          <Input
            icon="search"
            aria-label={t('picker.filter')}
            placeholder={t('picker.filter')}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <div
            className="file-picker-list"
            role="listbox"
            aria-label={t('picker.entries')}
            aria-busy={loading}
          >
            {loading ? (
              <p>{t('loading')}</p>
            ) : (
              rows.map((entry) => (
                <div
                  key={entry.path}
                  role="option"
                  aria-selected={selected?.path === entry.path}
                  tabIndex={0}
                  className={`file-picker-row${selected?.path === entry.path ? ' is-selected' : ''}`}
                  onClick={() => choose(entry)}
                  onDoubleClick={() => {
                if (busy || loading) return;
                    if (isDirLike(entry)) setDirectory(entry.path);
                    else if (mode !== 'folder') onChoose(entry.path);
                  }}
                  onKeyDown={(event) => {
                    if (event.key === ' ' || event.key === 'Enter') {
                      event.preventDefault();
                      if (event.key === 'Enter' && isDirLike(entry)) setDirectory(entry.path);
                      else choose(entry);
                    }
                  }}
                >
                  <Icon name={isDirLike(entry) ? 'files' : 'file'} />
                  <span>{entry.name}</span>
                  <small>{isDirLike(entry) ? t('folders') : formatBytes(entry.size)}</small>
                </div>
              ))
            )}
            {!loading && !failure && !rows.length && <p>{t('picker.noEntries')}</p>}
          </div>
        </div>
      </div>
      <div className="file-picker-bottom">
        <label>
          <input
            type="checkbox"
            checked={hidden}
            onChange={(event) => setHidden(event.target.checked)}
          />
          {t('showHidden')}
        </label>
        {mode === 'save' && (
          <Input
            label={t('name')}
            value={name}
            autoFocus
            onChange={(event) => setName(event.target.value)}
          />
        )}
      </div>
      {(failure || error) && (
        <p role="alert" className="file-picker-error">
          {failure || error}
        </p>
      )}
      {warning && <p role="status">{warning}</p>}
    </Dialog>
  );
}
