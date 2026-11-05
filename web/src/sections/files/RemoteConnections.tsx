import { useCallback, useEffect, useState } from 'react';
import { ApiError, call, errorMessage } from '../../api';
import { useT } from '../../i18n';
import { useWindowActivity } from '../../lib/windowActivity';
import { Button, ConfirmDialog, Dialog, Input, Select, toast } from '../../ui';
import { remoteLocation, remoteTarget } from './remoteLocation';
import { networkAddress, parseNetworkPath } from './networkPath';
import type { Remote } from './types';
import './remote-files.css';

export function useRemoteConnections(onOpen: (location: string) => void, activeLocation = '') {
  const t = useT('files');
  const active = useWindowActivity();
  const [connections, setConnections] = useState<Remote[]>([]);
  const [editing, setEditing] = useState<Remote | 'new' | null>(null);
  const [forgetting, setForgetting] = useState<Remote | null>(null);
  const refresh = useCallback(() => {
    void call<{ connections: Remote[] }>('remote.list', {}, { noUnlock: true }).then(
      (r) => setConnections(r.connections),
      () => undefined,
    );
  }, []);
  useEffect(() => {
    if (active) refresh();
  }, [active, refresh]);
  const open = async (remote: Remote, openIn = onOpen) => {
    try {
      const r = await call<{ path: string }>(
        'remote.start',
        { conn: remote.id },
        { noUnlock: true },
      );
      openIn(remoteLocation(remote.id, r.path));
    } catch (error) {
      if (
        error instanceof ApiError &&
        error.data &&
        typeof error.data === 'object' &&
        'reason' in error.data &&
        error.data.reason === 'hostKey'
      )
        setEditing(remote);
      toast.err(t('remote.connectFailed'), errorMessage(error));
    }
  };
  return {
    connections,
    refresh,
    open,
    add: () => setEditing('new'),
    edit: setEditing,
    forget: setForgetting,
    dialogs: (
      <>
        {editing && (
          <RemoteConnectionDialog
            connection={editing === 'new' ? undefined : editing}
            onClose={() => setEditing(null)}
            onSaved={(remote) => {
              refresh();
              setEditing(null);
              void open(remote);
            }}
          />
        )}
        <ConfirmDialog
          open={!!forgetting}
          title={t('remote.forgetTitle', { name: forgetting?.name ?? '' })}
          description={t('remote.forgetHint')}
          confirmLabel={t('remote.forget')}
          onClose={() => setForgetting(null)}
          onConfirm={async () => {
            if (!forgetting) return;
            await call('remote.forget', { id: forgetting.id }, { noUnlock: true });
            if (remoteTarget(activeLocation)?.conn === forgetting.id) onOpen('home:');
            setForgetting(null);
            refresh();
          }}
        />
      </>
    ),
  };
}

interface TestResult {
  ok: boolean;
  path?: string;
  needsTrust?: boolean;
  changed?: boolean;
  fingerprint?: string;
  hostKey?: string;
  keyType?: string;
}

