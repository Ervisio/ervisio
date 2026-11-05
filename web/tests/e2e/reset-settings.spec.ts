import { expect, test } from '@playwright/test';

for (const shell of ['classic', 'ervisios']) {
  test(`personal preference reset clears the account and browser UI settings, preserves credentials/files in ${shell}`, async ({
    page,
  }) => {
    await page.addInitScript((shell) => {
      if (localStorage.getItem('reset-fixture-ready')) return;
      localStorage.setItem('reset-fixture-ready', '1');
      localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
      localStorage.setItem(
        'ervisio.mock.prefs',
        JSON.stringify({
          'shell.mode': shell,
          language: 'en',
          theme: 'daylight',
          terminal: { fontSize: 24 },
          'files.bookmarks': ['/keep'],
        }),
      );
      localStorage.setItem('ervisio.term.layout', JSON.stringify({ open: ['old-session'] }));
      localStorage.setItem(
        'ervisio.mock.remoteText',
        JSON.stringify({ '/keep': 'data must stay' }),
      );
      localStorage.setItem('other.application', 'keep this');
    }, shell);
    await page.goto('/settings#reset');
    const controls = page.locator('.set-reset');
    await expect(controls).toBeVisible();
    await controls.getByRole('button', { name: 'Reset my preferences', exact: true }).click();
    const confirm = page.getByRole('alertdialog', { name: 'Reset my interface', exact: true });
    const action = confirm.getByRole('button', { name: 'Reset my preferences', exact: true });
    await expect(action).toBeDisabled();
    await confirm.getByRole('textbox').fill('alice');
    await action.click();
    await expect
      .poll(() =>
        page.evaluate(
          () => JSON.parse(localStorage.getItem('ervisio.mock.prefs') || '{}').terminal,
        ),
      )
      .toBeUndefined();
    await expect
      .poll(() => page.evaluate(() => localStorage.getItem('ervisio.term.layout')))
      .toBeNull();
    expect(await page.evaluate(() => localStorage.getItem('ervisio.mock.user'))).toBe('"alice"');
    expect(await page.evaluate(() => localStorage.getItem('other.application'))).toBe('keep this');
    expect(await page.evaluate(() => localStorage.getItem('ervisio.mock.remoteText'))).toContain(
      'data must stay',
    );
  });
}

test('factory reset shows exact paths and backup first, requires typed confirmation, and polls read-only status without authentication redirects', async ({
  page,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('root'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'en' }),
    );
  });
  let statusRequests = 0;
  await page.route('**/api/maintenance/reset/status?**', async (route) => {
    const url = new URL(route.request().url());
    expect(url.searchParams.get('token')).toBe('2'.repeat(64));
    statusRequests++;
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: '1'.repeat(32),
        state: 'done',
        phase: 'complete',
        nextURL: 'https://console.example:9090',
      }),
    });
  });
  await page.goto('/settings#reset');
  await page.getByRole('button', { name: 'Review full reset', exact: true }).click();
  const preview = page.getByRole('dialog', { name: 'Review Ervisio reset', exact: true });
  await expect(preview).toContainText('/fixture/ervisio/profiles');
  await expect(preview).toContainText('/fixture/protected-backups');
  await expect(preview).toContainText('/fixture/recovery.swap');
  expect(await page.evaluate(() => localStorage.getItem('ervisio.mock.resetApplied'))).toBeNull();
  await preview.getByRole('button', { name: 'Continue to confirmation', exact: true }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Reset all Ervisio data?', exact: true });
  const action = confirm.getByRole('button', { name: 'Back up and reset Ervisio', exact: true });
  await expect(action).toBeDisabled();
  await confirm.getByRole('textbox').fill('RESET ERVISIO');
  await action.click();
  await expect(page.getByText('Ervisio reset completed', { exact: true })).toBeVisible();
  expect(statusRequests).toBeGreaterThan(0);
  await expect(
    page.getByRole('link', { name: 'Open the console address', exact: true }),
  ).toHaveAttribute('href', 'https://console.example:9090');
});
