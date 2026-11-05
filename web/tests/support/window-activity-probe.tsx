import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { WindowActivityProvider, useActivityExternalStore } from '../../src/lib/windowActivity';
import { refreshNow, usePolling } from '../../src/lib/refresh';

const listeners = new Set<() => void>();
let value = 0;
const snapshot = () => value;
const subscribe = (notify: () => void) => {
  listeners.add(notify);
  return () => {
    listeners.delete(notify);
  };
};
const ticks = { one: 0, two: 0 };
type Name = keyof typeof ticks;
export interface ActivityProbe {
  setActive(name: Name, active: boolean): void;
  inspect(): { one: number; two: number; subscribers: number };
  publish(): void;
  refresh(): void;
}
declare global {
  interface Window {
    activityProbe: ActivityProbe;
  }
}

function Probe({ name }: { name: Name }) {
  usePolling(() => {
    ticks[name] += 1;
  }, 30);
  const data = useActivityExternalStore(subscribe, snapshot);
  return (
    <>
      <input aria-label={`${name} state`} defaultValue="" />
      <output data-testid={`${name}-snapshot`}>{data}</output>
    </>
  );
}

export function mountActivityProbe() {
  const element = document.createElement('div');
  element.id = 'window-activity-probe';
  document.body.appendChild(element);
  const control: ActivityProbe = {
    setActive: () => undefined,
    inspect: () => ({ ...ticks, subscribers: listeners.size }),
    publish: () => {
      value += 1;
      listeners.forEach((notify) => notify());
    },
    refresh: refreshNow,
  };
  window.activityProbe = control;
  function Harness() {
    const [activity, setActivity] = useState({ one: true, two: true });
    control.setActive = (name, active) =>
      setActivity((previous) => ({ ...previous, [name]: active }));
    return (
      <>
        {(['one', 'two'] as const).map((name) => (
          <section key={name} hidden={!activity[name]} data-probe={name}>
            <WindowActivityProvider active={activity[name]}>
              <WindowActivityProvider active>
                <Probe name={name} />
              </WindowActivityProvider>
            </WindowActivityProvider>
          </section>
        ))}
      </>
    );
  }
  createRoot(element).render(<Harness />);
}
