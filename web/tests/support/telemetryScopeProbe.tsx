import { useState, useSyncExternalStore } from 'react';
import { createRoot } from 'react-dom/client';
import { useAlerts, useHost, useMetrics } from '../../src/sections/overview/data';
import { WindowActivityProvider } from '../../src/lib/windowActivity';

export function mountTelemetryScopeProbe() {
  const fixture = (window as any).telemetryFixture;
  fixture.useSyncExternalStore = useSyncExternalStore;
  function Consumer({ name }: { name: string }) {
    const host = useHost();
    const metrics = useMetrics();
    const alerts = useAlerts();
    const value = {
      host: host?.hostname ?? null,
      cpu: metrics.metrics?.cpu.percent ?? null,
      history: metrics.history.length,
      alerts: alerts.alerts?.map((alert) => alert.id) ?? null,
    };
    fixture.renders.push({ scope: fixture.scope, name, ...value });
    return <output data-testid={`telemetry-${name}`}>{JSON.stringify(value)}</output>;
  }
  function Harness() {
    const [active, setActive] = useState({ one: true, two: true });
    fixture.setActive = (name: 'one' | 'two', enabled: boolean) =>
      setActive((previous) => ({ ...previous, [name]: enabled }));
    return (
      <>
        {(['one', 'two'] as const).map((name) => (
          <WindowActivityProvider key={name} active={active[name]}>
            <Consumer name={name} />
          </WindowActivityProvider>
        ))}
      </>
    );
  }
  const element = document.createElement('div');
  document.body.appendChild(element);
  createRoot(element).render(<Harness />);
}
