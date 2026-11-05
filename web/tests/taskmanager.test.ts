import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  machineCpu,
  processWindow,
  selectProcesses,
  type TaskProcess,
} from '../src/sections/taskmanager/model.ts';

const row = (pid: number, changes: Partial<TaskProcess> = {}): TaskProcess => ({
  pid,
  ppid: 1,
  name: `worker-${pid}`,
  command: '/usr/bin/worker',
  user: 'alice',
  state: 'S',
  cpu: 0,
  memory: 1024,
  memPercent: 1,
  threads: 1,
  cpuAvailable: true,
  memoryAvailable: true,
  ...changes,
});

test('process CPU uses total machine capacity and unavailable samples remain distinct from zero', () => {
  assert.equal(machineCpu(row(1, { cpu: 250 }), 4), 62.5);
  assert.equal(machineCpu(row(1, { cpu: 400 }), 4), 100);
  assert.equal(machineCpu(row(1, { cpuAvailable: false }), 4), null);
  assert.equal(machineCpu(row(1, { cpu: Number.NaN }), 4), null);
  assert.equal(machineCpu(row(1), 0), 0);
});

test('all processes remain searchable by PID/name/owner and sorting does not mutate the snapshot', () => {
  const rows = Array.from({ length: 2500 }, (_, index) => row(index + 1));
  assert.equal(selectProcesses(rows, '2500', { key: 'pid', direction: 'asc' })[0].pid, 2500);
  assert.equal(
    selectProcesses(rows, 'worker-2499', { key: 'name', direction: 'asc' })[0].pid,
    2499,
  );
  assert.equal(selectProcesses(rows, 'alice', { key: 'pid', direction: 'desc' }).length, 2500);
  assert.equal(selectProcesses(rows, '', { key: 'pid', direction: 'desc' })[0].pid, 2500);
  assert.equal(rows[0].pid, 1);
});

test('unavailable counters sort last in both directions rather than being presented as zero', () => {
  const rows = [
    row(1, { cpuAvailable: false, memoryAvailable: false }),
    row(2),
    row(3, { cpu: 30, memory: 8192 }),
  ];
  assert.deepEqual(
    selectProcesses(rows, '', { key: 'cpu', direction: 'desc' }).map((p) => p.pid),
    [3, 2, 1],
  );
  assert.deepEqual(
    selectProcesses(rows, '', { key: 'memory', direction: 'asc' }).map((p) => p.pid),
    [2, 3, 1],
  );
});

test('virtualized process window bounds DOM rows while exposing the final processes', () => {
  assert.deepEqual(processWindow(0, 0, 400), { start: 0, end: 0 });
  const first = processWindow(2500, 0, 430);
  assert.equal(first.start, 0);
  assert.ok(first.end <= 24);
  const last = processWindow(2500, 2495 * 43, 430);
  assert.equal(last.end, 2500);
  assert.ok(last.end - last.start < 25);
});
