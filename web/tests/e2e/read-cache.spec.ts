import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'it', theme: 'daylight' }),
    );
  });
  // Exercise the real UI/loaders with deterministic server results and count real read starts.
  await page.route('**/src/api/mock.ts', async (route) => {
    const response = await route.fetch();
    const source = await response.text();
    const injection = `
      const probe = globalThis.__readProbe ??= { calls: {}, fail: false };
      if (method === 'system.connectivity' || method.startsWith('software.')) {
        probe.calls[method] = (probe.calls[method] || 0) + 1;
        await new Promise(resolve => setTimeout(resolve, 150));
        if (probe.fail) throw new Error('QA refresh unavailable');
        if (method === 'system.connectivity') {
          const missing = {present:false,supported:false,enabled:null,state:'unavailable',devices:[]};
          return {source:'host',platform:'linux',wifi:{...missing,present:true,supported:true,enabled:true,state:'on',ssid:'QA Wi-Fi'},bluetooth:missing,interfaces:[]};
        }
        if (method === 'software.summary' || method === 'software.check') return {manager:'apt',sources:['repo'],aurHelper:'',aurSupported:false,updates:0,counts:{repo:0,aur:0,flatpak:0},downloadSize:0,rebootNeeded:false,rebootPending:false,security:0,lastCheck:0,schedule:null,busy:false,warnings:[]};
        if (method === 'software.active') return null;
        return [];
      }
    `;
    const replaced = source.replace(/(async function mockCall\([^)]*\)\s*\{)/, `$1${injection}`);
    expect(replaced).not.toBe(source);
    await route.fulfill({ response, body: replaced });
  });
});

test('quick connectivity reopens from a real cached result and failed refresh retains it', async ({
  page,
}) => {
  await page.goto('/');
  const quick = page.getByRole('button', { name: 'Impostazioni rapide', exact: true });
  await quick.click();
  await expect(page.locator('.eos-connectivity')).toContainText('QA Wi-Fi');
  await page.locator('.eos-dismiss').click({ position: { x: 4, y: 4 } });
  await quick.click();
  await expect(page.locator('.eos-connectivity')).toContainText('QA Wi-Fi');
  expect(await page.evaluate(() => (window as any).__readProbe.calls['system.connectivity'])).toBe(
    1,
  );
  await page.evaluate(() => {
    (window as any).__readProbe.fail = true;
  });
  await page.locator('.eos-connectivity-heading button').click();
  await expect(page.locator('.eos-connectivity [role="alert"]')).toContainText(
    'QA refresh unavailable',
  );
  await expect(page.locator('.eos-connectivity')).toContainText('QA Wi-Fi');
});

test('software lazily reads catalogs once and shares fresh results between tabs', async ({
  page,
}) => {
  await page.goto('/software');
  await expect(page.locator('.eos-native-sidebar')).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => (window as any).__readProbe?.calls['software.updates'] ?? 0))
    .toBe(1);
  const before = await page.evaluate(() => (window as any).__readProbe.calls);
  expect(before['software.apps'] ?? 0).toBe(0);
  expect(before['software.installed'] ?? 0).toBe(0);
  expect(before['software.summary']).toBe(1);
  const navigation = page.locator('.eos-native-sidebar');
  await navigation.getByRole('button', { name: 'Trova software', exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => (window as any).__readProbe.calls['software.apps']))
    .toBe(1);
  await expect
    .poll(() => page.evaluate(() => (window as any).__readProbe.calls['software.installed']))
    .toBe(1);
  await page.waitForTimeout(250);
  await navigation.getByRole('button', { name: 'Aggiornamenti', exact: true }).click();
  await navigation.getByRole('button', { name: 'Trova software', exact: true }).click();
  await page.waitForTimeout(250);
  const after = await page.evaluate(() => (window as any).__readProbe.calls);
  expect(after['software.apps']).toBe(1);
  expect(after['software.installed']).toBe(1);
  expect(after['software.updates']).toBe(1);
  expect(after['software.summary']).toBe(1);
  expect(after['software.suggest']).toBe(1);
  // A real manual check still reaches the server; failures remain visible with cached data.
  await page.evaluate(() => {
    (window as any).__readProbe.fail = true;
  });
  await page.locator('.sw-bar').getByRole('button', { name: 'Controlla ora', exact: true }).click();
  await expect(page.locator('.sw-read-error')).toContainText('QA refresh unavailable');
  expect(await page.evaluate(() => (window as any).__readProbe.calls['software.check'])).toBe(1);
});
