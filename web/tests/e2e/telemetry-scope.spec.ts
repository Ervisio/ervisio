import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  page.on('pageerror', (error) => console.log('Telemetry fixture error:', error.message));
  await page.addInitScript(() => {
    const fixture = {
      scope: 'host-A/alice',
      listeners: new Set<() => void>(),
      calls: [] as any[],
      streams: [] as any[],
      renders: [] as any[],
      setActive: (_name: string, _active: boolean) => {},
      changeScope(scope: string) {
        this.scope = scope;
        this.listeners.forEach((notify) => notify());
      },
    };
    (window as any).telemetryFixture = fixture;
  });
  await page.route('**/__telemetry_scope_fixture', (route) =>
    route.fulfill({ contentType: 'text/html', body: '<!doctype html><html><body></body></html>' }),
  );
  await page.route('**/src/lib/accountScope.ts*', (route) =>
    route.fulfill({
      contentType: 'application/javascript',
      body: `export function useAccountScope(){const f=window.telemetryFixture;return f.useSyncExternalStore(cb=>{f.listeners.add(cb);return()=>f.listeners.delete(cb)},()=>f.scope)}`,
    }),
  );
  await page.route('**/src/lib/refresh.ts*', (route) =>
    route.fulfill({
      contentType: 'application/javascript',
      body: 'export const useRefreshInterval=()=>2000;export const onRefreshNow=()=>()=>{};',
    }),
  );
  await page.route('**/src/api/index.ts*', (route) =>
    route.fulfill({
      contentType: 'application/javascript',
      body: `
    export const errorMessage=e=>e.message;
    export function call(method){const f=window.telemetryFixture;return new Promise((resolve,reject)=>f.calls.push({owner:f.scope,method,resolve,reject}))}
    export function stream(method,params,callbacks){const f=window.telemetryFixture;const record={owner:f.scope,method,callbacks,closed:0};f.streams.push(record);return {send(){},close(){record.closed++}}}
  `,
    }),
  );
  await page.goto('/__telemetry_scope_fixture');
  await page.evaluate(async () => {
    const refreshModule = '/@react-refresh';
    const runtime = (await import(refreshModule)).default;
    runtime.injectIntoGlobalHook(window);
    (window as any).$RefreshReg$ = () => {};
    (window as any).$RefreshSig$ = () => (type: unknown) => type;
    (window as any).__vite_plugin_react_preamble_installed__ = true;
    const module = '/tests/support/telemetryScopeProbe.tsx';
    (await import(module)).mountTelemetryScopeProbe();
  });
});

test('shared telemetry has one stream, drops prior scope data even when minimized, and ignores late stream callbacks', async ({
  page,
}) => {
  await expect
    .poll(() => page.evaluate(() => (window as any).telemetryFixture.streams.length))
    .toBe(1);
  await page.evaluate(() => {
    const fixture = (window as any).telemetryFixture;
    for (const call of fixture.calls)
      call.resolve(
        call.method === 'system.host'
          ? { hostname: 'private-host-A' }
          : [{ id: 'private-alert-A' }],
      );
    fixture.streams[0].callbacks.onData({
      time: Date.now(),
      cpu: { percent: 17, cores: [] },
      memory: { percent: 23 },
      net: [],
      disks: [],
    });
  });
  await expect(page.getByTestId('telemetry-one')).toContainText('private-host-A');
  await expect(page.getByTestId('telemetry-two')).toContainText('private-alert-A');
  await page.evaluate(() => (window as any).telemetryFixture.setActive('two', false));
  expect(await page.evaluate(() => (window as any).telemetryFixture.streams[0].closed)).toBe(0);
  await page.evaluate(() => (window as any).telemetryFixture.changeScope('host-B/bob'));
  await expect
    .poll(() => page.evaluate(() => (window as any).telemetryFixture.streams.length))
    .toBe(2);
  await expect(page.getByTestId('telemetry-two')).toHaveText(
    '{' + '"host":null,"cpu":null,"history":0,"alerts":null' + '}',
  );
  await page.evaluate(() => {
    const fixture = (window as any).telemetryFixture;
    fixture.streams[0].callbacks.onData({
      time: Date.now(),
      cpu: { percent: 99 },
      memory: { percent: 99 },
      net: [],
      disks: [],
    });
    fixture.streams[0].callbacks.onError({ message: 'private-A stream error' });
    for (const call of fixture.calls.filter((call: any) => call.owner === 'host-B/bob'))
      call.resolve(call.method === 'system.host' ? { hostname: 'host-B' } : [{ id: 'alert-B' }]);
    fixture.streams[1].callbacks.onData({
      time: Date.now(),
      cpu: { percent: 31 },
      memory: { percent: 42 },
      net: [],
      disks: [],
    });
  });
  await expect(page.getByTestId('telemetry-one')).toContainText('host-B');
  await expect(page.getByTestId('telemetry-one')).toContainText('"cpu":31');
  const leaked = await page.evaluate(() =>
    (window as any).telemetryFixture.renders.filter(
      (render: any) =>
        render.scope === 'host-B/bob' &&
        (render.host === 'private-host-A' ||
          render.cpu === 17 ||
          render.cpu === 99 ||
          render.alerts?.includes('private-alert-A')),
    ),
  );
  expect(leaked).toEqual([]);
  await page.evaluate(() => (window as any).telemetryFixture.setActive('one', false));
  await expect
    .poll(() => page.evaluate(() => (window as any).telemetryFixture.streams[1].closed))
    .toBe(1);
  await page.evaluate(() => (window as any).telemetryFixture.setActive('two', true));
  await expect
    .poll(() => page.evaluate(() => (window as any).telemetryFixture.streams.length))
    .toBe(3);
  await expect(page.getByTestId('telemetry-two')).toContainText('host-B');
});

test('old host and alert RPC responses cannot overwrite the new account after an identity change', async ({
  page,
}) => {
  await expect
    .poll(() => page.evaluate(() => (window as any).telemetryFixture.calls.length))
    .toBe(2);
  await page.evaluate(() => (window as any).telemetryFixture.changeScope('host-B/bob'));
  await expect
    .poll(() => page.evaluate(() => (window as any).telemetryFixture.calls.length))
    .toBe(4);
  await page.evaluate(() => {
    const fixture = (window as any).telemetryFixture;
    for (const owner of ['host-B/bob', 'host-A/alice']) {
      for (const call of fixture.calls.filter((call: any) => call.owner === owner))
        call.resolve(call.method === 'system.host' ? { hostname: owner } : [{ id: owner }]);
    }
  });
  await expect(page.getByTestId('telemetry-one')).toContainText('host-B/bob');
  await expect(page.getByTestId('telemetry-two')).not.toContainText('host-A/alice');
});
