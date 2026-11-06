import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  addShortcut,
  clampBounds,
  defaultDesktop,
  moveItem,
  readDesktop,
  removeFolder,
  deleteDesktopItems,
  restoreDesktopItems,
} from '../src/desktop/model.ts';

const apps = [
  'this-pc',
  'overview',
  'files',
  'terminal',
  'services',
  'plugins',
  'settings',
  'p:ai/ai',
  'p:docker/containers',
];

test('desktop deletion removes organizer contents and supports isolated undo for three minutes', () => {
  const original = tree();
  const { data, deletion } = deleteDesktopItems(original, ['work'], 1000);
  assert.equal(data.folders.length, 0);
  const modified = { ...data, wallpaper: 'aurora' };
  const restored = restoreDesktopItems(modified, deletion, 180_999);
  assert.deepEqual(restored.folders, original.folders);
  assert.equal(restored.wallpaper, 'aurora');
  assert.equal(restoreDesktopItems(modified, deletion, 181_000), modified);
});

function tree() {
  const data = defaultDesktop(apps);
  data.folders = [
    { id: 'work', name: 'Work', parent: null },
    { id: 'servers', name: 'Servers', parent: 'work' },
    { id: 'archive', name: 'Archive', parent: 'servers' },
  ];
  return addShortcut(data, 'p:docker/containers', 'servers');
}

test('first desktop contains real core apps and installed AI only; fresh users have independent objects', () => {
  const first = defaultDesktop(apps);
  assert.deepEqual(
    first.shortcuts.map((s) => s.app),
    ['this-pc', 'files', 'overview', 'terminal', 'plugins', 'settings', 'p:ai/ai'],
  );
  assert.ok(first.shortcuts.every((s) => apps.includes(s.app)));
  first.folders.push({ id: 'alice', name: 'Alice', parent: null });
  first.bounds.files = { x: 10, y: 20, width: 900, height: 600 };
  const second = defaultDesktop(['files']);
  assert.deepEqual(second.folders, []);
  assert.deepEqual(second.bounds, {});
  assert.deepEqual(
    second.shortcuts.map((s) => s.app),
    ['files'],
  );
});

test('folders and shortcuts survive a server JSON round-trip, including temporarily unavailable plugins', () => {
  const stored = tree();
  stored.bounds.files = { x: 100, y: 200, width: 1000, height: 700 };
  stored.wallpaper = 'mint';
  const restored = readDesktop(
    JSON.parse(JSON.stringify(stored)),
    apps.filter((id) => id !== 'p:docker/containers'),
  );
  assert.deepEqual(restored.folders, stored.folders);
  assert.deepEqual(restored.shortcuts, stored.shortcuts);
  assert.deepEqual(restored.bounds.files, stored.bounds.files);
  assert.equal(restored.wallpaper, 'mint');
  assert.ok(restored.shortcuts.some((s) => s.app === 'p:docker/containers'));
});

test('stored corruption is sanitized without mutating the input or creating cyclic folder trees', () => {
  const source = {
    version: 1,
    folders: [
      { id: 'a', name: 'A', parent: 'b' },
      { id: 'b', name: 'B', parent: 'a' },
      { id: 'self', name: 'Self', parent: 'self' },
      { id: 'orphan', name: 'Orphan', parent: 'missing' },
      { id: 'a', name: 'Duplicate', parent: null },
      null,
    ],
    shortcuts: [
      { id: 'a', app: 'files', folder: null },
      { id: 'valid', app: 'files', folder: 'missing' },
      { id: 'valid', app: 'terminal', folder: null },
    ],
    pinned: ['files', 'files', 7],
    wallpaper: 'unknown',
    bounds: {
      files: { x: -1, y: 1e7, width: -50, height: 1e7 },
      invalid: { x: NaN, y: 0, width: 1, height: 1 },
    },
  };
  const before = structuredClone(source);
  const restored = readDesktop(source, apps);
  for (const folder of restored.folders) {
    const seen = new Set<string>();
    let id: string | null = folder.id;
    while (id) {
      assert.ok(!seen.has(id));
      seen.add(id);
      id = restored.folders.find((f) => f.id === id)?.parent ?? null;
    }
  }
  assert.equal(restored.folders.length, 4);
  assert.deepEqual(restored.shortcuts, [{ id: 'valid', app: 'files', folder: null }]);
  assert.deepEqual(restored.pinned, ['files']);
  assert.equal(restored.wallpaper, 'bloom');
  assert.deepEqual(restored.bounds.files, { x: 0, y: 8000, width: 320, height: 8000 });
  assert.equal(restored.bounds.invalid, undefined);
  assert.deepEqual(source, before);
});

test('virtual folder movement rejects descendants/self and retains shortcuts without touching host paths', () => {
  const data = tree();
  assert.equal(moveItem(data, 'work', 'archive'), data);
  assert.equal(moveItem(data, 'work', 'work'), data);
  assert.equal(moveItem(data, 'work', 'unknown'), data);
  const moved = moveItem(data, 'servers', null);
  assert.equal(moved.folders.find((f) => f.id === 'servers')?.parent, null);
  assert.equal(moved.folders.find((f) => f.id === 'archive')?.parent, 'servers');
  assert.deepEqual(moved.shortcuts, data.shortcuts);
});

test('removing a folder promotes its children and shortcuts, preserving all contained apps', () => {
  const data = tree();
  const removed = removeFolder(data, 'servers');
  assert.equal(
    removed.folders.some((f) => f.id === 'servers'),
    false,
  );
  assert.equal(removed.folders.find((f) => f.id === 'archive')?.parent, 'work');
  assert.equal(removed.shortcuts.find((s) => s.app === 'p:docker/containers')?.folder, 'work');
  assert.equal(removed.shortcuts.length, data.shortcuts.length);
  assert.equal(removeFolder(data, 'unknown'), data);
  assert.equal(data.folders.find((f) => f.id === 'archive')?.parent, 'servers');
});

test('adding/moving shortcuts prevents duplicates and unknown targets, and preserves the source on rejected moves', () => {
  let data = tree();
  assert.equal(addShortcut(data, 'files', null), data);
  assert.equal(addShortcut(data, 'files', 'missing'), data);
  data = addShortcut(data, 'files', 'work');
  const source = data.shortcuts.find((s) => s.app === 'files' && s.folder === null)!;
  assert.equal(moveItem(data, source.id, 'work'), data);
  const moved = moveItem(data, source.id, 'servers');
  assert.equal(moved.shortcuts.find((s) => s.id === source.id)?.folder, 'servers');
  assert.equal(moveItem(data, 'missing', null), data);
});

test('clamping keeps oversized, negative and keyboard-resized windows visible inside desktop and tiny viewports', () => {
  for (const viewport of [
    { width: 1440, height: 800 },
    { width: 390, height: 420 },
    { width: 180, height: 120 },
  ]) {
    for (const bounds of [
      { x: 9000, y: 9000, width: 2000, height: 2000 },
      { x: -50, y: -50, width: -100, height: -100 },
      { x: NaN, y: Infinity, width: Infinity, height: NaN },
    ]) {
      const bounded = clampBounds(bounds, viewport.width, viewport.height);
      assert.ok(Object.values(bounded).every(Number.isFinite));
      assert.ok(bounded.width > 0 && bounded.height > 0);
      assert.ok(bounded.x >= 0 && bounded.y >= 0);
      assert.ok(bounded.x + bounded.width <= viewport.width);
      assert.ok(bounded.y + bounded.height <= viewport.height);
    }
  }
});
