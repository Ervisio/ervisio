import { useCallback, useEffect, useState } from 'react';
import { ApiError, call, errorMessage } from '../../api';
import { useT } from '../../i18n';
import { useWindowActivity } from '../../lib/windowActivity';
import { Button, ConfirmDialog, Dialog, Input, Select, Switch, toast } from '../../ui';
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

const BOX_HOST = '.your-storagebox.de';
// A Storage Box is plain SFTP on port 23, at a host named after the user.
const isStorageBox = (c?: Remote) =>
  c?.kind === 'sftp' && c.port === 23 && c.host === c.user + BOX_HOST;

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
  const [kind, setKind] = useState(
    isStorageBox(connection)
      ? 'storagebox'
      : connection?.kind === 'smb' || connection?.kind === 's3'
        ? connection.kind
        : 'sftp',
  );
  const box = kind === 'storagebox';
  const proto = box ? 'sftp' : kind;
  const [endpoint, setEndpoint] = useState(connection?.url ?? '');
  const [bucket, setBucket] = useState(connection?.bucket ?? '');
  const [region, setRegion] = useState(connection?.region ?? '');
  const [pathStyle, setPathStyle] = useState(connection?.pathStyle ?? false);
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
    const place =
      network ??
      (box
        ? { host: user.trim() + BOX_HOST, port: 23, path }
        : kind === 's3'
          ? { url: endpoint, bucket, region, pathStyle, path }
          : { host, port: Number(port), path });
    return {
      ...(connection && connection.kind === proto ? { id: connection.id } : {}),
      name,
      kind: proto,
      ...place,
      user,
      ...(kind === 'smb' ? { domain } : {}),
      ...(proto === 'sftp' ? { auth, hostKey: pin } : {}),
      ...(password ? { password } : {}),
      ...(proto === 'sftp' && key ? { key } : {}),
      ...(proto === 'sftp' && (passphrase || key) ? { passphrase } : {}),
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
            { value: 's3', label: t('remote.s3') },
            { value: 'storagebox', label: t('remote.storagebox') },
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
        ) : kind === 's3' ? (
          <>
            <Input
              label={t('remote.endpoint')}
              value={endpoint}
              placeholder="https://fsn1.your-objectstorage.com"
              hint={t('remote.endpointHint')}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => {
                setEndpoint(e.target.value);
                invalidate();
              }}
            />
            <div className="files-remote-row">
              <Input
                required
                label={t('remote.bucket')}
                value={bucket}
                autoComplete="off"
                spellCheck={false}
                onChange={(e) => {
                  setBucket(e.target.value);
                  invalidate();
                }}
              />
              <Input
                label={t('remote.region')}
                value={region}
                placeholder="us-east-1"
                autoComplete="off"
                spellCheck={false}
                onChange={(e) => {
                  setRegion(e.target.value);
                  invalidate();
                }}
              />
            </div>
          </>
        ) : box ? null : (
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
          label={t(kind === 's3' ? 'remote.accessKey' : 'remote.user')}
          value={user}
          hint={box ? t('remote.storageboxHint') : undefined}
          onChange={(e) => {
            setUser(e.target.value);
            if (box) setHostKey('');
            invalidate();
          }}
          autoComplete="off"
          spellCheck={false}
        />
        {proto === 'sftp' && (
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
        {proto !== 'sftp' || auth === 'password' ? (
          <Input
            type="password"
            label={t(kind === 's3' ? 'remote.secretKey' : 'remote.password')}
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
            placeholder={t(kind === 's3' ? 'remote.bucketRoot' : 'remote.loginFolder')}
            value={path}
            spellCheck={false}
            onChange={(e) => {
              setPath(e.target.value);
              invalidate();
            }}
          />
        )}
        {kind === 's3' && (
          <Switch
            label={t('remote.pathStyle')}
            checked={pathStyle}
            onChange={(on) => {
              setPathStyle(on);
              invalidate();
            }}
          />
        )}
        {proto === 'sftp' && connection?.fingerprint && !trust && (
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
