import assert from 'node:assert/strict';
import { test } from 'node:test';
import { terminalLink } from '../src/sections/terminal/terminalLink.ts';

test('a working-directory link preserves spaces, unicode and shell metacharacters as metadata', () => {
  const cwd = '/srv/clienti/caffè $(touch nope); progetto';
  const params = new URLSearchParams({ cwd });
  assert.deepEqual(terminalLink(new URLSearchParams(params.toString())), {
    cwd,
    command: undefined,
    admin: false,
  });
});

test('Windows directory links and explicit elevation stay distinct from shell commands', () => {
  assert.deepEqual(
    terminalLink(new URLSearchParams({ cwd: 'C:\\Users\\Alice\\My project', admin: '1' })),
    {
      cwd: 'C:\\Users\\Alice\\My project',
      command: undefined,
      admin: true,
    },
  );
  assert.equal(terminalLink(new URLSearchParams('admin=true')), null);
});

test('existing command links remain bounded single-line text for review before running', () => {
  const link = terminalLink(
    new URLSearchParams({ cmd: 'echo one\r\necho two\n' + 'x'.repeat(1100) }),
  );
  assert.ok(link);
  assert.equal(link.command?.length, 1000);
  assert.ok(link.command?.startsWith('echo one echo two '));
  assert.ok(!/[\r\n]/.test(link.command!));
  assert.equal(link.cwd, undefined);
  assert.equal(link.admin, false);
});

test('an ordinary terminal URL has no launch request', () => {
  assert.equal(terminalLink(new URLSearchParams()), null);
  assert.equal(terminalLink(new URLSearchParams('cwd=&cmd=&other=1')), null);
});

test('an explicit Windows shell is passed as metadata alongside a reviewed command', () => {
  const shell = 'C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe';
  const link = terminalLink(new URLSearchParams({ cmd: "& 'C:\\Users\\A B\\node.exe'", shell }));
  assert.equal(link?.shell, shell);
  assert.equal(link?.admin, false);
  assert.equal(
    terminalLink(new URLSearchParams({ cmd: 'echo hi', shell: 'bad\r\nshell' }))?.shell,
    undefined,
  );
});
