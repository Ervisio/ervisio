import { expect, test } from '@playwright/test';

for (const scenario of ['expired authentication', 'page reload']) {
  test(`public reset watcher survives ${scenario} and clears its read-only capability after completion`, async ({
    page,
  }) => {
    await page.addInitScript(() => {
      localStorage.removeItem('ervisio.mock.user');
      localStorage.setItem('ervisio.lang', 'en');
    });
    let ready = false,
      requests = 0;
    await page.route('**/api/maintenance/reset/status?**', async (route) => {
      requests++;
      await route.fulfill({
        status: ready ? 200 : 503,
        contentType: 'application/json',
        body: JSON.stringify(
          ready
            ? {
                id: '1'.repeat(32),
                state: 'done',
                phase: 'complete',
                nextURL: 'https://console.example:9090',
              }
            : { error: { code: 'unavailable' } },
        ),
      });
    });
    await page.goto(`/maintenance/reset#id=${'1'.repeat(32)}&token=${'2'.repeat(64)}`);
    await expect(page.locator('.set-reset-progress')).toBeVisible();
    await expect.poll(() => requests).toBeGreaterThan(0);
    await expect.poll(() => page.evaluate(() => location.hash)).toBe('');
    if (scenario === 'page reload') await page.reload();
    ready = true;
    await expect(page.getByText('Ervisio reset completed', { exact: true })).toBeVisible();
    await expect(page).toHaveURL(/\/maintenance\/reset$/);
    await expect(
      page.getByRole('link', { name: 'Open the console address', exact: true }),
    ).toHaveAttribute('href', 'https://console.example:9090');
    expect(
      await page.evaluate(() =>
        Object.keys(sessionStorage).some((key) => key.startsWith('ervisio.maintenance.reset:')),
      ),
    ).toBe(false);
  });
}
test('malformed job capabilities never trigger a progress request', async ({ page }) => {
  let requests = 0;
  await page.route('**/api/maintenance/reset/status?**', async (route) => {
    requests++;
    await route.abort();
  });
  await page.goto('/maintenance/reset#id=invalid&token=invalid');
  await expect(page.locator('.set-reset-progress')).toBeVisible();
  expect(requests).toBe(0);
});
