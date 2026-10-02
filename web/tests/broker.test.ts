// Broker policy tests: `npm --prefix web test` (node --test with built-in TypeScript type stripping).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  authorize,
  authorizeHttpStream,
  authorizePty,
  authorizeStream,
  cleanHttpPath,
  matchFolder,
  normPath,
  underDeclared,
  validAsset,
  type BrokerManifest,
} from '../src/plugins/broker.ts';

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

/* ---------- SDK v3 ---------- */

const v3: BrokerManifest = {
  id: 'docker',
  capabilities: {
    commands: [
      { name: 'shell', pty: true, admin: true, adminUnlessGroup: 'docker', args: [{}, {}] },
      { name: 'ps', admin: false },
    ],
    http: [
      {
        name: 'docker',
        socket: '/var/run/docker.sock',
        admin: true,
        adminUnlessGroup: 'docker',
        headers: ['Content-Type', 'X-Registry-Auth'],
        rules: [
          { methods: ['GET'], path: '/v1\\.[0-9]+/containers/json' },
          { methods: ['POST'], path: '/v1\\.[0-9]+/containers/[a-zA-Z0-9_.-]+/(start|stop)' },
        ],
      },
      { name: 'user', socket: '/run/user.sock', rules: [{ methods: ['GET'], path: '/ping' }] },
    ],
    files: {
      read: ['/srv', { path: '/opt/stacks', admin: true, adminUnlessGroup: 'docker' }],
      write: [{ path: '/opt/stacks', admin: true, adminUnlessGroup: 'docker' }, { path: '~/.config/x', create: true }],
    },
  },
};

test('http: declared APIs, rules, methods and headers only', () => {
  const ok = authorize(v3, 'http', { name: 'docker', method: 'GET', path: '/v1.41/containers/json', query: 'all=1' }, user);
  assert.deepEqual(ok, {
    kind: 'call',
    method: 'plugins.http',
    params: { plugin: 'docker', name: 'docker', method: 'GET', path: '/v1.41/containers/json', query: 'all=1' },
    admin: true,
  });
  const member = authorize(v3, 'http', { name: 'docker', method: 'POST', path: '/v1.41/containers/web/start', headers: { 'x-registry-auth': 'e30=' } }, inDocker);
  assert.equal(member.kind === 'call' && member.admin, false, 'docker group members use the socket as themselves');
  const plain = authorize(v3, 'http', { name: 'user', method: 'GET', path: '/ping', admin: true }, user);
  assert.equal(plain.kind === 'call' && plain.admin, false, 'a user API never asks for admin');
  const deny = (req: Record<string, unknown>) => authorize(v3, 'http', { name: 'docker', method: 'GET', path: '/v1.41/containers/json', ...req }, user).kind === 'deny';
  assert.ok(deny({ name: 'other' }));
  assert.ok(deny({ method: 'DELETE' }));
  assert.ok(deny({ method: 'get' }));
  assert.ok(deny({ path: '/v1.41/containers/json/x' }), 'rules are anchored');
  assert.ok(deny({ path: '/x/v1.41/containers/json' }), 'rules are anchored');
  assert.ok(deny({ path: '/v1.41/containers/json?all=1' }));
  assert.ok(deny({ method: 'POST', path: '/v1.41/containers/a/../../b/start' }));
  assert.ok(deny({ method: 'POST', path: '/v1.41/containers/a%2f..%2fb/start' }));
  assert.ok(deny({ query: 'a=1\r\nHost: x' }));
  assert.ok(deny({ query: 'a=1#x' }));
  assert.ok(deny({ headers: { Host: 'evil' } }));
  assert.ok(deny({ headers: { Cookie: 'a' } }));
  assert.ok(deny({ headers: { 'Content-Type': 'a\r\nX: y' } }));
  assert.ok(deny({ body: 42 }));
});

test('http: bodies become strings for the daemon', () => {
  const bin = authorize(v3, 'http', { name: 'docker', method: 'POST', path: '/v1.41/containers/a/stop', body: new Uint8Array([255, 0, 254]) }, user);
  assert.equal(bin.kind === 'call' && bin.params.body, '/wD+');
  assert.equal(bin.kind === 'call' && bin.params.b64, true);
  const json = authorize(v3, 'http', { name: 'docker', method: 'POST', path: '/v1.41/containers/a/stop', body: '{}', json: true }, user);
  assert.equal(json.kind === 'call' && json.params.json, true);
});

