/** Pause presentation timers while a plugin window is minimized, without destroying its state. */
export interface FrameClock {
  setInterval(handler: TimerHandler, delay?: number, ...args: unknown[]): number;
  clearInterval(id: number): void;
  requestAnimationFrame(callback: FrameRequestCallback): number;
  cancelAnimationFrame(id: number): void;
}

interface Interval {
  handler: TimerHandler;
  delay?: number;
  args: unknown[];
  running?: number;
}
interface AnimationFrame {
  callback: FrameRequestCallback;
  running?: number;
}

/** Native timer handles remain private; stable negative handles survive pause/resume cycles. */
export function createPresentationTimers(clock: FrameClock) {
  let hidden = false;
  let next = -1;
  const intervals = new Map<number, Interval>();
  const frames = new Map<number, AnimationFrame>();
  const scheduleInterval = (item: Interval) => {
    item.running = clock.setInterval(item.handler, item.delay, ...item.args);
  };
  const scheduleFrame = (id: number, item: AnimationFrame) => {
    item.running = clock.requestAnimationFrame((time) => {
      item.running = undefined;
      if (hidden || !frames.has(id)) return;
      frames.delete(id);
      item.callback(time);
    });
  };
  return {
    setInterval(handler: TimerHandler, delay?: number, ...args: unknown[]): number {
      const id = next--;
      const item: Interval = { handler, delay, args };
      intervals.set(id, item);
      if (!hidden) scheduleInterval(item);
      return id;
    },
    clearInterval(id: number): boolean {
      const item = intervals.get(id);
      if (!item) return false;
      if (item.running !== undefined) clock.clearInterval(item.running);
      intervals.delete(id);
      return true;
    },
    requestAnimationFrame(callback: FrameRequestCallback): number {
      const id = next--;
      const item: AnimationFrame = { callback };
      frames.set(id, item);
      if (!hidden) scheduleFrame(id, item);
      return id;
    },
    cancelAnimationFrame(id: number): boolean {
      const item = frames.get(id);
      if (!item) return false;
      if (item.running !== undefined) clock.cancelAnimationFrame(item.running);
      frames.delete(id);
      return true;
    },
    setHidden(value: boolean): void {
      if (value === hidden) return;
      hidden = value;
      for (const item of intervals.values()) {
        if (hidden && item.running !== undefined) {
          clock.clearInterval(item.running);
          item.running = undefined;
        } else if (!hidden && item.running === undefined) scheduleInterval(item);
      }
      for (const [id, item] of frames) {
        if (hidden && item.running !== undefined) {
          clock.cancelAnimationFrame(item.running);
          item.running = undefined;
        } else if (!hidden && item.running === undefined) scheduleFrame(id, item);
      }
    },
    dispose(): void {
      for (const item of intervals.values())
        if (item.running !== undefined) clock.clearInterval(item.running);
      for (const item of frames.values())
        if (item.running !== undefined) clock.cancelAnimationFrame(item.running);
      intervals.clear();
      frames.clear();
    },
  };
}

function originalHidden(document: Document): () => boolean {
  let target: object | null = document;
  while (target) {
    const descriptor = Object.getOwnPropertyDescriptor(target, 'hidden');
    if (descriptor?.get) return () => !!descriptor.get!.call(document);
    target = Object.getPrototypeOf(target);
  }
  return () => false;
}

/** Install before importing plugin code. One-shot timers, promises, requests and broker traffic stay live. */
export function installFrameVisibility(frame: Window, document: Document) {
  const nativeHidden = originalHidden(document);
  const nativeInterval = frame.setInterval.bind(frame);
  const nativeClearInterval = frame.clearInterval.bind(frame);
  const nativeClearTimeout = frame.clearTimeout.bind(frame);
  const nativeFrame = frame.requestAnimationFrame.bind(frame);
  const nativeCancelFrame = frame.cancelAnimationFrame.bind(frame);
  const timers = createPresentationTimers({
    setInterval: nativeInterval,
    clearInterval: nativeClearInterval,
    requestAnimationFrame: nativeFrame,
    cancelAnimationFrame: nativeCancelFrame,
  });
  let active = true;
  let hidden = nativeHidden();
  const listeners = new Set<(hidden: boolean) => void>();
  const previousHidden = Object.getOwnPropertyDescriptor(document, 'hidden');
  const previousState = Object.getOwnPropertyDescriptor(document, 'visibilityState');
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden });
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => (hidden ? 'hidden' : 'visible'),
  });
  frame.setInterval = timers.setInterval;
  frame.clearInterval = (id = 0) => {
    if (!timers.clearInterval(id)) nativeClearInterval(id);
  };
  // Browser timeout/interval handles share a namespace: retain cross-clearing compatibility.
  frame.clearTimeout = (id = 0) => {
    if (!timers.clearInterval(id)) nativeClearTimeout(id);
  };
  frame.requestAnimationFrame = timers.requestAnimationFrame;
  frame.cancelAnimationFrame = (id) => {
    if (!timers.cancelAnimationFrame(id)) nativeCancelFrame(id);
  };
  const update = (event?: Event) => {
    const next = !active || nativeHidden();
    if (next === hidden) return;
    hidden = next;
    timers.setHidden(hidden);
    document.documentElement.dataset.frameHidden = String(hidden);
    // Native tab visibility already has an event in progress; do not deliver it twice.
    if (!event) document.dispatchEvent(new Event('visibilitychange'));
    for (const callback of listeners) {
      try {
        callback(hidden);
      } catch (error) {
        console.error(error);
      }
    }
  };
  timers.setHidden(hidden);
  document.documentElement.dataset.frameHidden = String(hidden);
  document.addEventListener('visibilitychange', update);
  return {
    get hidden() {
      return hidden;
    },
    onChange(callback: (hidden: boolean) => void) {
      listeners.add(callback);
      return () => {
        listeners.delete(callback);
      };
    },
    setActive(value: boolean) {
      active = value;
      update();
    },
    dispose() {
      document.removeEventListener('visibilitychange', update);
      timers.dispose();
      listeners.clear();
      frame.setInterval = nativeInterval;
      frame.clearInterval = nativeClearInterval;
      frame.clearTimeout = nativeClearTimeout;
      frame.requestAnimationFrame = nativeFrame;
      frame.cancelAnimationFrame = nativeCancelFrame;
      if (previousHidden) Object.defineProperty(document, 'hidden', previousHidden);
      else delete (document as unknown as Record<string, unknown>).hidden;
      if (previousState) Object.defineProperty(document, 'visibilityState', previousState);
      else delete (document as unknown as Record<string, unknown>).visibilityState;
      delete document.documentElement.dataset.frameHidden;
    },
  };
}
