// Broker policy tests: `npm --prefix web test` (node --test with built-in TypeScript type stripping).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { authorize, authorizeStream, normPath, underDeclared, validAsset, type BrokerManifest } from '../src/plugins/broker.ts';

const docker: BrokerManifest = {
  id: 'docker',
  capabilities: {
    commands: [
      { name: 'ps', admin: true, adminUnlessGroup: 'docker' },
      { name: 'logs', admin: true, adminUnlessGroup: 'docker', args: [{}] },
      { name: 'version', admin: false },
    ],
    files: { read: ['/srv', '~/projects'], write: ['/srv/www'] },
  },
  contributes: { pages: [{ id: 'docker' }] },
};
const user = { groups: ['users'], isRoot: false, home: '/home/ann' };
const inDocker = { ...user, groups: ['docker'] };

test('only declared commands, through plugins.exec', () => {
  const p = authorize(docker, 'exec', { command: 'version', args: [] }, user);
  assert.deepEqual(p, { kind: 'call', method: 'plugins.exec', params: { plugin: 'docker', command: 'version', args: [] }, admin: false });
  assert.equal(authorize(docker, 'exec', { command: 'rm' }, user).kind, 'deny');
  assert.equal(authorize(docker, 'exec', { command: 'toString' }, user).kind, 'deny');
  assert.equal(authorize(docker, 'exec', { command: 'ps', args: 'x' }, user).kind, 'deny');
  assert.equal(authorize(docker, 'exec', { command: 'ps', args: [1] }, user).kind, 'deny');
  assert.equal(authorize(docker, 'exec', { command: 'ps', args: new Array(17).fill('a') }, user).kind, 'deny');
});

test('the plugin id always comes from the manifest, never from the request', () => {
  const p = authorize(docker, 'exec', { command: 'version', plugin: 'other' }, user);
  assert.equal(p.kind === 'call' && p.params.plugin, 'docker');
});

test('admin only for commands declared admin, and only when needed', () => {
  const asUser = authorize(docker, 'exec', { command: 'ps' }, user);
  assert.equal(asUser.kind === 'call' && asUser.admin, true);
  const asDocker = authorize(docker, 'exec', { command: 'ps' }, inDocker);
  assert.equal(asDocker.kind === 'call' && asDocker.admin, false);
  const root = authorize(docker, 'exec', { command: 'ps' }, { ...user, isRoot: true });
  assert.equal(root.kind === 'call' && root.admin, false);
  const plain = authorize(docker, 'exec', { command: 'version', admin: true }, user);
  assert.equal(plain.kind === 'call' && plain.admin, false, 'a request cannot ask for admin');
});

test('no other RPC methods or operations', () => {
  for (const op of ['call', 'stream', 'files.read', 'plugins.install', 'config.set', 'prefs.set', '__proto__']) {
    assert.equal(authorize(docker, op, { method: 'system.power' }, user).kind, 'deny', op);
  }
});

test('files: only declared folders, write only in write folders', () => {
  const ok = (op: string, path: string) => authorize(docker, op, { path, data: 'x' }, user).kind === 'call';
  assert.ok(ok('readFile', '/srv/a.txt'));
  assert.ok(ok('listDir', '/srv'));
  assert.ok(ok('readFile', '~/projects/x'));
  assert.ok(ok('readFile', '/home/ann/projects/x'));
  assert.ok(ok('writeFile', '/srv/www/index.html'));
  assert.ok(!ok('writeFile', '/srv/a.txt'), 'read-only folder');
  assert.ok(!ok('readFile', '/etc/shadow'));
  assert.ok(!ok('readFile', '/srv/../etc/shadow'));
  assert.ok(!ok('readFile', '/srvx/a'));
  assert.ok(!ok('readFile', 'srv/a'));
  assert.ok(!ok('readFile', '/home/bob/projects/x'));
  const w = authorize(docker, 'writeFile', { path: '/srv/www/a', data: 'x' }, user);
  assert.equal(w.kind === 'call' && w.method, 'plugins.writeFile');
  assert.equal(w.kind === 'call' && w.admin, false);
  const none: BrokerManifest = { id: 'none' };
  assert.equal(authorize(none, 'readFile', { path: '/' }, user).kind, 'deny');
});

test('streams: declared commands only', () => {
  assert.equal(authorizeStream(docker, 'logs', ['web'], user).kind, 'stream');
  assert.equal(authorizeStream(docker, 'bash', [], user).kind, 'deny');
});

test('assets: relative paths inside the plugin folder', () => {
  assert.ok(validAsset('img/logo.png'));
  for (const bad of ['', '/etc/passwd', '../x', 'a/../../x', 'a//b', 'a\\b', '%2e%2e/x', 'a?b', './a']) assert.ok(!validAsset(bad), bad);
});

test('toast and open', () => {
  assert.equal(authorize(docker, 'toast', { title: 'Hi', tone: 'warn' }, user).kind, 'toast');
  assert.equal(authorize(docker, 'toast', { title: 'x'.repeat(201) }, user).kind, 'deny');
  assert.deepEqual(authorize(docker, 'open', { page: 'docker' }, user), { kind: 'open', to: '/p/docker/docker' });
  assert.equal(authorize(docker, 'open', { page: '../../settings' }, user).kind, 'deny');
});

test('path helpers', () => {
  assert.equal(normPath('/a/./b/../c'), '/a/c');
  assert.equal(normPath('/../..'), '/');
  assert.equal(normPath('~/x', '/home/ann/'), '/home/ann/x');
  assert.equal(normPath('~/x'), null);
  assert.ok(underDeclared('/anything', ['/']));
});
