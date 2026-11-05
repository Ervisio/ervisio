import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ScopedReadCache } from '../src/lib/scopedReadCache.ts';

test('read cache deduplicates requests, reuses fresh data and refreshes stale data', async () => {
  let now = 1;
  const cache = new ScopedReadCache<number>(() => now);
  let calls = 0;
  let resolve!: (value: number) => void;
  const read = () => {
    calls++;
    return new Promise<number>((done) => {
      resolve = done;
    });
  };
  const first = cache.load('host/alice', read, 100);
  assert.equal(cache.load('host/alice', read, 100), first);
  resolve(7);
  await first;
  assert.equal(await cache.load('host/alice', read, 100), 7);
  assert.equal(calls, 1);
  now = 102;
  const refresh = cache.load('host/alice', read, 100);
  assert.equal(cache.peek('host/alice'), 7, 'stale data remains visible during refresh');
  resolve(8);
  await refresh;
  assert.equal(cache.peek('host/alice'), 8);
});

test('read caches isolate account and host keys, including requests completing after an account switch', async () => {
  const cache = new ScopedReadCache<string>();
  let finish!: (value: string) => void;
  const old = cache.load(
    'host1/alice',
    () =>
      new Promise((done) => {
        finish = done;
      }),
    1000,
  );
  await cache.load('host1/bob', async () => 'bob data', 1000);
  await cache.load('host2/alice', async () => 'other host', 1000);
  finish('alice data');
  await old;
  assert.equal(cache.peek('host1/alice'), 'alice data');
  assert.equal(cache.peek('host1/bob'), 'bob data');
  assert.equal(cache.peek('host2/alice'), 'other host');
});

test('failed reads retain real data and invalidation prevents pre-mutation reads overwriting newer data', async () => {
  const cache = new ScopedReadCache<number>();
  cache.put('alice', 1);
  await assert.rejects(
    cache.load(
      'alice',
      async () => {
        throw Error('offline');
      },
      0,
    ),
  );
  assert.equal(cache.peek('alice'), 1);
  let finish!: (value: number) => void;
  const old = cache.load(
    'alice',
    () =>
      new Promise((done) => {
        finish = done;
      }),
    0,
  );
  cache.invalidate('alice');
  await cache.load('alice', async () => 3, 0);
  finish(2);
  await old;
  assert.equal(cache.peek('alice'), 3);
});
