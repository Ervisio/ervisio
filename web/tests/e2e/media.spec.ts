import { expect, test, type Page } from '@playwright/test';

const pixel = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l9sAAAAASUVORK5CYII=',
  'base64',
);
async function fixture(page: Page, shell: string) {
  await page.addInitScript((shell) => {
    if (localStorage.getItem('media.fixture')) return;
    localStorage.setItem('media.fixture', '1');
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem('ervisio.mock.home', JSON.stringify('/home/alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': shell, language: 'en', theme: 'daylight' }),
    );
    const entry = (name: string, mime: string) => ({
      name,
      type: 'file',
      mime,
      size: 100,
      mtime: 1,
      mode: '0644',
      perm: '-rw-r--r--',
      owner: 'alice',
      group: 'alice',
      uid: 1000,
      gid: 1000,
    });
    localStorage.setItem(
      'ervisio.mock.files',
      JSON.stringify({
        '/home/alice': [
          entry('one.png', 'image/png'),
          entry('two.jpg', 'image/jpeg'),
          entry('clip.mp4', 'video/mp4'),
        ],
      }),
    );
  }, shell);
  await page.route('**/api/files/media?**', async (route) => {
    const path = new URL(route.request().url()).searchParams.get('path');
    if (path?.endsWith('.mp4'))
      await route.fulfill({
        status: 200,
        contentType: 'video/mp4',
        body: Buffer.from('unsupported-codec-fixture'),
      });
    else await route.fulfill({ status: 200, contentType: 'image/png', body: pixel });
  });
}

for (const shell of ['classic', 'ervisios']) {
  test(`Photos opens from Explorer with real image controls and folder navigation in ${shell}`, async ({
    page,
  }) => {
    await fixture(page, shell);
    await page.goto('/files?path=/home/alice');
    await page.locator('[data-path="/home/alice/one.png"]').first().dblclick();
    const viewer = page.locator('.media-viewer');
    await expect(viewer).toBeVisible();
    await expect(viewer.locator('.media-photo')).toHaveJSProperty('naturalWidth', 1);
    await viewer.getByRole('button', { name: 'Zoom in', exact: true }).click();
    await expect(viewer.locator('.media-zoom-value')).toContainText('125%');
    await viewer.getByRole('button', { name: 'Rotate clockwise', exact: true }).click();
    await expect(viewer.locator('.media-photo')).toHaveAttribute('style', /rotate\(90deg\)/);
    await viewer.getByRole('button', { name: 'File information', exact: true }).click();
    const details = page.getByRole('dialog', { name: 'File information', exact: true });
    await expect(details).toContainText('/home/alice/one.png');
    await details.getByRole('button', { name: 'Close', exact: true }).click();
    await viewer.getByRole('button', { name: 'two.jpg', exact: true }).click();
    await expect(viewer.locator('.media-file-heading')).toContainText('two.jpg');
    await viewer.getByRole('button', { name: 'Show or hide filmstrip', exact: true }).click();
    await expect(viewer.locator('.media-filmstrip')).toHaveCount(0);
    await expect(viewer.getByRole('link', { name: 'Download', exact: true })).toHaveAttribute(
      'href',
      /path=%2Fhome%2Falice%2Ftwo.jpg/,
    );
    if (shell === 'ervisios') {
      expect(new URL(page.url()).pathname).toBe('/');
      await expect(page).toHaveTitle('Ervisio');
    }
    await page.screenshot({ path: `../.qa/media-${shell}.png` });
  });
  test(`Photos reports unsupported video codecs with a download fallback in ${shell}`, async ({
    page,
  }) => {
    await fixture(page, shell);
    await page.goto('/media?path=/home/alice/clip.mp4');
    const viewer = page.locator('.media-viewer');
    await expect(
      viewer.getByText('This file cannot be played here', { exact: true }),
    ).toBeVisible();
    await expect(
      viewer.getByRole('link', { name: 'Download', exact: true }).last(),
    ).toHaveAttribute('href', /clip.mp4/);
  });
}

test('Classic navigation hides editing utilities and exposes Task Manager without removing contextual editors', async ({
  page,
}) => {
  await fixture(page, 'classic');
  await page.goto('/files');
  await expect(page.getByRole('link', { name: 'Notepad', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Code editor', exact: true })).toHaveCount(0);
  await page.getByRole('link', { name: 'Task Manager', exact: true }).click();
  await expect(page.locator('.tm-root')).toBeVisible();
  expect(new URL(page.url()).pathname).toBe('/taskmanager');
});
