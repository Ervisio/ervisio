// Broker policy tests: `npm --prefix web test` (node --test with built-in TypeScript type stripping).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  authorize,
  authorizeDownload,
  authorizeHttpStream,
  authorizePty,
  authorizeStream,
  authorizeUpload,
  sanitizeFilename,
  SaveLimiter,
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

test('openUrl never opens the app itself', () => {
  const me = { ...user, appOrigin: 'https://box.lan:8443' };
  assert.equal(authorize(docker, 'openUrl', { url: 'https://box.lan:8443/terminal?cmd=id' }, me).kind, 'deny');
  assert.equal(authorize(docker, 'openUrl', { url: 'HTTPS://BOX.lan:8443/' }, me).kind, 'deny');
  assert.equal(authorize(docker, 'openUrl', { url: 'https://box.lan:9000/' }, me).kind, 'openUrl', 'other ports are other services');
});

/* ---------- large transfers and the activity log ---------- */

const xfer: BrokerManifest = {
  id: 'docker',
  capabilities: {
    commands: [
      { name: 'dump', admin: false, args: [{}] },
      { name: 'rootdump', admin: true },
      { name: 'shell', pty: true, admin: false },
    ],
    http: [
      {
        name: 'docker',
        socket: '/var/run/docker.sock',
        admin: true,
        adminUnlessGroup: 'docker',
        headers: ['Content-Type', 'X-Registry-Auth'],
        maxUpload: 1000,
        rules: [
          { methods: ['GET'], path: '/images/[a-z0-9:._/-]+/get' },
          { methods: ['POST'], path: '/images/load' },
          { methods: ['PUT'], path: '/containers/[a-z0-9]+/archive' },
          { methods: ['DELETE'], path: '/images/[a-z0-9]+' },
        ],
      },
      { name: 'plain', socket: '/run/p.sock', rules: [{ methods: ['POST'], path: '/in' }, { methods: ['GET'], path: '/out' }] },
    ],
  },
};
const dl = (req: object, extra: object = {}) => authorizeDownload(xfer, { req, ...extra }, user);

test('download: GET of a declared path, with a file name, admin as for http', () => {
  const p = dl({ name: 'plain', method: 'GET', path: '/out', query: 'a=1' }, { filename: 'out.tar' });
  assert.deepEqual(p, {
    kind: 'transfer',
    transfer: 'download',
    body: { kind: 'download', plugin: 'docker', name: 'plain', method: 'GET', path: '/out', query: 'a=1', headers: undefined, filename: 'out.tar', admin: false },
    admin: false,
  });
  const a = dl({ name: 'docker', method: 'GET', path: '/images/busybox/get' });
  assert.ok(a.kind === 'transfer' && a.admin && a.body.admin === true && a.body.filename === 'download');
  const g = authorizeDownload(xfer, { req: { name: 'docker', method: 'GET', path: '/images/busybox/get' } }, inDocker);
  assert.ok(g.kind === 'transfer' && !g.admin);
  const root = authorizeDownload(xfer, { req: { name: 'docker', method: 'GET', path: '/images/busybox/get' } }, { ...user, isRoot: true });
  assert.ok(root.kind === 'transfer' && !root.admin);
});

test('download: refused when the rules or the method do not allow it', () => {
  const denied = (p: ReturnType<typeof dl>) => assert.equal(p.kind, 'deny');
  denied(dl({ name: 'plain', method: 'POST', path: '/in' }));
  denied(dl({ name: 'plain', method: 'HEAD', path: '/out' }));
  denied(dl({ name: 'plain', method: 'GET', path: '/in' }));
  denied(dl({ name: 'plain', method: 'GET', path: '/out/../in' }));
  denied(dl({ name: 'nope', method: 'GET', path: '/out' }));
  denied(dl({ name: 'plain', method: 'GET', path: '/out', body: 'x' }));
  denied(dl({ name: 'plain', method: 'GET', path: '/out', headers: { Cookie: 'a=b' } }));
  denied(dl({ name: 'plain', method: 'GET', path: '/out', query: 'a b' }));
  denied(dl(undefined as unknown as object));
  denied(dl({ name: 'plain', method: 'GET', path: '/out' }, { filename: 'x'.repeat(300) }));
  denied(dl({ name: 'plain', method: 'GET', path: '/out' }, { filename: 5 }));
  // The plugin id is the manifest's.
  const p = authorizeDownload(xfer, { req: { name: 'plain', method: 'GET', path: '/out' }, plugin: 'other' }, user);
  assert.ok(p.kind === 'transfer' && p.body.plugin === 'docker');
});

