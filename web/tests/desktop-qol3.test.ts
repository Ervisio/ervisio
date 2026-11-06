import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  defaultDesktop,
  moveItem,
  readDesktop,
  removeFolder,
  validDirectoryPath,
} from '../src/desktop/model.ts';
import { dropSources, hasExternalFiles } from '../src/sections/files/dropfiles.ts';
import { menuPosition } from '../src/ui/menuPosition.ts';

test('folder links migrate additively and validate targets without creating host folders', () => {
  const original = defaultDesktop(['files']);
  const migrated = readDesktop({ ...original, directoryShortcuts: undefined }, ['files']);
  assert.deepEqual(migrated.directoryShortcuts, []);
  const restored = readDesktop(
    {
      ...original,
      folders: [{ id: 'organizer', name: 'Work', parent: null }],
      directoryShortcuts: [
        { id: 'link', name: 'Projects', path: '/srv/projects', folder: 'organizer' },
        { id: 'network', name: 'Remote', path: 'remote:0123456789abcdef:/projects', folder: null },
        { id: 'bad', name: 'Bad', path: 'javascript:alert(1)' },
        { id: 'link', name: 'Duplicate', path: '/etc' },
        { id: 'organizer', name: 'Collision', path: '/etc' },
      ],
      positions: { link: { column: 2, row: 3 } },
    },
    ['files'],
  );
  assert.equal(restored.directoryShortcuts.length, 2);
  assert.deepEqual(restored.positions.link, { column: 2, row: 3 });
  assert.equal(
    moveItem(restored, 'network', 'organizer').directoryShortcuts[1].folder,
    'organizer',
  );
  assert.equal(removeFolder(restored, 'organizer').directoryShortcuts[0].folder, null);
  assert.equal(restored.directoryShortcuts[0].folder, 'organizer');
  for (const path of ['/srv/a b', 'C:\\Projects', '\\\\nas\\backup', 'remote:0123456789abcdef:/'])
    assert.ok(validDirectoryPath(path), path);
  for (const path of [
    'relative',
    'sftp://user:password@host/path',
    'remote:bad:/',
    '/srv\0secret',
    '/srv\nsecret',
  ])
    assert.equal(validDirectoryPath(path), false, path);
});

test('internal drag cannot be mistaken for a computer upload even with attached Files', () => {
  const data = (types: string[]) => ({ types }) as unknown as DataTransfer;
  assert.equal(hasExternalFiles(data(['Files'])), true);
  assert.equal(hasExternalFiles(data(['Files', 'application/x-ervisio-files'])), false);
  assert.equal(hasExternalFiles(data(['Files', 'application/x-ervisio-desktop'])), false);
  assert.equal(hasExternalFiles(data(['text/plain'])), false);
});

test('same-parent drops never copy while cross-folder drops keep only actionable sources', () => {
  assert.deepEqual(dropSources(['/srv/a', '/srv/a', '/srv/b'], '/srv/'), []);
  assert.deepEqual(dropSources(['/srv/a', '/else/b'], '/srv'), ['/else/b']);
  assert.deepEqual(dropSources(['C:\\Projects\\a.txt'], 'c:/projects/'), []);
  assert.deepEqual(dropSources(['remote:0123456789abcdef:/a'], 'remote:0123456789abcdef:/'), []);
  assert.deepEqual(dropSources({ bad: true }, '/srv'), []);
});

test('short taskbar menus anchor directly above their icon and clamp every viewport edge', () => {
  assert.deepEqual(
    menuPosition(
      { x: 360, y: 520 },
      { width: 246, height: 124 },
      { width: 816, height: 590 },
      true,
    ),
    { x: 360, y: 388 },
  );
  assert.deepEqual(
    menuPosition(
      { x: 370, y: 760 },
      { width: 246, height: 124 },
      { width: 390, height: 844 },
      true,
    ),
    { x: 136, y: 628 },
  );
  assert.deepEqual(
    menuPosition({ x: -20, y: 5 }, { width: 246, height: 124 }, { width: 390, height: 844 }, true),
    { x: 8, y: 8 },
  );
});
