import { expect, test } from '@playwright/test';

test('This PC keeps its window identity, accepts legacy links and leaves the browser at the desktop URL', async ({
  page,
}) => {
  await page.addInitScript(() => {
    if (localStorage.getItem('thispc.workspace.fixture')) return;
    localStorage.setItem('thispc.workspace.fixture', '1');
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem('ervisio.mock.home', JSON.stringify('/home/alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({
        'shell.mode': 'ervisios',
        language: 'en',
        theme: 'daylight',
        'files.tabs': {
          landingVersion: 2,
          active: 'home',
          tabs: [
            {
              id: 'home',
              left: { loc: '/home/alice', view: 'grid' },
              right: { loc: '/home/alice', view: 'grid' },
              split: false,
            },
          ],
        },
      }),
    );
    localStorage.setItem('ervisio.mock.files', JSON.stringify({ '/home/alice': [] }));
  });
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/');
  await page.locator('.eos-shortcut').filter({ hasText: 'This PC' }).dblclick();
  const pc = page.getByRole('region', { name: 'This PC', exact: true });
  await expect(pc.locator('.files-computer')).toBeVisible();
  await expect(pc.locator('.eos-pc')).toHaveCount(0);
  await expect(page.locator('.eos-window')).toHaveCount(1);
  await expect(page).toHaveURL(/\/$/);
  await pc
    .locator('.files-places')
    .first()
    .getByRole('link', { name: 'Home', exact: true })
    .click();
  await expect(pc.locator('.files-pn')).toBeVisible();
  await expect(page.locator('.eos-window')).toHaveCount(1);
  await page.waitForTimeout(600); // Persist a non-Computer active tab before the legacy URL is reopened.
  await page.reload();
  await expect(page.locator('.eos-window')).toHaveCount(0);
  await page.goto('/?desktop=pc');
  await expect(pc.locator('.files-computer')).toBeVisible();
  await expect(page.locator('.eos-window')).toHaveCount(1);
  await pc.getByRole('button', { name: 'Minimize', exact: true }).click();
  await page.locator('.eos-task-app[aria-label="This PC"]').click();
  await expect(pc.locator('.files-computer')).toBeVisible();
  await page.locator('.eos-shortcut').filter({ hasText: 'File Explorer' }).dblclick();
  await expect(
    page.getByRole('region', { name: 'File Explorer', exact: true }).locator('.files-root'),
  ).toBeVisible();
  await expect(page.locator('.eos-window')).toHaveCount(2);
  expect(errors).toEqual([]);
});
