import { test } from 'node:test';
import assert from 'node:assert/strict';
import { rememberDesktopRecovery, takeDesktopRecovery } from '../src/desktop/recovery.ts';

function store() {
  const entries = new Map<string, string>();
  return {
    getItem: (key: string) => entries.get(key) ?? null,
    setItem: (key: string, value: string) => {
      entries.set(key, value);
    },
    removeItem: (key: string) => {
      entries.delete(key);
    },
  };
}
const app = { key: 'editor', to: '/editor' };

test('explicit recovery preserves encoded app metadata once without changing the browser URL', () => {
  const storage = store();
  const url = '/editor?folder=%2Fsrv%2Fproject+folder';
  rememberDesktopRecovery(storage, 'alice', app, url, 1000);
  assert.equal(takeDesktopRecovery(storage, 'alice', [app], 2000)?.url, url);
  assert.equal(takeDesktopRecovery(storage, 'alice', [app], 2000), null);
});

test('recovery rejects another account, expired intent, external URL and app mismatch', () => {
  for (const [scope, url, now] of [
    ['bob', '/editor', 2000],
    ['alice', '/editor', 400_000],
    ['alice', 'https://example.invalid', 2000],
    ['alice', '//example.invalid/editor', 2000],
    ['alice', '/files?path=/etc', 2000],
  ] as const) {
    const storage = store();
    rememberDesktopRecovery(storage, 'alice', app, url, 1000);
    assert.equal(takeDesktopRecovery(storage, scope, [app], now), null);
  }
});
