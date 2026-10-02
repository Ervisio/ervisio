// Moving this browser's LinuxAdmin settings ("la.*") to Ervisio's keys ("ervisio.*"): `npm --prefix web test`.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { migrateStorage, MIGRATED_KEY } from '../src/lib/storageMigration.ts';

class MemStorage {
  m = new Map<string, string>();
  get length() {
    return this.m.size;
  }
  key(i: number) {
    return [...this.m.keys()][i] ?? null;
  }
  getItem(k: string) {
    return this.m.has(k) ? this.m.get(k)! : null;
  }
  setItem(k: string, v: string) {
    this.m.set(k, String(v));
  }
  removeItem(k: string) {
    this.m.delete(k);
  }
}

test('copies la.* keys to ervisio.* once and removes them', () => {
  const s = new MemStorage();
  s.setItem('la.lang', 'it');
  s.setItem('la.themeVars', '{"vars":{},"scheme":"dark"}');
  s.setItem('la.recentUsers', '[{"user":"alice"}]');
  s.setItem('la.term.layout', '{}');
  s.setItem('other', 'x');
  s.setItem('lazy', 'not ours');
  assert.equal(migrateStorage(s), 4);
  assert.equal(s.getItem('ervisio.lang'), 'it');
  assert.equal(s.getItem('ervisio.themeVars'), '{"vars":{},"scheme":"dark"}');
  assert.equal(s.getItem('ervisio.recentUsers'), '[{"user":"alice"}]');
  assert.equal(s.getItem('ervisio.term.layout'), '{}');
  assert.equal(s.getItem('la.lang'), null);
  assert.equal(s.getItem('other'), 'x');
  assert.equal(s.getItem('lazy'), 'not ours');
  assert.equal(s.getItem(MIGRATED_KEY), '1');
  // Once only: an "la." key written later (an old tab) is left alone.
  s.setItem('la.lang', 'en');
  assert.equal(migrateStorage(s), 0);
  assert.equal(s.getItem('ervisio.lang'), 'it');
});

test('never overwrites a value Ervisio already has', () => {
  const s = new MemStorage();
  s.setItem('ervisio.lang', 'en');
  s.setItem('la.lang', 'it');
  assert.equal(migrateStorage(s), 0);
  assert.equal(s.getItem('ervisio.lang'), 'en');
  assert.equal(s.getItem('la.lang'), null);
});

test('survives storage that throws or is missing', () => {
  assert.equal(migrateStorage(null), 0);
  const broken = {
    length: 1,
    key: () => 'la.x',
    getItem: () => {
      throw new Error('SecurityError');
    },
    setItem: () => {},
    removeItem: () => {},
  };
  assert.equal(migrateStorage(broken), 0);
});
