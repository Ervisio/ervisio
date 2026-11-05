import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resetBrowserPreferences } from '../src/sections/settings/resetBrowser.ts';

test('browser reset removes Ervisio UI caches without clearing auth, fixture data, or unrelated applications', () => {
  const values = new Map([
    ['ervisio.prefs:["alice"]', '{}'],
    ['ervisio.login-shell:["alice"]', 'ervisios'],
    ['ervisio.term.layout', '{}'],
    ['ervisio.lang', 'it'],
    ['ervisio.rememberUsers', '1'],
    ['ervisio.mock.user', 'alice'],
    ['ervisio.mock.prefs', '{}'],
    ['other.application', 'important'],
    ['ervisio.host', '{}'],
    ['auth.token', 'preserve'],
  ]);
  const storage = {
    get length() {
      return values.size;
    },
    key: (index: number) => [...values.keys()][index],
    removeItem: (key: string) => {
      values.delete(key);
    },
  };
  assert.equal(resetBrowserPreferences(storage), 5);
  assert.deepEqual(
    [...values.keys()],
    ['ervisio.mock.user', 'ervisio.mock.prefs', 'other.application', 'ervisio.host', 'auth.token'],
  );
});
