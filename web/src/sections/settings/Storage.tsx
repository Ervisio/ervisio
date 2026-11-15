import { useT } from '../../i18n';
import { Badge, Button, IconButton, toast } from '../../ui';
import { useRemoteConnections } from '../files/RemoteConnections';
import type { Remote } from '../files/types';
import './envs.css';

const address = (r: Remote) =>
  r.kind === 's3' ? r.bucket : r.kind === 'smb' ? `${r.host}/${r.share}` : r.host;

/** Settings > External storage: the saved connections Files lists under Remote. */
export function StorageBlock() {
  const t = useT('settings');
  const tf = useT('files');
  const remotes = useRemoteConnections(() => toast.ok(t('storage.saved')));
  return (
    <>
      {remotes.connections.length === 0 && (
        <div className="env-empty">
          <small>{t('storage.none')}</small>
        </div>
      )}
      {remotes.connections.map((r) => (
        <div className="st-host" key={r.id}>
          <div className="grow">
            <b>{r.name}</b>
            <small className="mono">{address(r)}</small>
          </div>
          <Badge>{r.kind.toUpperCase()}</Badge>
          <IconButton
            icon="edit"
            label={tf('remote.editNamed', { name: r.name })}
            onClick={() => remotes.edit(r)}
          />
          <IconButton
            icon="trash"
            label={tf('remote.forgetNamed', { name: r.name })}
            onClick={() => remotes.forget(r)}
          />
        </div>
      ))}
      <Button icon="plus" onClick={remotes.add}>
        {tf('remote.add')}
      </Button>
      {remotes.dialogs}
    </>
  );
}
