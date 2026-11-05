// Real, isolated development-host acceptance. Never points at a remote or production server.
// Requires --dev-insecure-noauth bootstrapped storageState, a no-mock Vite instance and both binaries.
import assert from 'node:assert/strict';
import { mkdir, readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import { chromium } from 'playwright';

const core = fileURLToPath(new URL('../..', import.meta.url));
const target = resolve(core, '.qa/native-host/upload-target');
const state = process.env.ERVISIO_QA_STATE ?? resolve(core, '.qa/native-host/browser-state.json');
const base = process.env.ERVISIO_HOST_QA_BASE ?? 'http://127.0.0.1:5174';
assert.ok(
  ['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname),
  'QA must use a loopback development host',
);
await mkdir(target, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  ...(process.env.PLAYWRIGHT_CHANNEL ? { channel: process.env.PLAYWRIGHT_CHANNEL } : {}),
});
try {
  const context = await browser.newContext({
    storageState: state,
    viewport: { width: 1440, height: 960 },
    reducedMotion: 'reduce',
  });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto(base);
  await page.waitForSelector('.eos');
  const real = await page.evaluate(async () => {
    const path = '/src/api/http.ts';
    return !(await import(path)).MOCK;
  });
  assert.equal(real, true, 'Must verify the real daemon, not VITE_MOCK');
  await page.evaluate(async () => {
    const path = '/src/api/client.ts';
    const { call } = await import(path);
    await call('prefs.set', { key: 'language', value: 'it' });
  });
  await page.reload();
  await page.waitForSelector('.eos');
  const filename = `ervisios-upload-${Date.now()}.txt`;
  const content = 'ErvisiOS real upload acceptance\n';
  await page.locator('.eos-workspace').evaluate(
    (element, file) => {
      const dataTransfer = new DataTransfer();
      dataTransfer.items.add(new File([file.content], file.filename, { type: 'text/plain' }));
      element.dispatchEvent(
        new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer }),
      );
    },
    { filename, content },
  );
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Cartella di destinazione').fill(target);
  await dialog.getByRole('button', { name: 'Carica', exact: true }).click();
  await page.waitForFunction(() => !document.querySelector('.ui-dlg'), null, { timeout: 15000 });
  let uploaded = '';
  for (let i = 0; i < 40; i += 1) {
    try {
      uploaded = await readFile(resolve(target, filename), 'utf8');
    } catch {
      /* still transferring */
    }
    if (uploaded === content) break;
    await new Promise((done) => setTimeout(done, 100));
  }
  assert.equal(
    uploaded,
    content,
    'Real transfer must write the file in the selected server folder',
  );
  await page.getByRole('region', { name: 'Esplora risorse', exact: true }).waitFor();
  await page
    .locator('.files-name')
    .filter({ hasText: filename })
    .first()
    .waitFor({ timeout: 15000 })
    .catch(async () => {
      assert.ok(
        (await page.locator('.eos-window-body').innerText()).includes(filename),
        'Uploaded file must appear in Explorer',
      );
    });
  const download = await page.evaluate(
    async (path) => {
      const module = '/src/api/client.ts';
      return (await import(module)).downloadUrl(path);
    },
    resolve(target, filename),
  );
  const response = await context.request.get(new URL(download, base).href);
  assert.ok(response.ok(), `Download status ${response.status()}`);
  assert.equal(await response.text(), content, 'Real download must match uploaded bytes');
  await page.screenshot({ path: resolve(core, '.qa/native-host/explorer-upload.png') });
  assert.deepEqual(errors, []);
  console.log(
    JSON.stringify(
      {
        realDaemon: true,
        upload: 'passed',
        download: 'passed',
        explorer: 'passed',
        errors,
        target: resolve(target, filename),
      },
      null,
      2,
    ),
  );
  await context.close();
} finally {
  await browser.close();
}
