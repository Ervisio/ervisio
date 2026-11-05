import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  defaultDesktop,
  layoutDesktopItems,
  placeDesktopItems,
  readDesktop,
} from '../src/desktop/model.ts';

test('old desktops migrate without positions and position data is bounded to actual virtual items', () => {
  const original = { ...defaultDesktop(['files', 'terminal']), positions: undefined };
  const migrated = readDesktop(original, ['files', 'terminal']);
  assert.deepEqual({ ...migrated.positions }, {});
  const restored = readDesktop(
    {
      ...original,
      positions: {
        'shortcut:files': { column: 4, row: 3 },
        'shortcut:terminal': { column: -1, row: 0 },
        unknown: { column: 0, row: 0 },
      },
    },
    ['files', 'terminal'],
  );
  assert.deepEqual({ ...restored.positions }, { 'shortcut:files': { column: 4, row: 3 } });
  assert.equal(original.positions, undefined);
});

test('viewport reflow resolves collisions and keeps every icon visible without destroying saved wide positions', () => {
  const ids = ['a', 'b', 'c', 'd', 'e'];
  const saved = { a: { column: 8, row: 5 }, b: { column: 8, row: 5 }, c: { column: 0, row: 0 } };
  const before = structuredClone(saved);
  const narrow = layoutDesktopItems(ids, saved, 2, 2);
  assert.equal(
    new Set(Object.values(narrow).map((cell) => `${cell.column}:${cell.row}`)).size,
    ids.length,
  );
  assert.ok(Object.values(narrow).every((cell) => cell.column < 2 && cell.row < 3));
  assert.deepEqual(saved, before);
  assert.deepEqual(layoutDesktopItems(ids, saved, 12, 6).a, saved.a);
});

test('group movement preserves offsets, snaps at edges and relocates occupied icons without losing any', () => {
  const data = defaultDesktop(['files', 'terminal', 'plugins']);
  const ids = data.shortcuts.map((item) => item.id);
  data.positions = {
    [ids[0]]: { column: 0, row: 0 },
    [ids[1]]: { column: 0, row: 1 },
    [ids[2]]: { column: 2, row: 2 },
  };
  const moved = placeDesktopItems(data, ids, ids.slice(0, 2), ids[0], { column: 2, row: 2 }, 3, 4);
  assert.deepEqual(moved.positions[ids[0]], { column: 2, row: 2 });
  assert.deepEqual(moved.positions[ids[1]], { column: 2, row: 3 });
  assert.notDeepEqual(moved.positions[ids[2]], { column: 2, row: 2 });
  assert.equal(new Set(ids.map((id) => JSON.stringify(moved.positions[id]))).size, ids.length);
  assert.deepEqual(moved.shortcuts, data.shortcuts);
  assert.deepEqual(data.positions[ids[0]], { column: 0, row: 0 });
  const edge = placeDesktopItems(
    moved,
    ids,
    ids.slice(0, 2),
    ids[0],
    { column: 100, row: 100 },
    3,
    4,
  );
  assert.deepEqual(edge.positions[ids[0]], { column: 2, row: 2 });
});

test('position preferences survive per-user JSON round trips and retain absent plugin shortcuts', () => {
  const data = defaultDesktop(['files', 'p:ai/ai']);
  data.positions['shortcut:p:ai/ai'] = { column: 3, row: 2 };
  const restored = readDesktop(JSON.parse(JSON.stringify(data)), ['files']);
  assert.deepEqual(restored.positions['shortcut:p:ai/ai'], { column: 3, row: 2 });
  assert.ok(restored.shortcuts.some((item) => item.app === 'p:ai/ai'));
  assert.deepEqual(defaultDesktop(['files']).positions, {});
});