function RemoteConnectionDialog({
  connection,
  onClose,
  onSaved,
}: {
  connection?: Remote;
  onClose(): void;
  onSaved(remote: Remote): void;
}) {
  const t = useT('files');
  const [name, setName] = useState(connection?.name ?? '');
  const [kind, setKind] = useState(connection?.kind === 'smb' ? 'smb' : 'sftp');
  const [address, setAddress] = useState(
    connection?.kind === 'smb'
      ? networkAddress(
          connection.host ?? '',
          connection.share ?? '',
          connection.path,
          connection.port,
        )
      : '',
  );
  const [domain, setDomain] = useState(connection?.domain ?? '');
  const [host, setHost] = useState(connection?.host ?? '');
  const [port, setPort] = useState(String(connection?.port ?? 22));
  const [user, setUser] = useState(connection?.user ?? '');
  const [path, setPath] = useState(connection?.path ?? '');
  const [auth, setAuth] = useState(connection?.auth ?? 'password');
  const [password, setPassword] = useState('');
  const [key, setKey] = useState('');
  const [passphrase, setPassphrase] = useState('');
  const [hostKey, setHostKey] = useState(connection?.hostKey ?? '');
  const [trust, setTrust] = useState<TestResult | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [tested, setTested] = useState(false);
  const invalidate = () => {
    setTested(false);
    setTrust(null);
    setError('');
  };
  const spec = (pin = hostKey) => {
    const network = kind === 'smb' ? parseNetworkPath(address) : null;
    if (kind === 'smb' && !network) throw new Error(t('remote.badNetworkPath'));
    return {
      ...(connection && connection.kind === kind ? { id: connection.id } : {}),
      name,
      kind,
      ...(network ?? { host, port: Number(port), path }),
      user,
      ...(kind === 'smb' ? { domain } : { auth, hostKey: pin }),
      ...(password ? { password } : {}),
      ...(kind === 'sftp' && key ? { key } : {}),
      ...(kind === 'sftp' && (passphrase || key) ? { passphrase } : {}),
    };
  };
  const test = async (pin = hostKey, save = false) => {
    setBusy(true);
    setError('');
    setTrust(null);
    try {
      const result = await call<TestResult>('remote.test', spec(pin), { noUnlock: true });
      if (result.needsTrust) {
        setTrust(result);
        setTested(false);
        return;
      }
      if (!result.ok) throw new Error(t('remote.connectFailed'));
      setTested(true);
      if (save) {
        const remote = await call<Remote>('remote.save', spec(pin), { noUnlock: true });
        // Clear secrets before handing the saved, redacted connection to the page.
        setPassword('');
        setKey('');
        setPassphrase('');
        onSaved(remote);
      }
    } catch (e) {
      setError(errorMessage(e));
      setTested(false);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      title={t(connection ? 'remote.edit' : 'remote.add')}
      icon="server"
      onClose={onClose}
      className="files-remote-dialog hue-file"
      onSubmit={(e) => {
        e.preventDefault();
        void test(hostKey, true);
      }}
      description={t('remote.description')}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            {t('cancel')}
          </Button>
          <Button icon="refresh" loading={busy} onClick={() => void test()}>
            {t('remote.test')}
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !!trust}>
            {t('remote.saveConnect')}
          </Button>
        </>
      }
    >
      <div className="files-remote-form">
        <Select
          label={t('remote.protocol')}
          value={kind}
          options={[
            { value: 'sftp', label: t('remote.sftp') },
            { value: 'smb', label: t('remote.smb') },
          ]}
          onChange={(value) => {
            setKind(value);
            setPassword('');
            setKey('');
            setPassphrase('');
            setHostKey('');
            invalidate();
          }}
        />
        <Input
          label={t('remote.name')}
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            invalidate();
          }}
          maxLength={64}
          autoComplete="off"
        />
        {kind === 'smb' ? (
          <Input
            required
            label={t('remote.networkPath')}
            value={address}
            placeholder="\\\\server\\share"
            hint={t('remote.networkPathHint')}
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => {
              setAddress(e.target.value);
              invalidate();
            }}
          />
        ) : (
          <div className="files-remote-row">
            <Input
              required
              label={t('remote.host')}
              value={host}
              onChange={(e) => {
                setHost(e.target.value);
                setHostKey('');
                invalidate();
              }}
              autoComplete="off"
              spellCheck={false}
            />
            <Input
              required
              label={t('remote.port')}
              type="number"
              min={1}
              max={65535}
              value={port}
              onChange={(e) => {
                setPort(e.target.value);
                setHostKey('');
                invalidate();
              }}
            />
          </div>
        )}
        <Input
          required
          label={t('remote.user')}
          value={user}
          onChange={(e) => {
            setUser(e.target.value);
            invalidate();
          }}
          autoComplete="off"
          spellCheck={false}
        />
        {kind === 'sftp' && (
          <Select
            label={t('remote.auth')}
            value={auth}
            onChange={(value) => {
              setAuth(value);
              invalidate();
            }}
            options={[
              { value: 'password', label: t('remote.password') },
              { value: 'key', label: t('remote.privateKey') },
            ]}
          />
        )}
        {kind === 'smb' || auth === 'password' ? (
          <Input
            type="password"
            label={t('remote.password')}
            value={password}
            hint={connection?.hasPassword ? t('remote.keepSecret') : undefined}
            autoComplete="new-password"
            onChange={(e) => {
              setPassword(e.target.value);
              invalidate();
            }}
          />
        ) : (
          <>
            <Input
              type="file"
              label={t('remote.privateKey')}
              hint={connection?.hasKey ? t('remote.keepKey') : t('remote.keyHint')}
              onChange={async (e) => {
                const file = e.target.files?.[0];
                if (!file) return;
                if (file.size > 65536) {
                  setError(t('remote.keyTooLarge'));
                  return;
                }
                setKey(await file.text());
                invalidate();
              }}
            />
            <Input
              type="password"
              label={t('remote.passphrase')}
              value={passphrase}
              autoComplete="new-password"
              hint={connection?.hasPassphrase ? t('remote.keepSecret') : undefined}
              onChange={(e) => {
                setPassphrase(e.target.value);
                invalidate();
              }}
            />
          </>
        )}
        {kind === 'smb' ? (
          <Input
            label={t('remote.domain')}
            value={domain}
            autoComplete="off"
            onChange={(e) => {
              setDomain(e.target.value);
              invalidate();
            }}
          />
        ) : (
          <Input
            label={t('remote.startPath')}
            placeholder={t('remote.loginFolder')}
            value={path}
            spellCheck={false}
            onChange={(e) => {
              setPath(e.target.value);
              invalidate();
            }}
          />
        )}
        {kind === 'sftp' && connection?.fingerprint && !trust && (
          <p className="files-remote-fingerprint">
            <b>{t('remote.trustedKey')}</b>
            <code>{connection.fingerprint}</code>
          </p>
        )}
        {trust && (
          <section className="files-remote-trust" role="alert">
            <b>{t(trust.changed ? 'remote.keyChanged' : 'remote.trustTitle')}</b>
            <p>{t(trust.changed ? 'remote.keyChangedHint' : 'remote.trustHint')}</p>
            <code>
              {trust.keyType} · {trust.fingerprint}
            </code>
            <Button
              icon="lock"
              onClick={() => {
                const pin = trust.hostKey!;
                setHostKey(pin);
                void test(pin);
              }}
            >
              {t('remote.trust')}
            </Button>
          </section>
        )}
        {tested && (
          <p role="status" className="files-remote-success">
            {t('remote.testOk')}
          </p>
        )}
        {error && (
          <p role="alert" className="files-remote-error">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}
