import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  isRemoteLocation,
  remoteCallPlan,
  remoteCopyPlan,
  remoteLocation,
  remoteTarget,
} from '../src/sections/files/remoteLocation.ts';
import { networkAddress, parseNetworkPath } from '../src/sections/files/networkPath.ts';

const id = '0123456789abcdef';
const target = remoteLocation(id, '/srv/app');

test('Windows UNC and SMB URLs identify the share and subfolder without accepting credentials', () => {
  const storage = { host: 'u705411.your-storagebox.de', port: 445, share: 'backup', path: '/' };
  assert.deepEqual(parseNetworkPath('\\\\u705411.your-storagebox.de\\backup'), storage);
  assert.deepEqual(parseNetworkPath('smb://u705411.your-storagebox.de/backup'), storage);
  assert.deepEqual(parseNetworkPath('\\\\nas\\shared\\apps\\config'), {
    host: 'nas',
    port: 445,
    share: 'shared',
    path: '/apps/config',
  });
  assert.deepEqual(parseNetworkPath('smb://nas:1445/shared/apps'), {
    host: 'nas',
    port: 1445,
    share: 'shared',
    path: '/apps',
  });
  assert.equal(networkAddress('nas', 'shared', '/apps'), '\\\\nas\\shared\\apps');
  for (const unsafe of [
    'smb://user:secret@nas/share',
    'smb://nas/share?password=secret',
    '\\\\nas',
    'smb://nas/share/../x',
    '\\\\nas\\share\\..\\other',
    '\\\\nas\\bad*share',
  ])
    assert.equal(parseNetworkPath(unsafe), null, unsafe);
});

test('remote locations separate a saved connection and its POSIX path without embedding credentials', () => {
  assert.deepEqual(remoteTarget(target), { conn: id, path: '/srv/app' });
  assert.deepEqual(remoteTarget(remoteLocation(id)), { conn: id, path: '/' });
  assert.equal(isRemoteLocation('remote:password@host:/srv'), false);
  assert.throws(() => remoteLocation(id, '/srv\0/x'));
});

test('only supported remote operations are routed, with native paths and no cross-host mixing', () => {
  assert.deepEqual(remoteCallPlan('files.list', { path: target, showHidden: true }), {
    method: 'remote.ls',
    conn: id,
    params: { path: '/srv/app', showHidden: true, conn: id },
  });
  assert.deepEqual(
    remoteCallPlan('files.writeText', { path: target, expectedMtime: 123, content: 'x' })?.params,
    { path: '/srv/app', expectedMtime: 123, content: 'x', conn: id },
  );
  assert.equal(remoteCallPlan('files.stat', { path: '/etc/hosts' }), null);
  assert.throws(() => remoteCallPlan('files.chmod', { path: target }), /not supported/);
  assert.throws(() => remoteCallPlan('files.delete', { paths: [target], trash: true }), /Trash/);
  assert.throws(
    () => remoteCallPlan('files.rename', { from: target, to: '/tmp/x' }),
    /cannot be mixed/,
  );
  assert.throws(
    () =>
      remoteCallPlan('files.delete', { paths: [target, remoteLocation('fedcba9876543210', '/x')] }),
    /between connections/,
  );
});

test('transfers explicitly distinguish local and remote endpoints', () => {
  assert.deepEqual(remoteCopyPlan(['/tmp/upload.txt'], target), {
    from: { conn: '', paths: ['/tmp/upload.txt'] },
    to: { conn: id, path: '/srv/app' },
  });
  assert.deepEqual(remoteCopyPlan([target], '/tmp/downloads'), {
    from: { conn: id, paths: ['/srv/app'] },
    to: { conn: '', path: '/tmp/downloads' },
  });
  assert.equal(remoteCopyPlan(['/tmp/a'], '/tmp/b'), null);
});
