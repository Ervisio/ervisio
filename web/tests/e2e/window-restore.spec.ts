import { expect, test, type Locator, type Page } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({
        'shell.mode': 'ervisios',
        language: 'it',
        theme: 'daylight',
        'shell.desktop': {
          version: 1,
          folders: [],
          shortcuts: [{ id: 'pc', app: 'this-pc', folder: null }],
          pinned: [],
          wallpaper: 'bloom',
          bounds: { 'this-pc': { x: 140, y: 90, width: 820, height: 520 } },
        },
      }),
    );
  });
  await page.goto('/');
  await page.locator('.eos-shortcut').filter({ hasText: 'Questo PC' }).dblclick();
  await expect(page.getByRole('region', { name: 'Questo PC', exact: true })).toBeVisible();
});

const geometry = async (window: Locator) => {
  const box = await window.boundingBox();
  if (!box) throw Error('Window has no geometry');
  return box;
};
const savedBounds = (page: Page) =>
  page.evaluate(
    () =>
      JSON.parse(localStorage.getItem('ervisio.mock.prefs') || '{}')['shell.desktop'].bounds[
        'this-pc'
      ],
  );

async function dock(page: Page, window: Locator, edge: 'top' | 'left' | 'right') {
  const bar = await geometry(window.locator('.eos-titlebar'));
  const host = await geometry(page.locator('.eos-workspace'));
  await page.mouse.move(bar.x + 200, bar.y + 20);
  await page.mouse.down();
  await page.mouse.move(
    edge === 'left'
      ? host.x + 2
      : edge === 'right'
        ? host.x + host.width - 2
        : host.x + host.width / 2,
    edge === 'top' ? host.y + 2 : host.y + 220,
    { steps: 8 },
  );
  await page.mouse.up();
  await expect(window).toHaveClass(
    new RegExp(edge === 'top' ? 'is-maximized' : `is-snapped-${edge}`),
  );
}

test('top-edge maximize restores the previous normal size below the pointer and continues the same drag', async ({
  page,
}) => {
  const window = page.getByRole('region', { name: 'Questo PC', exact: true });
  const normal = await geometry(window);
  await dock(page, window, 'top');
  const host = await geometry(page.locator('.eos-workspace'));
  const before = await savedBounds(page);
  const x = host.x + host.width * 0.6;
  const y = host.y + 20;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + 2, y + 2);
  await expect(window).toHaveClass(/is-maximized/);
  await page.mouse.move(host.x + 700, host.y + 180);
  await expect(window).not.toHaveClass(/is-maximized|is-snapped/);
  const restored = await geometry(window);
  expect(restored.width).toBeCloseTo(normal.width, 0);
  expect(restored.height).toBeCloseTo(normal.height, 0);
  expect(restored.x).toBeCloseTo(host.x + 700 - normal.width * 0.6, 0);
  expect(restored.y).toBeCloseTo(host.y + 160, 0);
  expect(await savedBounds(page)).toEqual(before);
  await page.mouse.move(host.x + 780, host.y + 220);
  const continued = await geometry(window);
  expect(continued.x - restored.x).toBeCloseTo(80, 0);
  expect(continued.y - restored.y).toBeCloseTo(40, 0);
  await page.mouse.up();
  await expect
    .poll(() => savedBounds(page))
    .toMatchObject({
      width: normal.width,
      height: normal.height,
      x: continued.x - host.x,
      y: continued.y - host.y,
    });
});

test('left and right edge docking both restore normal dimensions and keep the titlebar reachable', async ({
  page,
}) => {
  const window = page.getByRole('region', { name: 'Questo PC', exact: true });
  const normal = await geometry(window);
  const host = await geometry(page.locator('.eos-workspace'));
  for (const edge of ['left', 'right'] as const) {
    await dock(page, window, edge);
    const snapped = await geometry(window.locator('.eos-titlebar'));
    await page.mouse.move(snapped.x + snapped.width * 0.4, snapped.y + 20);
    await page.mouse.down();
    await page.mouse.move(host.x + 650, host.y + 220);
    await expect(window).not.toHaveClass(/is-snapped|is-maximized/);
    const restored = await geometry(window);
    expect(restored.width).toBeCloseTo(normal.width, 0);
    expect(restored.height).toBeCloseTo(normal.height, 0);
    expect(restored.x).toBeGreaterThanOrEqual(host.x);
    expect(restored.y).toBeGreaterThanOrEqual(host.y);
    await page.mouse.move(host.x + 700, host.y + 250);
    await page.mouse.up();
    const final = await geometry(window);
    expect(final.x - restored.x).toBeCloseTo(50, 0);
    expect(final.y - restored.y).toBeCloseTo(30, 0);
  }
});

test('clicking a maximized titlebar preserves it, and buttons or double-click restore the retained bounds', async ({
  page,
}) => {
  const window = page.getByRole('region', { name: 'Questo PC', exact: true });
  const normal = await geometry(window);
  await window.getByRole('button', { name: 'Ingrandisci', exact: true }).click();
  await expect(window).toHaveClass(/is-maximized/);
  const title = window.locator('.eos-title');
  await title.click();
  await expect(window).toHaveClass(/is-maximized/);
  await title.dblclick();
  await expect(window).not.toHaveClass(/is-maximized/);
  expect(await geometry(window)).toEqual(normal);
  await window.getByRole('button', { name: 'Ingrandisci', exact: true }).click();
  await window.getByRole('button', { name: 'Ripristina', exact: true }).click();
  expect(await geometry(window)).toEqual(normal);
});
