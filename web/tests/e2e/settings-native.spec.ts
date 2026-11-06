import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    if (localStorage.getItem('settings.native.fixture')) return;
    localStorage.setItem('settings.native.fixture', '1');
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'it', theme: 'daylight' }),
    );
  });
});

test('native settings show the selected real category and search all authorized controls', async ({
  page,
}) => {
  await page.goto('/settings#language');
  await expect(page.locator('.os-settings')).toBeVisible();
  await expect(page.locator('.os-settings-category')).toHaveCount(1);
  await expect(page.locator('.os-settings-category[data-gid="language"]')).toBeVisible();
  expect(
    await page
      .locator('.os-settings-content')
      .evaluate((element) => element.scrollHeight <= element.clientHeight + 1),
  ).toBe(true);
  expect(
    await page
      .locator('.eos-window-body')
      .evaluate((element) => element.scrollHeight <= element.clientHeight + 1),
  ).toBe(true);
  await page.locator('.os-settings-nav [data-settings-category="appearance"]').click();
  await expect(page.locator('.os-settings-category')).toHaveCount(1);
  await expect(page.locator('.os-settings-category[data-gid="appearance"]')).toBeVisible();
  await expect(page).toHaveURL(/\/$/);
  expect(
    await page.locator('.os-settings-nav').evaluate((element) => {
      const search = element.querySelector('.os-settings-search')!.getBoundingClientRect();
      const nav = element.querySelector('nav')!.getBoundingClientRect();
      return nav.top - search.bottom < 20;
    }),
  ).toBe(true);
  await page.screenshot({ path: 'test-results/settings-native.png', animations: 'disabled' });
  await page.locator('.os-settings-search input').fill('root');
  await expect(page.locator('.os-settings-category[data-gid="signin"]')).toBeVisible();
  await expect(page.locator('.os-settings .st-row')).not.toHaveCount(0);
  await page.locator('.os-settings-nav [data-settings-category="terminal"]').click();
  await expect(page.locator('.os-settings-search input')).toHaveValue('');
  await expect(page.locator('.os-settings-category')).toHaveCount(1);
  await expect(page.locator('.os-settings-category[data-gid="terminal"]')).toBeVisible();
});

test('native settings fit small windows and preserve the classic settings layout', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/settings');
  await page
    .getByRole('combobox', { name: 'Categoria impostazioni' })
    .selectOption('notifications');
  await expect(page.locator('.os-settings-category[data-gid="notifications"]')).toBeVisible();
  expect(
    await page
      .locator('.os-settings')
      .evaluate((element) => element.scrollWidth <= element.clientWidth),
  ).toBe(true);
  await page.evaluate(() => {
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'classic', language: 'it', theme: 'daylight' }),
    );
  });
  await page.goto('/settings');
  await expect(page.locator('.st')).toBeVisible();
  await expect(page.locator('.os-settings')).toHaveCount(0);
  expect(await page.locator('.st [data-gid]').count()).toBeGreaterThan(5);
});
