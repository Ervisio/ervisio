import { expect, test } from '@playwright/test';

test('a failed lazy app download offers a full reload and recovers once transport returns', async ({
  page,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'it' }),
    );
  });
  const appSource = '**/src/sections/software/index.tsx';
  await page.route(appSource, (route) => route.abort('connectionfailed'));
  await page.goto('/software');
  const error = page.locator('.eos-app-error');
  await expect(error).toBeVisible();
  await error.getByText('Dettagli dell’errore', { exact: true }).click();
  await expect(error.locator('pre')).toContainText('module');
  await page.unroute(appSource);
  await error.getByRole('button', { name: 'Ricarica interfaccia', exact: true }).click();
  await expect(page.locator('.eos-native-frame')).toBeVisible();
  await expect(page.locator('.eos-app-error')).toHaveCount(0);
  await expect(page).toHaveURL(/\/$/);
  await expect(page).toHaveTitle('Ervisio');
  await page.reload();
  await expect(page.locator('.eos-desktop-grid')).toBeVisible();
  await expect(page.locator('.eos-window')).toHaveCount(0);
});
