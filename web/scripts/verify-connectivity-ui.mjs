// Read-only UI checks against VITE_MOCK=1. No host radio is changed.
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
const { chromium } = await import(process.env.ERVISIO_PLAYWRIGHT ? pathToFileURL(resolve(process.env.ERVISIO_PLAYWRIGHT, 'index.mjs')).href : 'playwright');
const browser = await chromium.launch({ headless: true, ...(process.env.ERVISIO_BROWSER_CHANNEL ? { channel: process.env.ERVISIO_BROWSER_CHANNEL } : {}) });
const output = resolve(process.env.ERVISIO_QA_OUTPUT ?? '.qa/connectivity');
await mkdir(output, { recursive: true });
const base = process.env.ERVISIO_UI_BASE ?? 'http://127.0.0.1:5173';
const radio = (enabled) => ({ present: true, supported: true, enabled, state: enabled ? 'on' : 'off', devices: [{ id: 'qa-radio', name: 'QA radio' }] });
try {
  for (const state of ['unavailable', 'supported', 'failed']) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await context.newPage();
    await page.addInitScript(({ state, radio }) => {
      localStorage.setItem('ervisio.mock.user', JSON.stringify('qa-user'));
      localStorage.setItem('ervisio.mock.prefs', JSON.stringify({ 'shell.mode': 'ervisios', language: 'en' }));
      if (state === 'failed') localStorage.setItem('ervisio.mock.connectivityError', JSON.stringify('QA radio query failed'));
      if (state === 'supported') localStorage.setItem('ervisio.mock.connectivity', JSON.stringify({ source: 'host', platform: 'linux', wifi: { ...radio.on, ssid: 'QA network' }, bluetooth: radio.off, interfaces: [{ id: 'enp0s3', name: 'enp0s3', type: 'ethernet', state: 'up', addresses: ['192.168.1.137/24'] }] }));
    }, { state, radio: { on: radio(true), off: radio(false) } });
    await page.goto(base);
    await page.getByRole('button', { name: 'Quick settings', exact: true }).click();
    const region = page.locator('.eos-connectivity');
    await region.getByText(/Radios on server arch/).waitFor();
    if (state === 'failed') {
      await region.getByRole('alert').getByText('QA radio query failed', { exact: true }).waitFor();
      await page.screenshot({ path: resolve(output, 'connectivity-failed.png') });
      await page.evaluate(() => localStorage.removeItem('ervisio.mock.connectivityError'));
      await region.getByRole('button', { name: 'Retry', exact: true }).click();
      await region.getByText('This server has no compatible radio hardware.', { exact: true }).first().waitFor();
    } else {
      const wifi = region.getByRole('switch', { name: 'Wi-Fi', exact: true });
      await wifi.waitFor();
      assert.equal(await wifi.getAttribute('aria-checked'), state === 'supported' ? 'true' : 'false');
      assert.equal(await wifi.isDisabled(), state === 'unavailable');
      assert.equal(await region.getByRole('switch', { name: 'Bluetooth', exact: true }).getAttribute('aria-checked'), 'false');
      await region.getByText('192.168.1.137/24', { exact: true }).waitFor();
      if (state === 'supported') await region.getByText('SSID: QA network', { exact: true }).waitFor();
      await page.screenshot({ path: resolve(output, `connectivity-${state}.png`) });
    }
    console.log(`connectivity ${state}: passed`);
    await context.close();
  }
  const loginContext = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const login = await loginContext.newPage();
  await login.addInitScript(() => {
    localStorage.setItem('ervisio.rememberUsers', '1');
    localStorage.setItem('ervisio.recentUsers', JSON.stringify([{ user: 'qa-user' }]));
    localStorage.setItem(`ervisio.login-shell:${JSON.stringify(['', 'qa-user'])}`, 'ervisios');
  });
  await login.goto(`${base}/login`);
  await login.locator('.login--ervisios').waitFor();
  await login.getByRole('textbox', { name: 'Password', exact: true }).waitFor().catch(() => login.locator('input[type=password]').waitFor());
  await login.screenshot({ path: resolve(output, 'ervisios-login.png') });
  assert.equal(await login.locator('.os-login-clock time').count(), 1);
  console.log('ErvisiOS real login form: passed');
  await loginContext.close();
} finally { await browser.close(); }
