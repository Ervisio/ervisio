import test from 'node:test';
import assert from 'node:assert/strict';
import { parseShellCommand, runShellCommand, setShellCommandHandler } from '../src/shell/commands.ts';

const apps = [
  { id: 'files', title: 'Files', to: '/files', aliases: ['esplora risorse', 'file manager'] },
  { id: 'settings', title: 'Settings', to: '/settings', aliases: ['impostazioni'] },
  { id: 'docker/containers', title: 'Containers', to: '/p/docker/containers', aliases: ['docker'] },
];

test('local intents open only an exact available application or alias', () => {
  assert.deepEqual(parseShellCommand('Apri esplora risorse!', apps), { kind: 'open', app: apps[0] });
  assert.deepEqual(parseShellCommand('Open the Docker', apps), { kind: 'open', app: apps[2] });
  assert.equal(parseShellCommand('apri impostazioni e cancella i file', apps), null);
  assert.equal(parseShellCommand('open /terminal?cmd=rm -rf /', apps), null);
  assert.equal(parseShellCommand('open unknown', apps), null);
  assert.equal(parseShellCommand('apri docker', [...apps, { id: 'docker/volumes', title: 'Volumes', to: '/p/docker/volumes', aliases: ['docker'] }]), null);
});

test('themes and desktop controls are deterministic whole-message requests', () => {
  assert.deepEqual(parseShellCommand('Passa al tema scuro', apps), { kind: 'theme', value: 'dark' });
  assert.deepEqual(parseShellCommand('Cambia il tema', apps), { kind: 'theme', value: 'toggle' });
  assert.deepEqual(parseShellCommand('show the desktop', apps), { kind: 'window', action: 'show-desktop' });
  assert.deepEqual(parseShellCommand('usa ErvisiOS', apps), { kind: 'mode', value: 'ervisios' });
  assert.equal(parseShellCommand('cambia il tema e riavvia nginx', apps), null);
  assert.equal(parseShellCommand('shell_exec rm -rf /', apps), null);
  assert.equal(parseShellCommand('a'.repeat(501), apps), null);
});

test('handler lifecycle does not retain a signed-out shell or remove a newer handler', async () => {
  assert.deepEqual(await runShellCommand('open Files'), { handled: false });
  const oldCleanup = setShellCommandHandler(() => ({ handled: true, message: 'old' }));
  const cleanup = setShellCommandHandler(() => ({ handled: true, message: 'new' }));
  oldCleanup();
  assert.equal((await runShellCommand('open Files')).message, 'new');
  cleanup();
  assert.deepEqual(await runShellCommand('open Files'), { handled: false });
});
