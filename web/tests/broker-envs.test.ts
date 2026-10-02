// Broker policy for environments (env option) and user-approved network hosts.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { authorize, authorizeHttpStream, authorizePty, authorizeStream, type BrokerManifest } from '../src/plugins/broker.ts';

const m: BrokerManifest = {
  id: 'docker',
  capabilities: {
    commands: [
      { name: 'ps', admin: true, adminUnlessGroup: 'docker', remote: 'docker' },
      { name: 'sh', admin: true, pty: true, remote: 'docker' },
      { name: 'local', admin: true },
    ],
    http: [
      { name: 'docker', admin: true, adminUnlessGroup: 'docker', remote: 'docker', rules: [{ methods: ['GET'], path: '/containers/json' }] },
      { name: 'plain', admin: false, rules: [{ methods: ['GET'], path: '/x' }] },
    ],
    userHosts: true,
  },
};
const user = { groups: ['users'], isRoot: false, home: '/home/ann' };
const ENV = 'env-0a1b2c3d';

test('an HTTP request may name an environment only when the API opts in', () => {
  const ok = authorize(m, 'http', { name: 'docker', method: 'GET', path: '/containers/json', env: ENV }, user);
  // Against an environment the daemon's tunnel is the user's own: no administrator rights apply.
  assert.deepEqual(ok, { kind: 'call', method: 'plugins.http', params: { plugin: 'docker', name: 'docker', method: 'GET', path: '/containers/json', env: ENV }, admin: false });
  // Without it the usual admin rule applies.
  const local = authorize(m, 'http', { name: 'docker', method: 'GET', path: '/containers/json' }, user);
  assert.equal(local.kind, 'call');
  assert.equal((local as { admin: boolean }).admin, true);
  assert.equal(authorize(m, 'http', { name: 'plain', method: 'GET', path: '/x', env: ENV }, user).kind, 'deny');
  for (const bad of ['env-1', 'ENV-0a1b2c3d', '../x', 5, {}, 'env-0a1b2c3dd']) {
    assert.equal(authorize(m, 'http', { name: 'docker', method: 'GET', path: '/containers/json', env: bad }, user).kind, 'deny', String(bad));
  }
  // The rules still apply to environment calls.
  assert.equal(authorize(m, 'http', { name: 'docker', method: 'DELETE', path: '/containers/json', env: ENV }, user).kind, 'deny');
  const s = authorizeHttpStream(m, { name: 'docker', method: 'GET', path: '/containers/json', env: ENV }, user);
  assert.equal(s.kind, 'stream');
  assert.equal((s as { params: { env?: string } }).params.env, ENV);
});

test('commands: exec, execStream and pty take an environment only when declared remote', () => {
  const e = authorize(m, 'exec', { command: 'ps', args: [], env: ENV }, user);
  assert.deepEqual(e, { kind: 'call', method: 'plugins.exec', params: { plugin: 'docker', command: 'ps', args: [], env: ENV }, admin: false });
  assert.equal(authorize(m, 'exec', { command: 'local', args: [], env: ENV }, user).kind, 'deny');
  assert.equal(authorize(m, 'exec', { command: 'ps', args: [], env: 'nope' }, user).kind, 'deny');
  const st = authorizeStream(m, 'ps', [], user, ENV);
  assert.equal(st.kind, 'stream');
  assert.equal((st as { admin: boolean }).admin, false);
  assert.equal(authorizeStream(m, 'local', [], user, ENV).kind, 'deny');
  assert.equal((authorizeStream(m, 'ps', [], user) as { admin: boolean }).admin, true);
  const p = authorizePty(m, 'sh', [], 80, 24, user, ENV);
  assert.equal(p.kind, 'stream');
  assert.equal((p as { params: { env?: string } }).params.env, ENV);
});

test('plugins.envs.list takes no arguments and no admin rights', () => {
  assert.deepEqual(authorize(m, 'envs', { env: ENV }, user), { kind: 'call', method: 'plugins.envs.list', params: {}, admin: false });
});

test('network requests need userHosts and a plain host name', () => {
  assert.deepEqual(authorize(m, 'network', { host: 'Registry.Example.org:5000' }, user), { kind: 'network', host: 'registry.example.org:5000', scheme: 'https' });
  assert.deepEqual(authorize(m, 'network', { host: 'intranet', scheme: 'http' }, user), { kind: 'network', host: 'intranet', scheme: 'http' });
  for (const bad of ['', '*.example.org', 'https://x.org', 'x.org/path', 'a b', 'x.org:99999999', 'x@y.org', 5]) {
    assert.equal(authorize(m, 'network', { host: bad }, user).kind, 'deny', String(bad));
  }
  const without: BrokerManifest = { id: 'x', capabilities: {} };
  assert.equal(authorize(without, 'network', { host: 'x.org' }, user).kind, 'deny');
});
