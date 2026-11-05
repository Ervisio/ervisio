import { expect, test } from '@playwright/test';

test('Notepad tabs occupy the ErvisiOS titlebar and preserve real window dragging and close protection', async ({
  page,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'en' }),
    );
  });
  await page.goto('/notepad');
  const window = page.getByRole('region', { name: 'Notepad', exact: true });
  const bar = window.locator('.notepad-window-titlebar');
  await expect(bar.getByRole('tablist', { name: 'Documents' })).toBeVisible();
  await expect(window.locator('.editor-workspace .editor-tabs')).toHaveCount(0);
  await expect(bar.locator('.eos-title')).toHaveCount(0);
  await bar.getByRole('button', { name: 'New tab', exact: true }).click();
  await expect(bar.getByRole('tab')).toHaveCount(2);
  const before = await window.boundingBox();
  const area = await bar.boundingBox();
  await page.mouse.move(area!.x + area!.width - 160, area!.y + 20);
  await page.mouse.down();
  await page.mouse.move(area!.x + area!.width - 115, area!.y + 70, { steps: 5 });
  await page.mouse.up();
  const after = await window.boundingBox();
  expect(after!.x).toBeGreaterThan(before!.x);
  await window.getByRole('textbox', { name: 'Notepad text', exact: true }).fill('Unsaved note');
  await bar.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Keep your changes?', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await expect(window).toBeVisible();
});

test('classic Notepad retains its own document tabs without a desktop titlebar', async ({
  page,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'classic', language: 'en' }),
    );
  });
  await page.goto('/notepad');
  await expect(page.locator('.editor-workspace .editor-tabs')).toBeVisible();
  await expect(page.locator('.notepad-window-titlebar')).toHaveCount(0);
});
