import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    if (localStorage.getItem('ervisios.e2e.ready')) return;
    localStorage.setItem('ervisios.e2e.ready', '1');
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'it', theme: 'daylight' }),
    );
  });
});

test('real apps open in independent windows and restore from the taskbar', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto('/');
  await expect(page.locator('.eos')).toBeVisible();
  await page.screenshot({ path: 'test-results/desktop.png' });
  await page.locator('.eos-shortcut').filter({ hasText: 'Questo PC' }).dblclick();
  await expect(page.getByRole('region', { name: 'Questo PC', exact: true })).toBeVisible();
  await page.locator('.eos-shortcut').filter({ hasText: 'Esplora risorse' }).dblclick();
  const explorer = page.getByRole('region', { name: 'Esplora risorse', exact: true });
  await expect(explorer).toBeVisible();
  await expect(page.locator('.eos-window')).toHaveCount(2);
  await explorer.getByRole('button', { name: 'Riduci a icona', exact: true }).click();
  await expect(explorer).toBeHidden();
  await page.locator('.eos-task-app[aria-label="Esplora risorse"]').click();
  await expect(explorer).toBeVisible();
  await page.screenshot({ path: 'test-results/explorer.png' });
  expect(errors).toEqual([]);
});

test('virtual folders survive reload and can contain app shortcuts', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Impostazioni rapide', exact: true }).click();
  await page.getByRole('button', { name: 'Nuova cartella', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Nome della cartella').fill('Lavoro');
  await dialog.getByRole('button', { name: 'Salva', exact: true }).click();
  await expect(page.locator('.eos-folder').filter({ hasText: 'Lavoro' })).toBeVisible();
  await page
    .locator('.eos-shortcut')
    .filter({ hasText: 'Esplora risorse' })
    .click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Sposta in cartella' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Lavoro', exact: true }).click();
  await page.reload();
  await page.locator('.eos-folder').filter({ hasText: 'Lavoro' }).dblclick();
  const folder = page.getByRole('region', { name: 'Lavoro', exact: true });
  await expect(
    folder.locator('.eos-shortcut').filter({ hasText: 'Esplora risorse' }),
  ).toBeVisible();
  await expect(
    page.locator('.eos-desktop-items .eos-shortcut').filter({ hasText: 'Esplora risorse' }),
  ).toHaveCount(0);
});

test('per-user shell switch preserves the desktop', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Start', exact: true }).click();
  await page.getByRole('button', { name: 'Passa all’interfaccia classica', exact: true }).click();
  await expect(page.locator('.app')).toBeVisible();
  await page.goto('/settings');
  await page.getByText('ErvisiOS', { exact: true }).click();
  await expect(page.locator('.eos')).toBeVisible();
  await expect(
    page.locator('.eos-desktop-items .eos-shortcut').filter({ hasText: 'Questo PC' }),
  ).toBeVisible();
});

test('desktop and start fit a touch viewport', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await page.getByRole('button', { name: 'Start', exact: true }).click();
  await expect(page.locator('.eos-panel--start')).toBeVisible();
  await page.locator('.eos-start-app').filter({ hasText: 'Questo PC' }).click();
  await expect(page.getByRole('region', { name: 'Questo PC', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.screenshot({ path: 'test-results/mobile.png' });
});

test('local app commands restore the same window without a provider', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.eos')).toBeVisible();
  const command = async (text: string) =>
    page.evaluate(async (value) => {
      const module = '/src/shell/commands.ts';
      return (await import(module)).runShellCommand(value);
    }, text);
  expect(await command('Apri questo PC')).toMatchObject({ handled: true });
  await expect(page.getByRole('region', { name: 'Questo PC', exact: true })).toBeVisible();
  expect(await command('Mostra desktop')).toMatchObject({ handled: true });
  await expect(page.locator('.eos-window:visible')).toHaveCount(0);
  expect(await command('Apri questo PC')).toMatchObject({ handled: true });
  await expect(page.getByRole('region', { name: 'Questo PC', exact: true })).toBeVisible();
  expect(await command('Analizza i log del server')).toEqual({ handled: false });
});

test('dropping computer files asks for a real server destination', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.eos')).toBeVisible();
  await page.locator('.eos-workspace').evaluate((element) => {
    const dataTransfer = new DataTransfer();
    dataTransfer.items.add(new File(['upload fixture'], 'documento.txt', { type: 'text/plain' }));
    element.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer }));
  });
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Carica 1 file');
  await expect(dialog.getByLabel('Cartella di destinazione')).toBeVisible();
  await expect(dialog).toContainText('documento.txt');
  await dialog.getByRole('button', { name: 'Annulla', exact: true }).click();
  await expect(dialog).toHaveCount(0);
});

test('apps can be dragged from Start onto the desktop', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Start', exact: true }).click();
  const source = page.locator('.eos-start-app').filter({ hasText: 'Servizi' });
  await source.dragTo(page.locator('.eos-workspace'), { force: true, targetPosition: { x: 300, y: 50 } });
  await expect(page.locator('.eos-desktop-items .eos-shortcut').filter({ hasText: 'Servizi' })).toBeVisible();
});

test('ErvisiOS sign-in uses real auth fields over the concept desktop', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => {
    localStorage.removeItem('ervisio.mock.user');
    localStorage.setItem(`ervisio.login-shell:${JSON.stringify(['', 'qa-user'])}`, 'ervisios');
  });
  await page.goto('/login');
  await page.locator('input[autocomplete="username"]').fill('qa-user');
  await expect(page.locator('.login--ervisios')).toBeVisible();
  await expect(page.locator('.os-login-clock')).toBeVisible();
  await expect(page.locator('input[autocomplete="current-password"]')).toBeVisible();
  await page.screenshot({ path: 'test-results/login-ervisios.png' });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: 'test-results/login-mobile.png' });
});
