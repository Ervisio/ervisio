import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem('ervisio.mock.home', JSON.stringify('/home/alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'en', theme: 'daylight' }),
    );
  });
});

test('directory launch creates one session with an exact cwd and unifies tabs with window controls', async ({
  page,
}) => {
  const cwd = '/srv/customer folder/$example; project';
  await page.goto('/terminal?' + new URLSearchParams({ cwd }));
  const win = page.getByRole('region', { name: 'Terminal', exact: true });
  await expect(win).toBeVisible();
  await expect(win.locator('.eos-titlebar [role="tab"]')).toHaveCount(1);
  const sessions = await page.evaluate(() =>
    JSON.parse(localStorage.getItem('ervisio.mock.terminals') || '[]'),
  );
  expect(sessions).toHaveLength(1);
  expect(sessions[0].cwd).toBe(cwd);
  expect(sessions[0].admin).toBe(false);
  expect(sessions[0].cmd).toBeUndefined();
  await expect(win.locator('.eos-titlebar .eos-title')).toHaveCount(0);
  await expect(win.locator('.terminal-native-tabs')).toHaveCount(1);
  await expect(win.locator('.eos-window-body .terminal-native-tabs')).toHaveCount(0);
  const tab = await win.locator('.eos-titlebar [role="tab"]').boundingBox();
  const controls = await win.locator('.eos-window-controls').boundingBox();
  expect(tab).not.toBeNull();
  expect(controls).not.toBeNull();
  expect(Math.abs(tab!.y - controls!.y)).toBeLessThan(8);
  await expect(
    win.locator('.terminal-native-toolbar').getByRole('button', { name: 'Commands', exact: true }),
  ).toBeVisible();
  await win
    .locator('.terminal-native-toolbar')
    .getByRole('button', { name: 'Commands', exact: true })
    .click();
  await expect(win.getByRole('dialog', { name: 'Commands', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(win.getByRole('dialog', { name: 'Commands', exact: true })).toHaveCount(0);
});

test('the shared terminal titlebar drags the window while tab buttons remain interactive', async ({
  page,
}) => {
  await page.goto('/terminal');
  const win = page.getByRole('region', { name: 'Terminal', exact: true });
  await expect(win.locator('.eos-titlebar [role="tab"]')).toHaveCount(1);
  const before = await win.boundingBox();
  const titlebar = await win.locator('.eos-titlebar').boundingBox();
  expect(before).not.toBeNull();
  expect(titlebar).not.toBeNull();
  const x = titlebar!.x + titlebar!.width - 180,
    y = titlebar!.y + 20;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + 55, y + 40, { steps: 5 });
  await expect(win).toHaveClass(/is-dragging/);
  await page.mouse.up();
  const after = await win.boundingBox();
  expect(Math.round(after!.x - before!.x)).toBe(55);
  expect(Math.round(after!.y - before!.y)).toBe(40);
  await win
    .locator('.eos-titlebar')
    .getByRole('button', { name: 'New session', exact: true })
    .click();
  await expect(win.locator('.eos-titlebar [role="tab"]')).toHaveCount(2);
  const tabs = win.locator('.eos-titlebar [role="tab"]');
  await tabs.last().focus();
  await page.keyboard.press('ArrowLeft');
  await expect(tabs.first()).toBeFocused();
  await expect(tabs.first()).toHaveAttribute('aria-selected', 'true');
});

test('each explicit directory launch opens one fresh tab and consumes its nonce', async ({
  page,
}) => {
  const cwd = '/srv/same project';
  await page.goto('/terminal?' + new URLSearchParams({ cwd, launch: 'first' }));
  const win = page.getByRole('region', { name: 'Terminal', exact: true });
  await expect(win.locator('.eos-titlebar [role="tab"]')).toHaveCount(1);
  await expect(page).toHaveURL(/\/terminal$/);
  await page.evaluate(
    (to) => {
      window.dispatchEvent(
        new CustomEvent('ervisio:shell-action', { detail: { action: 'open-app', to } }),
      );
    },
    '/terminal?' + new URLSearchParams({ cwd, launch: 'second' }),
  );
  await expect(win.locator('.eos-titlebar [role="tab"]')).toHaveCount(2);
  await expect(page).toHaveURL(/\/terminal$/);
  const sessions = await page.evaluate(() =>
    JSON.parse(localStorage.getItem('ervisio.mock.terminals') || '[]'),
  );
  expect(sessions).toHaveLength(2);
  expect(sessions.every((session: { cwd: string }) => session.cwd === cwd)).toBe(true);
  await win.getByRole('button', { name: 'Minimize', exact: true }).click();
  await page.locator('.eos-task-app[aria-label="Terminal"]').click();
  await expect(win.locator('.eos-titlebar [role="tab"]')).toHaveCount(2);
});
