import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    if (localStorage.getItem('files.native.fixture')) return;
    localStorage.setItem('files.native.fixture', '1');
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem('ervisio.mock.home', JSON.stringify('/home/alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'en', theme: 'daylight' }),
    );
    const entry = (name: string, type = 'file') => ({
      name,
      type,
      size: 0,
      mtime: 1730000000000,
      mode: type === 'dir' ? 'drwxr-xr-x' : '-rw-r--r--',
      perm: type === 'dir' ? '755' : '644',
      owner: 'alice',
      group: 'alice',
      uid: 1000,
      gid: 1000,
    });
    localStorage.setItem(
      'ervisio.mock.files',
      JSON.stringify({
        '/home/alice': [
          entry('Documents', 'dir'),
          entry('README.txt'),
          entry('report.pdf'),
          entry('photo.png'),
          entry('app.ts'),
          entry('archive.zip'),
        ],
        '/home/alice/Documents': [entry('inside.txt')],
      }),
    );
  });
});

test('native Properties is a compact centered popup and selection never opens a side panel', async ({
  page,
}) => {
  await page.goto('/files?path=/home/alice');
  const files = page.locator('.files-native');
  const readme = files.locator('[data-path="/home/alice/README.txt"]');
  await readme.click();
  await expect(readme).toHaveClass(/is-sel/);
  await expect(page.locator('.ui-panel')).toHaveCount(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await readme.click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Rename', exact: false })).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Copy', exact: false }).first()).toBeVisible();
  const workspaceBefore = await files.locator('.files-native-workspace').boundingBox();
  await page.getByRole('menuitem', { name: 'Properties', exact: true }).click();
  const properties = page.getByRole('dialog', { name: 'Properties: README.txt', exact: true });
  await expect(properties).toBeVisible();
  await expect(page.locator('.ui-panel')).toHaveCount(0);
  const popup = await properties.boundingBox();
  const viewport = page.viewportSize()!;
  expect(popup).not.toBeNull();
  expect(popup!.width).toBeLessThanOrEqual(520);
  expect(Math.abs(popup!.x + popup!.width / 2 - viewport.width / 2)).toBeLessThanOrEqual(3);
  expect(Math.abs(popup!.y + popup!.height / 2 - viewport.height / 2)).toBeLessThanOrEqual(3);
  const workspaceDuring = await files.locator('.files-native-workspace').boundingBox();
  expect(workspaceBefore).not.toBeNull();
  expect(workspaceDuring).not.toBeNull();
  expect(Math.abs(workspaceDuring!.width - workspaceBefore!.width)).toBeLessThanOrEqual(1);
  await page.screenshot({ path: 'test-results/files-properties-popup.png' });
  await page.keyboard.press('Escape');
  await expect(properties).toHaveCount(0);
  await files.locator('[data-path="/home/alice/report.pdf"]').click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await readme.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Properties', exact: true }).click();
  await properties
    .locator('.ui-dlg-ft')
    .getByRole('button', { name: 'Close', exact: true })
    .click();
  await expect(properties).toHaveCount(0);
  await files.locator('[data-path="/home/alice/Documents"]').dblclick();
  await expect(files.locator('[data-path="/home/alice/Documents/inside.txt"]')).toBeVisible();
  await expect(page.locator('.ui-panel')).toHaveCount(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('native list shares folder and extension icons, selects without preview and opens folders on double-click', async ({
  page,
}) => {
  await page.goto('/files?path=/home/alice');
  const files = page.locator('.files-native');
  await files.getByRole('radio', { name: 'List', exact: true }).click();
  const folder = files.locator('tr[data-path="/home/alice/Documents"]');
  const pdf = files.locator('tr[data-path="/home/alice/report.pdf"]');
  await expect(
    folder.locator('.files-os-icon[data-file-kind="dir"] .files-os-folder'),
  ).toBeVisible();
  await expect(pdf.locator('.files-os-icon[data-file-ext="PDF"]')).toBeVisible();
  await expect(
    files.locator(
      'tr[data-path="/home/alice/app.ts"] .files-os-icon[data-file-kind="code"][data-file-ext="TS"]',
    ),
  ).toBeVisible();
  await pdf.click();
  await expect(pdf).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('.ui-panel')).toHaveCount(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await folder.dblclick();
  await expect(files.locator('tr[data-path="/home/alice/Documents/inside.txt"]')).toBeVisible();
  await expect(page.locator('.ui-panel')).toHaveCount(0);
});

test('classic file selection retains its existing automatic details panel', async ({ page }) => {
  await page.goto('/files?path=/home/alice');
  await page.evaluate(() =>
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'classic', language: 'en', theme: 'daylight' }),
    ),
  );
  await page.reload();
  const files = page.locator('.files-root');
  await expect(files).not.toHaveClass(/files-native/);
  await files
    .locator('.files-places')
    .first()
    .getByRole('link', { name: 'Home', exact: true })
    .click();
  await files.locator('[data-path="/home/alice/README.txt"]').click();
  await expect(page.getByRole('complementary', { name: 'README.txt', exact: true })).toBeVisible();
  await expect(files.locator('.files-os-icon')).toHaveCount(0);
});

test('Properties inspects the inactive split pane target after right-click activates it', async ({
  page,
}) => {
  await page.goto('/files?path=/home/alice');
  const files = page.locator('.files-native');
  await files.getByRole('button', { name: 'Split view', exact: true }).click();
  const left = files.locator('.files-pn').nth(0);
  const right = files.locator('.files-pn').nth(1);
  await right.locator('[data-path="/home/alice/Documents"]').dblclick();
  await left.locator('[data-path="/home/alice/README.txt"]').click();
  await expect(left).toHaveClass(/is-act/);
  await right.locator('[data-path="/home/alice/Documents/inside.txt"]').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Properties', exact: true }).click();
  await expect(
    page.getByRole('dialog', { name: 'Properties: inside.txt', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.ui-panel')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('native Properties popup fits a phone viewport and closes from its footer', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/files?path=/home/alice');
  const files = page.locator('.files-native');
  const readme = files.locator('[data-path="/home/alice/README.txt"]');
  await readme.click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const workspaceBefore = await files.locator('.files-native-workspace').boundingBox();
  await readme.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Properties', exact: true }).click();
  const properties = page.getByRole('dialog', { name: 'Properties: README.txt', exact: true });
  await expect(properties).toBeVisible();
  const popup = await properties.boundingBox();
  expect(popup).not.toBeNull();
  expect(popup!.width).toBeLessThanOrEqual(390 - 24);
  expect(popup!.height).toBeLessThanOrEqual(844 - 24);
  expect(popup!.x).toBeGreaterThanOrEqual(12);
  expect(popup!.y).toBeGreaterThanOrEqual(12);
  const workspaceDuring = await files.locator('.files-native-workspace').boundingBox();
  expect(Math.abs(workspaceDuring!.width - workspaceBefore!.width)).toBeLessThanOrEqual(1);
  await expect(page.locator('.ui-panel')).toHaveCount(0);
  await page.screenshot({ path: 'test-results/files-properties-mobile.png' });
  await properties
    .locator('.ui-dlg-ft')
    .getByRole('button', { name: 'Close', exact: true })
    .click();
  await expect(properties).toHaveCount(0);
});
