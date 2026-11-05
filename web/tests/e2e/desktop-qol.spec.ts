import { expect, test, type Page } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    if (localStorage.getItem('desktop-qol-ready')) return;
    localStorage.setItem('desktop-qol-ready', '1');
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'en', theme: 'daylight' }),
    );
  });
  await page.goto('/');
  await expect(page.locator('.eos-desktop-grid')).toBeVisible();
});

const icons = (page: Page) => page.locator('.eos-desktop-grid > .eos-shortcut');
const stored = (page: Page) =>
  page.evaluate(
    () => JSON.parse(localStorage.getItem('ervisio.mock.prefs') || '{}')['shell.desktop'],
  );

test('single click selects, Ctrl adds, Shift selects a range, and keyboard selects all without opening apps', async ({
  page,
}) => {
  await icons(page).nth(0).click();
  await expect(page.locator('.eos-desktop-grid > .is-selected')).toHaveCount(1);
  await icons(page)
    .nth(2)
    .click({ modifiers: ['Control'] });
  await expect(page.locator('.eos-desktop-grid > .is-selected')).toHaveCount(2);
  await icons(page)
    .nth(4)
    .click({ modifiers: ['Shift'] });
  await expect(page.locator('.eos-desktop-grid > .is-selected')).toHaveCount(3);
  await expect(page.locator('.eos-window')).toHaveCount(0);
  await page.locator('.eos-desktop-grid').press('Control+a');
  await expect(page.locator('.eos-desktop-grid > .is-selected')).toHaveCount(
    await icons(page).count(),
  );
});

test('left-button rectangle selects its icons and clears when released', async ({ page }) => {
  const grid = await page.locator('.eos-desktop-grid').boundingBox();
  expect(grid).not.toBeNull();
  await page.mouse.move(grid!.x + 100, grid!.y + 220);
  await page.mouse.down();
  await page.mouse.move(grid!.x + 1, grid!.y + 1, { steps: 10 });
  await expect(page.locator('.eos-selection-rectangle')).toBeVisible();
  await expect(page.locator('.eos-desktop-grid > .is-selected')).toHaveCount(3);
  await page.mouse.up();
  await expect(page.locator('.eos-selection-rectangle')).toHaveCount(0);
  await expect(page.locator('.eos-window')).toHaveCount(0);
});

test('dragging selected icons moves the group to snapped cells and survives reload', async ({
  page,
}) => {
  const first = icons(page).nth(0),
    second = icons(page).nth(1);
  await first.click();
  await second.click({ modifiers: ['Control'] });
  const id = await first.getAttribute('data-desktop-item');
  const secondId = await second.getAttribute('data-desktop-item');
  await first.dragTo(page.locator('.eos-desktop-grid'), { targetPosition: { x: 350, y: 260 } });
  await expect.poll(async () => (await stored(page)).positions[id!]).toEqual({ column: 3, row: 2 });
  expect((await stored(page)).positions[secondId!]).toEqual({ column: 3, row: 3 });
  await page.reload();
  const moved = page.locator(`[data-desktop-item="${id}"]`);
  await expect(moved).toHaveCSS('left', '306px');
  await expect(moved).toHaveCSS('top', '208px');
  await moved.dblclick();
  await expect(page.getByRole('region', { name: 'This PC', exact: true })).toBeVisible();
});

test('grid reflows on narrow screens without horizontal overflow or rewriting saved coordinates', async ({
  page,
}) => {
  const first = icons(page).nth(0);
  const id = await first.getAttribute('data-desktop-item');
  await first.dragTo(page.locator('.eos-desktop-grid'), { targetPosition: { x: 1050, y: 260 } });
  await expect
    .poll(async () => (await stored(page)).positions[id!])
    .toEqual({ column: 10, row: 2 });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(first).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await stored(page)).positions[id!]).toEqual({ column: 10, row: 2 });
  await page.setViewportSize({ width: 1440, height: 900 });
  await expect(first).toHaveCSS('left', '1020px');
});