test('download of a command: declared, not pty, argument list checked, admin by level', () => {
  const p = authorizeDownload(xfer, { command: 'dump', args: ['x'], filename: 'd.bin' }, user);
  assert.deepEqual(p, { kind: 'transfer', transfer: 'download', body: { kind: 'download', plugin: 'docker', command: 'dump', args: ['x'], filename: 'd.bin', admin: false }, admin: false });
  const r = authorizeDownload(xfer, { command: 'rootdump' }, user);
  assert.ok(r.kind === 'transfer' && r.admin);
  for (const a of [{ command: 'nope' }, { command: 'shell' }, { command: 'dump', args: 'x' }, { command: 'dump', args: new Array(17).fill('a') }, { command: 'dump', args: [1] }]) {
    assert.equal(authorizeDownload(xfer, a, user).kind, 'deny', JSON.stringify(a));
  }
});

test('upload: POST or PUT of a declared path, size within maxUpload', () => {
  const up = (req: object, size: unknown = 10) => authorizeUpload(xfer, req, size, user);
  const p = up({ name: 'docker', method: 'POST', path: '/images/load', query: 'quiet=1', headers: { 'X-Registry-Auth': 'tok' } }, 500);
  assert.ok(p.kind === 'transfer' && p.transfer === 'upload' && p.admin && p.size === 500);
  assert.ok(p.kind === 'transfer' && p.body.size === 500 && p.body.kind === 'upload' && p.body.method === 'POST');
  assert.ok(up({ name: 'docker', method: 'PUT', path: '/containers/abc/archive' }).kind === 'transfer');
  assert.ok(up({ name: 'docker', method: 'POST', path: '/images/load' }, 1000).kind === 'transfer');
  assert.equal(up({ name: 'docker', method: 'POST', path: '/images/load' }, 1001).kind, 'deny', 'over maxUpload');
  assert.ok(up({ name: 'plain', method: 'POST', path: '/in' }, 20 * 2 ** 30).kind === 'transfer', 'default 20 GiB');
  assert.equal(up({ name: 'plain', method: 'POST', path: '/in' }, 20 * 2 ** 30 + 1).kind, 'deny');
  for (const bad of [
    { name: 'docker', method: 'GET', path: '/images/busybox/get' },
    { name: 'docker', method: 'DELETE', path: '/images/abc' },
    { name: 'docker', method: 'PATCH', path: '/images/load' },
    { name: 'docker', method: 'POST', path: '/images/other' },
    { name: 'docker', method: 'POST', path: '/images/load', body: 'x' },
    { name: 'docker', method: 'POST', path: '/images/load', headers: { Authorization: 'x' } },
    { name: 'plain', method: 'PUT', path: '/in' },
    { name: 'nope', method: 'POST', path: '/in' },
    null,
  ]) {
    assert.equal(up(bad as object).kind, 'deny', JSON.stringify(bad));
  }
  for (const size of [-1, 1.5, NaN, Infinity, '10', null]) {
    assert.equal(up({ name: 'plain', method: 'POST', path: '/in' }, size).kind, 'deny', String(size));
  }
});

test('activity log: only this plugin, with checked parameters', () => {
  const p = authorize(xfer, 'auditList', { limit: 50, user: 'ann', since: 1700000000000, until: '2026-10-02T00:00:00Z', cursor: '2026-10-01:3', plugin: 'other' }, user);
  assert.deepEqual(p, {
    kind: 'call',
    method: 'plugins.audit.list',
    params: { plugin: 'docker', user: 'ann', cursor: '2026-10-01:3', since: 1700000000000, until: '2026-10-02T00:00:00Z', limit: 50 },
    admin: false,
  });
  assert.deepEqual(authorize(xfer, 'auditList', {}, user), { kind: 'call', method: 'plugins.audit.list', params: { plugin: 'docker' }, admin: false });
  for (const bad of [{ limit: 0 }, { limit: 1001 }, { limit: 1.5 }, { user: 5 }, { since: {} }, { until: 'x'.repeat(41) }, { text: 'x'.repeat(257) }]) {
    assert.equal(authorize(xfer, 'auditList', bad, user).kind, 'deny', JSON.stringify(bad));
  }
});

test('inline request bodies: up to 8 MiB, base64 for long text, a clear error above', () => {
  const api = { name: 'svc', socket: '/run/s.sock', rules: [{ methods: ['POST'], path: '/in' }] };
  const m: BrokerManifest = { id: 'p', capabilities: { http: [api] } };
  const call = (body: unknown) => authorize(m, 'http', { name: 'svc', method: 'POST', path: '/in', body }, user);
  const small = call('hello');
  assert.ok(small.kind === 'call' && small.params.body === 'hello' && small.params.b64 === undefined);
  // A long text goes out as base64 (JSON escaping cannot grow it past the frame limit).
  const text = 'é"\n'.repeat(200_000); // 600 k chars
  const long = call(text);
  assert.ok(long.kind === 'call' && long.params.b64 === true);
  assert.equal(Buffer.from(String((long as { params: { body: string } }).params.body), 'base64').toString(), text);
  assert.equal(call(new Uint8Array(8 << 20)).kind, 'call', '8 MiB of bytes');
  const tooBig = call(new Uint8Array((8 << 20) + 1));
  assert.ok(tooBig.kind === 'deny' && /sdk\.api\.upload/.test(tooBig.message));
  const hugeText = call('x'.repeat((8 << 20) + 1));
  assert.ok(hugeText.kind === 'deny' && /sdk\.api\.upload/.test(hugeText.message));
  assert.equal(call('é'.repeat(5 << 20)).kind, 'deny', 'text that is over 8 MiB as UTF-8');
  // Streams use the same check.
  assert.equal(authorizeHttpStream(m, { name: 'svc', method: 'POST', path: '/in', body: new Uint8Array((8 << 20) + 1) }, user).kind, 'deny');
});

