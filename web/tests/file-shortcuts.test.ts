import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fileShortcut, type FileKey } from '../src/sections/files/fileShortcuts.ts';

const key = (value: string, patch: Partial<FileKey> = {}): FileKey => ({
  key: value,
  ctrlKey: false,
  metaKey: false,
  shiftKey: false,
  altKey: false,
  ...patch,
});
test('Explorer shortcuts distinguish clipboard, undo, selection and permanent delete', () => {
  for (const [input, output] of [
    ['c', 'copy'],
    ['x', 'cut'],
    ['v', 'paste'],
    ['z', 'undo'],
    ['a', 'selectAll'],
  ] as const)
    assert.equal(fileShortcut(key(input, { ctrlKey: true })), output);
  assert.equal(fileShortcut(key('z', { metaKey: true })), 'undo');
  assert.equal(fileShortcut(key('Delete')), 'delete');
  assert.equal(fileShortcut(key('Delete', { shiftKey: true })), 'permDelete');
  assert.equal(fileShortcut(key('n', { ctrlKey: true, shiftKey: true })), 'newFolder');
  assert.equal(fileShortcut(key('ArrowUp', { altKey: true })), 'up');
  assert.equal(fileShortcut(key('c')), null);
  assert.equal(fileShortcut(key('a', { ctrlKey: true, altKey: true })), null);
});