test('httpStream and pty', () => {
  const s = authorizeHttpStream(v3, { name: 'docker', method: 'GET', path: '/v1.41/containers/json' }, inDocker);
  assert.equal(s.kind === 'stream' && s.method, 'plugins.httpStream');
  assert.equal(authorizeHttpStream(v3, { name: 'docker', method: 'GET', path: '/events' }, user).kind, 'deny');
  assert.equal(authorizeHttpStream(v3, null, user).kind, 'deny');
  const p = authorizePty(v3, 'shell', ['abc', '/bin/sh'], 120, 40, user);
  assert.deepEqual(p, { kind: 'stream', method: 'plugins.pty', params: { plugin: 'docker', command: 'shell', args: ['abc', '/bin/sh'], cols: 120, rows: 40 }, admin: true });
  assert.equal(authorizePty(v3, 'ps', [], 80, 24, user).kind, 'deny', 'only pty commands');
  assert.equal(authorize(v3, 'exec', { command: 'shell', args: ['a', 'b'] }, user).kind, 'deny', 'pty commands never through exec');
  assert.equal(authorizeStream(v3, 'shell', ['a', 'b'], user).kind, 'deny');
});

test('admin folders, mkdir and remove', () => {
  const r = authorize(v3, 'readFile', { path: '/opt/stacks/web/compose.yml' }, user);
  assert.equal(r.kind === 'call' && r.admin, true);
  const m = authorize(v3, 'readFile', { path: '/opt/stacks/web/compose.yml' }, inDocker);
  assert.equal(m.kind === 'call' && m.admin, false);
  const plain = authorize(v3, 'readFile', { path: '/srv/a' }, user);
  assert.equal(plain.kind === 'call' && plain.admin, false);
  const mk = authorize(v3, 'mkdir', { path: '/opt/stacks/new' }, user);
  assert.deepEqual(mk, { kind: 'call', method: 'plugins.mkdir', params: { plugin: 'docker', path: '/opt/stacks/new' }, admin: true });
  const rm = authorize(v3, 'remove', { path: '~/.config/x/a.json' }, user);
  assert.deepEqual(rm, { kind: 'call', method: 'plugins.remove', params: { plugin: 'docker', path: '~/.config/x/a.json' }, admin: false });
  assert.equal(authorize(v3, 'mkdir', { path: '/srv/new' }, user).kind, 'deny', 'mkdir only in write folders');
  assert.equal(authorize(v3, 'remove', { path: '/etc/passwd' }, user).kind, 'deny');
  // Longest match wins; on a tie the entry without admin.
  assert.equal(matchFolder('/a/b/c', ['/a', { path: '/a/b', admin: true }])?.admin, true);
  assert.equal(matchFolder('/a/b', [{ path: '/a/b', admin: true }, '/a/b'])?.admin, undefined);
});

test('http path cleaning', () => {
  assert.equal(cleanHttpPath('/a%20b'), '/a b');
  assert.equal(cleanHttpPath('/'), '/');
  for (const bad of ['', 'x', '//h/x', 'http://h/x', '/a/../b', '/a/./b', '/a//b', '/a/', '/a%2fb', '/%2e%2e', '/a%5cb', '/a%00', '/a\\b', '/a b', '/a?b', '/a#b', '/a%zz']) {
    assert.equal(cleanHttpPath(bad), null, bad);
  }
});

test('openUrl only takes http(s) addresses without credentials', () => {
  assert.deepEqual(authorize(docker, 'openUrl', { url: 'http://10.0.0.2:8080/x' }, user), { kind: 'openUrl', url: 'http://10.0.0.2:8080/x' });
  for (const url of ['javascript:alert(1)', 'data:text/html,x', 'file:///etc/passwd', 'https://u:p@example.org/', 'not a url', 42, 'https://a/' + 'x'.repeat(2048)]) {
    assert.equal(authorize(docker, 'openUrl', { url }, user).kind, 'deny', String(url));
  }
});