test('upload can ask for the response as a stream', () => {
  const up = (stream?: boolean) => authorizeUpload(xfer, { name: 'plain', method: 'POST', path: '/in' }, 10, user, stream);
  const a = up();
  assert.ok(a.kind === 'transfer' && !('stream' in a.body));
  const b = up(true);
  assert.ok(b.kind === 'transfer' && b.body.stream === true);
});

/* ---------- sdk.saveFile ---------- */

test('saveFile: a cleaned name, a checked MIME type, the data kinds a plugin may hold', () => {
  const save = (a: Record<string, unknown>) => authorize(docker, 'saveFile', a, user);
  const p = save({ filename: 'export.json', data: '{"a":1}', mime: 'application/json' });
  assert.ok(p.kind === 'save' && p.filename === 'export.json' && p.mime === 'application/json' && p.size === 7);
  const bytes = save({ filename: 'a.bin', data: new Uint8Array(5) });
  assert.ok(bytes.kind === 'save' && bytes.mime === 'application/octet-stream' && bytes.size === 5);
  const blob = save({ filename: 'a.tar', data: new Blob(['abc']), mime: 'application/x-tar' });
  assert.ok(blob.kind === 'save' && blob.size === 3);
  assert.ok((save({ filename: 'é.txt', data: 'é' }) as { size: number }).size === 2, 'size is in UTF-8 bytes');
  for (const bad of [
    {},
    { filename: '', data: 'x' },
    { filename: 5, data: 'x' },
    { filename: 'x'.repeat(1025), data: 'x' },
    { filename: 'a', data: 5 },
    { filename: 'a', data: { length: 1 } },
    { filename: 'a', data: null },
    { filename: 'a', data: 'x', mime: 'text/html; charset=utf-8' },
    { filename: 'a', data: 'x', mime: 'nonsense' },
    { filename: 'a', data: 'x', mime: 5 },
  ]) {
    assert.equal(save(bad).kind, 'deny', JSON.stringify(bad));
  }
});

test('saveFile: at most 64 MiB, with the way to a bigger file in the message', () => {
  const save = (data: unknown) => authorize(docker, 'saveFile', { filename: 'a', data }, user);
  assert.equal(save(new Uint8Array(64 << 20)).kind, 'save');
  const big = save(new Uint8Array((64 << 20) + 1));
  assert.ok(big.kind === 'deny' && /sdk\.api\.download/.test(big.message));
  assert.equal(save(new Blob([new Uint8Array((64 << 20) + 1)])).kind, 'deny');
  assert.equal(save('é'.repeat(33 << 20)).kind, 'deny', 'a text of 66 MiB as UTF-8');
});

test('saveFile names never carry a path or special characters', () => {
  for (const [name, want] of Object.entries({
    'backup.tar': 'backup.tar',
    '../../etc/passwd': 'passwd',
    'C:\\dir\\file.zip': 'file.zip',
    'a"b;c<d>.tar': 'a_b_c_d_.tar',
    '.hidden': 'hidden',
    '...': 'download',
    'line\nbreak.txt': 'line_break.txt',
    'x\u202egnp.exe': 'x_gnp.exe',
    '$(rm -rf).tar': '_(rm -rf).tar',
    'trailing. ': 'trailing',
    'naïve résumé.tar': 'naïve résumé.tar',
  })) {
    assert.equal(sanitizeFilename(name), want, name);
  }
  assert.equal(new TextEncoder().encode(sanitizeFilename('é'.repeat(300))).length <= 200, true);
  assert.equal(sanitizeFilename('a'.repeat(300)).length, 200);
});

test('saveFile is rate limited per frame', () => {
  const l = new SaveLimiter(3, 1000, 100);
  assert.ok(l.allow(10, 0) && l.allow(10, 100) && l.allow(10, 200));
  assert.equal(l.allow(10, 300), false, 'a fourth file in the window');
  assert.ok(l.allow(10, 1001), 'the first one aged out');
  const bytes = new SaveLimiter(100, 1000, 100);
  assert.ok(bytes.allow(60, 0));
  assert.equal(bytes.allow(60, 10), false, 'over the byte budget');
  assert.ok(bytes.allow(40, 20));
  assert.ok(bytes.allow(60, 1500), 'the window moved on');
});
