// Broker policy of sdk.api.jobs and sdk.api.notify: `npm --prefix web test`.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { authorize, type BrokerManifest } from '../src/plugins/broker.ts';

const m: BrokerManifest = {
  id: 'docker',
  capabilities: { jobs: [{ name: 'poll' }, { name: 'backup' }], notify: true },
};
const user = { groups: ['users'], isRoot: false, home: '/home/ann' };
const call = (op: string, a: Record<string, unknown>, mm: BrokerManifest = m) => authorize(mm, op, a, user);

test('create only for declared jobs, never as admin, plugin id from the manifest', () => {
  const p = call('jobs', { action: 'create', job: 'poll', plugin: 'evil', name: 'Web', params: { dir: '/opt/stacks/web' }, schedule: { every: 300 }, runAs: 'ann', confirmAdmin: true, extra: 1 });
  assert.deepEqual(p, {
    kind: 'call',
    method: 'plugins.jobs.create',
    params: { plugin: 'docker', job: 'poll', name: 'Web', params: { dir: '/opt/stacks/web' }, schedule: { every: 300 }, runAs: 'ann', confirmAdmin: true },
    admin: false,
  });
  assert.equal(call('jobs', { action: 'create', job: 'rm-rf' }).kind, 'deny');
  assert.equal(call('jobs', { action: 'create' }).kind, 'deny');
  assert.equal(call('jobs', { action: 'create', job: 'toString' }).kind, 'deny');
});

test('a plugin without jobs cannot use them', () => {
  const none: BrokerManifest = { id: 'x', capabilities: {} };
  const p = call('jobs', { action: 'list' }, none);
  assert.equal(p.kind, 'deny');
});

test('params, schedule and ids are checked', () => {
  assert.equal(call('jobs', { action: 'create', job: 'poll', params: { a: 1 } }).kind, 'deny');
  assert.equal(call('jobs', { action: 'create', job: 'poll', params: 'x' }).kind, 'deny');
  assert.equal(call('jobs', { action: 'create', job: 'poll', schedule: { every: '5m' } }).kind, 'deny');
  assert.equal(call('jobs', { action: 'create', job: 'poll', schedule: { at: ['03:30'], days: [1, 5] } }).kind, 'call');
  assert.equal(call('jobs', { action: 'update', id: 'abcd1234', schedule: null }).kind, 'call');
  assert.equal(call('jobs', { action: 'create', job: 'poll', schedule: null }).kind, 'deny');
  assert.equal(call('jobs', { action: 'update', id: '../x' }).kind, 'deny');
  assert.equal(call('jobs', { action: 'runNow', id: 'abcd1234' }).kind, 'call');
  assert.equal(call('jobs', { action: 'runNow' }).kind, 'deny');
  assert.equal(call('jobs', { action: 'history', id: 'abcd1234', limit: 500 }).kind, 'deny');
  assert.deepEqual(call('jobs', { action: 'history', id: 'abcd1234', limit: 5 }), { kind: 'call', method: 'plugins.jobs.history', params: { plugin: 'docker', id: 'abcd1234', limit: 5 }, admin: false });
});

test('webhook actions', () => {
  assert.deepEqual(call('jobs', { action: 'webhookCreate', id: 'abcd1234', label: 'ci' }), {
    kind: 'call', method: 'plugins.jobs.webhooks.create', params: { plugin: 'docker', id: 'abcd1234', label: 'ci' }, admin: false,
  });
  assert.equal(call('jobs', { action: 'webhookRevoke', id: 'abcd1234' }).kind, 'deny');
  assert.equal(call('jobs', { action: 'webhookRevoke', id: 'abcd1234', webhook: '12345678' }).kind, 'call');
  assert.equal(call('jobs', { action: 'webhookRegenerate', id: 'abcd1234', webhook: 'zz' }).kind, 'deny');
  assert.equal(call('jobs', { action: 'hack', id: 'abcd1234' }).kind, 'deny');
});

test('notify needs the capability and a title', () => {
  assert.deepEqual(call('notify', { title: 'Done', body: 'ok', level: 'warn', link: '/p/docker/x' }), {
    kind: 'call', method: 'plugins.notify', params: { plugin: 'docker', title: 'Done', body: 'ok', level: 'warn', link: '/p/docker/x' }, admin: false,
  });
  assert.equal(call('notify', {}).kind, 'deny');
  assert.equal(call('notify', { title: 'x', level: 'loud' }).kind, 'deny');
  assert.equal(call('notify', { title: 'x' }, { id: 'x', capabilities: { notify: false } }).kind, 'deny');
});
