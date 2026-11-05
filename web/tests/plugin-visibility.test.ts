import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  createPresentationTimers,
  installFrameVisibility,
  type FrameClock,
} from '../src/plugins/frame/visibility.ts';

class Clock implements FrameClock {
  next = 1;
  intervals = new Map<number, { handler: TimerHandler; args: unknown[] }>();
  frames = new Map<number, FrameRequestCallback>();
  timeouts = new Set<number>();
  setInterval(handler: TimerHandler, _delay?: number, ...args: unknown[]) {
    const id = this.next++;
    this.intervals.set(id, { handler, args });
    return id;
  }
  clearInterval(id: number) {
    this.intervals.delete(id);
  }
  requestAnimationFrame(callback: FrameRequestCallback) {
    const id = this.next++;
    this.frames.set(id, callback);
    return id;
  }
  cancelAnimationFrame(id: number) {
    this.frames.delete(id);
  }
  clearTimeout(id: number) {
    this.timeouts.delete(id);
  }
  pulse() {
    for (const item of [...this.intervals.values()])
      if (typeof item.handler === 'function') item.handler(...item.args);
    const frames = [...this.frames.values()];
    this.frames.clear();
    frames.forEach((callback) => callback(123));
  }
}

test('minimized presentation timers have no live native timers and resume without replaying missed ticks', () => {
  const clock = new Clock();
  const timers = createPresentationTimers(clock);
  let polls = 0,
    frames = 0;
  const interval = timers.setInterval(
    (amount: number) => {
      polls += amount;
    },
    20,
    2,
  );
  const loop = () => {
    frames++;
    timers.requestAnimationFrame(loop);
  };
  timers.requestAnimationFrame(loop);
  clock.pulse();
  assert.deepEqual([polls, frames], [2, 1]);
  timers.setHidden(true);
  assert.equal(clock.intervals.size + clock.frames.size, 0);
  clock.pulse();
  clock.pulse();
  assert.deepEqual([polls, frames], [2, 1]);
  timers.setHidden(false);
  assert.equal(clock.intervals.size, 1);
  assert.equal(clock.frames.size, 1);
  clock.pulse();
  assert.deepEqual([polls, frames], [4, 2]);
  timers.clearInterval(interval);
  timers.dispose();
  assert.equal(clock.intervals.size + clock.frames.size, 0);
});

test('timers created or cancelled while minimized retain stable handles without background wakeups', () => {
  const clock = new Clock();
  const timers = createPresentationTimers(clock);
  timers.setHidden(true);
  let polls = 0,
    frames = 0;
  const keep = timers.setInterval(() => polls++, 10);
  const discard = timers.setInterval(() => {
    throw new Error('cancelled interval ran');
  }, 10);
  const frame = timers.requestAnimationFrame(() => frames++);
  timers.clearInterval(discard);
  timers.cancelAnimationFrame(frame);
  assert.equal(clock.intervals.size + clock.frames.size, 0);
  timers.setHidden(false);
  timers.setHidden(false);
  clock.pulse();
  assert.deepEqual([polls, frames], [1, 0]);
  timers.clearInterval(keep);
  assert.equal(clock.intervals.size, 0);
});

class FrameDocument extends EventTarget {
  actualHidden = false;
  documentElement = { dataset: {} as Record<string, string> };
  get hidden() {
    return this.actualHidden;
  }
  get visibilityState() {
    return this.actualHidden ? 'hidden' : 'visible';
  }
}

test('SDK/document visibility combine window and browser state, preserve drafts, and leave one-shot async work live', async () => {
  const clock = new Clock();
  const oneShot = (callback: () => void) => {
    queueMicrotask(callback);
    return 500;
  };
  const frame = {
    setInterval: clock.setInterval.bind(clock),
    clearInterval: clock.clearInterval.bind(clock),
    requestAnimationFrame: clock.requestAnimationFrame.bind(clock),
    cancelAnimationFrame: clock.cancelAnimationFrame.bind(clock),
    setTimeout: oneShot,
    clearTimeout: clock.clearTimeout.bind(clock),
  } as unknown as Window;
  const document = new FrameDocument();
  const visibility = installFrameVisibility(frame, document as unknown as Document);
  const changes: boolean[] = [];
  const documentEvents: boolean[] = [];
  visibility.onChange((hidden) => changes.push(hidden));
  document.addEventListener('visibilitychange', () => documentEvents.push(document.hidden));
  const draft = { text: 'unsaved question', answer: '' };
  const work = new Promise<void>((resolve) =>
    frame.setTimeout(() => {
      draft.answer = 'completed and saved';
      resolve();
    }, 1),
  );
  const interval = frame.setInterval(() => undefined, 20);
  visibility.setActive(false);
  assert.equal(document.hidden, true);
  assert.equal(document.visibilityState, 'hidden');
  assert.equal(clock.intervals.size, 0);
  await work;
  assert.deepEqual(draft, { text: 'unsaved question', answer: 'completed and saved' });
  assert.equal(frame.setTimeout, oneShot);
  frame.clearTimeout(interval); // browser-compatible cross-clearing of interval IDs
  document.actualHidden = true;
  document.dispatchEvent(new Event('visibilitychange'));
  visibility.setActive(true);
  assert.equal(visibility.hidden, true);
  document.actualHidden = false;
  document.dispatchEvent(new Event('visibilitychange'));
  assert.equal(visibility.hidden, false);
  assert.equal(clock.intervals.size, 0, 'cancelled interval was not resurrected');
  assert.deepEqual(changes, [true, false]);
  assert.deepEqual(
    documentEvents,
    [true, true, false],
    'native visibility events are not duplicated',
  );
  visibility.dispose();
  assert.equal(document.hidden, false);
  assert.equal(Object.hasOwn(document, 'hidden'), false);
});
