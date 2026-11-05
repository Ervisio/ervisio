import assert from 'node:assert/strict';
import test from 'node:test';
import {
  normalizeText,
  serializeText,
  preferredEditor,
  textPosition,
} from '../src/sections/editor/document.ts';

test('editing CRLF text preserves the original disk line endings without doubling returns', () => {
  const original = 'server {\r\n  listen 80;\r\n}\r\n';
  const normalized = normalizeText(original);
  assert.equal(normalized.includes('\r'), false);
  assert.equal(serializeText(normalized, 'CRLF'), original);
  assert.equal(serializeText(original, 'CRLF'), original);
  assert.equal(serializeText(original, 'LF'), normalized);
});
test('backend config and code open with syntax; notes and logs use Notepad', () => {
  for (const name of [
    'Caddyfile',
    'Dockerfile',
    '.env',
    'server.yaml',
    'main.py',
    'app.ts',
    'package.json',
  ])
    assert.equal(preferredEditor(name), 'code');
  for (const name of ['notes.txt', 'service.LOG']) assert.equal(preferredEditor(name), 'text');
  assert.deepEqual(textPosition('first\nsecond', 8), { line: 2, column: 3 });
});
