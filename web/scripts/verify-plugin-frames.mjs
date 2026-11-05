// Run qa-plugin-frames.mjs first. Requires Playwright; set ERVISIO_PLAYWRIGHT to an installed package directory.
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const moduleName = process.env.ERVISIO_PLAYWRIGHT
  ? pathToFileURL(resolve(process.env.ERVISIO_PLAYWRIGHT, 'index.mjs')).href : 'playwright';
const { chromium } = await import(moduleName);
const browser = await chromium.launch({ headless: true, ...(process.env.ERVISIO_BROWSER_CHANNEL ? { channel: process.env.ERVISIO_BROWSER_CHANNEL } : {}) });
const output = resolve(process.env.ERVISIO_QA_OUTPUT ?? '.qa/plugin-frames');
await mkdir(output, { recursive: true });
const base = process.env.ERVISIO_QA_BASE ?? 'http://127.0.0.1:4177';
const surface = process.env.ERVISIO_QA_SURFACE === 'classic' ? 'classic' : 'ervisios';
const plugins = process.argv.length > 2 ? process.argv.slice(2) : ['docker', 'minecraft', 'caddy', 'ai'];
const results = [];
try {
  for (const id of plugins) {
    const context = await browser.newContext({ viewport: { width: 1100, height: 760 } });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await page.goto(`${base}/?plugin=${id}&surface=${surface}`);
    await page.waitForFunction(() => window.qa?.phase === 'loaded' || window.qa?.phase === 'failed', null, { timeout: 15_000 });
    const state = await page.evaluate(() => ({ phase: window.qa.phase, view: window.qa.view, errors: window.qa.errors }));
    assert.equal(state.phase, 'loaded', `${id}: ${state.errors.join('; ')}`);
    assert.equal(state.view.shell, surface);
    const frame = page.frameLocator('#frame');
    const nativeSelector = id === 'docker' ? '.dk-native-toolbar' : id === 'minecraft' ? '.mc-desktop' : id === 'caddy' ? '.cd-desktop-nav' : '.ai-root--ervisios';
    if (surface === 'ervisios') await frame.locator(nativeSelector).waitFor({ timeout: 15_000 });
    else { assert.equal(await frame.locator(nativeSelector).count(), 0); assert.equal(state.view.id, id); }
    if (surface === 'ervisios' && id === 'docker') {
      await frame.getByRole('button', { name: /^Images/ }).click();
      await frame.getByRole('heading', { name: 'Images', exact: true }).waitFor();
      await frame.getByRole('button', { name: /^Containers/ }).click();
      await frame.getByRole('heading', { name: 'Containers', exact: true }).waitFor();
      await frame.getByRole('button', { name: 'Refresh Docker data' }).click();
    }
    if (surface === 'ervisios' && id === 'minecraft') {
      await frame.getByRole('button', { name: /^Survival 1/ }).click();
      await frame.locator('.mc-console').waitFor();
      for (const name of ['Files', 'Worlds', 'Backups', 'Console']) {
        await frame.locator('.mc-main').getByRole('button', { name, exact: true }).click();
      }
      await frame.getByRole('button', { name: 'Resources', exact: true }).click();
      await frame.getByRole('heading', { name: 'Resources', exact: true }).waitFor();
      await frame.getByRole('button', { name: /^Servers/ }).click();
    }
    if (surface === 'ervisios' && id === 'caddy') {
      await frame.getByRole('button', { name: /^Caddy Running/ }).click();
      await frame.getByRole('button', { name: /^Caddyfile/ }).click();
      await frame.locator('textarea').first().waitFor();
      await frame.getByRole('button', { name: /^Certificates/ }).click();
      await frame.getByRole('button', { name: /^Logs/ }).click();
      await frame.getByRole('button', { name: /^Sites/ }).click();
    }
    if (surface === 'ervisios' && id === 'ai') {
      await frame.getByRole('textbox', { name: 'Message', exact: true }).fill('Open Settings');
      await frame.getByRole('button', { name: 'Send', exact: true }).click();
      await frame.getByText('Opening Settings.', { exact: true }).waitFor();
      const commands = await page.evaluate(() => window.qa.shell);
      assert.deepEqual(commands, ['Open Settings']);
      await page.evaluate(() => { window.qa.phase = 'starting'; document.querySelector('#frame').src = '/frame?reopen=1'; });
      await page.waitForFunction(() => window.qa.phase === 'loaded');
      await frame.getByText('Opening Settings.', { exact: true }).waitFor();
      await frame.getByRole('button', { name: 'Open Settings', exact: true }).waitFor();
    }
    const label = surface === 'ervisios' ? 'native' : 'classic';
    await page.screenshot({ path: resolve(output, `${id}-${label}.png`) });
    const layout = await frame.locator('body').evaluate((body) => ({ width: body.clientWidth, scrollWidth: body.scrollWidth, shell: document.documentElement.dataset.shell }));
    assert.equal(layout.shell, surface);
    assert.ok(layout.scrollWidth <= layout.width + 1, `${id}: horizontal overflow`);
    assert.deepEqual(errors, [], `${id}: browser errors`);
    await page.setViewportSize({ width: 640, height: 760 });
    await page.screenshot({ path: resolve(output, `${id}-${label}-narrow.png`) });
    const narrow = await frame.locator('body').evaluate((body) => ({ width: body.clientWidth, scrollWidth: body.scrollWidth }));
    assert.ok(narrow.scrollWidth <= narrow.width + 1, `${id}: narrow horizontal overflow`);
    results.push({ plugin: id, surface, phase: state.phase, view: state.view.id, errors, screenshot: resolve(output, `${id}-${label}.png`) });
    await context.close();
  }
  console.log(JSON.stringify(results, null, 2));
} finally { await browser.close(); }
