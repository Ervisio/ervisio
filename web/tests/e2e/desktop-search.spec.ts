import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'it', theme: 'daylight' }),
    );
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Start', exact: true }).click();
});

test('search selects the first result immediately and Enter opens it', async ({ page }) => {
  const input = page.getByRole('combobox', { name: 'Cerca app e plugin' });
  await expect(input).toBeFocused();
  await input.fill('impost');
  const selected = page.locator('.eos-panel--start').getByRole('option', { selected: true });
  await expect(selected).toHaveCount(1);
  await expect(selected).toHaveAttribute('aria-label', 'Impostazioni');
  await expect(input).toHaveAttribute(
    'aria-activedescendant',
    (await selected.getAttribute('id')) || '',
  );
  await input.press('Enter');
  await expect(page.getByRole('region', { name: 'Impostazioni', exact: true })).toBeVisible();
  await expect(page.locator('.eos-panel--start')).toHaveCount(0);
});

test('arrows choose another app and a new query or reopened launcher resets the selection', async ({
  page,
}) => {
  const input = page.getByRole('combobox');
  await input.fill('i');
  const launcher = page.locator('.eos-panel--start');
  const first = await launcher.getByRole('option').first().getAttribute('aria-label');
  await input.press('ArrowDown');
  const second = await launcher.getByRole('option', { selected: true }).getAttribute('aria-label');
  expect(second).not.toBe(first);
  await input.press('ArrowUp');
  await expect(launcher.getByRole('option', { selected: true })).toHaveAttribute(
    'aria-label',
    first || '',
  );
  await input.press('ArrowDown');
  await input.fill('term');
  await expect(launcher.getByRole('option', { selected: true })).toHaveAttribute(
    'aria-label',
    'Terminale',
  );
  await input.press('Escape');
  await page.getByRole('button', { name: 'Start', exact: true }).click();
  await expect(input).toHaveValue('');
  await expect(launcher.getByRole('option').first()).toHaveAttribute('aria-selected', 'true');
  await input.press('ArrowDown');
  const app = await launcher.getByRole('option', { selected: true }).getAttribute('aria-label');
  await input.press('Enter');
  await expect(page.getByRole('region', { name: app || '', exact: true })).toBeVisible();
});

test('no results and IME composition never open an app accidentally', async ({ page }) => {
  const input = page.getByRole('combobox');
  await input.fill('zzzz-no-result');
  await expect(page.locator('.eos-panel--start').getByRole('option')).toHaveCount(0);
  await expect(input).not.toHaveAttribute('aria-activedescendant');
  await input.press('ArrowDown');
  await input.press('Enter');
  await expect(page.locator('.eos-panel--start')).toBeVisible();
  await expect(page.locator('.eos-window')).toHaveCount(0);
  await input.fill('term');
  await input.dispatchEvent('keydown', { key: 'Enter', isComposing: true });
  await expect(page.locator('.eos-panel--start')).toBeVisible();
  await expect(page.locator('.eos-window')).toHaveCount(0);
});
