import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page, shell: string) {
  await page.addInitScript((shell) => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem('ervisio.mock.home', JSON.stringify('/srv/project'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({
        'shell.mode': shell,
        language: 'en',
        theme: 'daylight',
        'shell.desktop': {
          version: 1,
          folders: [{ id: 'apps', name: 'Apps', parent: null }],
          directoryShortcuts: [
            { id: 'project-link', name: 'Project link', path: '/srv/project', folder: null },
          ],
        },
      }),
    );
    const entry = (name: string, type = 'file') => ({
      name,
      type,
      size: 12,
      mtime: 1,
      mode: '0644',
      perm: 'rw-r--r--',
      owner: 'alice',
      group: 'alice',
    });
    localStorage.setItem(
      'ervisio.mock.files',
      JSON.stringify({
        '/srv/project': [entry('src', 'dir'), entry('one.txt')],
        '/srv/project/src': [],
        '/': [],
      }),
    );
    localStorage.setItem(
      'ervisio.mock.texts',
      JSON.stringify({
        '/srv/project/one.txt': { content: 'original', mtime: 1, encoding: 'utf-8' },
      }),
    );
  }, shell);
}

for (const shell of ['classic', 'ervisios']) {
  test(`shared chooser opens files and selects a project folder in ${shell}`, async ({ page }) => {
    await fixture(page, shell);
    await page.goto('/editor');
    await page.getByRole('button', { name: 'Open folder', exact: true }).first().click();
    const picker = page.locator('.file-picker');
    await picker.getByRole('option').filter({ hasText: 'src' }).click();
    await picker.getByRole('button', { name: 'Choose', exact: true }).click();
    await expect(picker).toHaveCount(0);
    await expect(page.locator('.editor-tree-root b')).toHaveAttribute('title', '/srv/project/src');
    await page.getByRole('button', { name: 'Open', exact: true }).first().click();
    await picker.getByRole('textbox', { name: 'Folder path', exact: true }).fill('/srv/project');
    await picker.getByRole('button', { name: 'Go', exact: true }).click();
    await picker.getByRole('option').filter({ hasText: 'one.txt' }).click();
    await picker.getByRole('button', { name: 'Choose', exact: true }).click();
    await expect(picker).toHaveCount(0);
    await expect(page.locator('.cm-content')).toContainText('original');
  });
  test(`Save As requires confirmation before replacing an existing file in ${shell}`, async ({
    page,
  }) => {
    await fixture(page, shell);
    await page.goto('/notepad?path=/srv/project/one.txt');
    await expect(page.locator('.editor-notepad')).toBeVisible();
    await page.getByRole('button', { name: /^Save as/ }).click();
    const picker = page.locator('.file-picker');
    await picker.getByRole('textbox', { name: 'Name', exact: true }).fill('one.txt');
    await picker.getByRole('button', { name: 'Save', exact: true }).click();
    // Saving to the current path uses its revision and needs no replacement prompt.
    await expect(picker).toHaveCount(0);
    await page.evaluate(() => {
      const files = JSON.parse(localStorage.getItem('ervisio.mock.files')!);
      files['/srv/project'].push({ name: 'other.txt', type: 'file', size: 5, mtime: 1 });
      localStorage.setItem('ervisio.mock.files', JSON.stringify(files));
      const texts = JSON.parse(localStorage.getItem('ervisio.mock.texts')!);
      texts['/srv/project/other.txt'] = { content: 'keep me', mtime: 1, encoding: 'utf-8' };
      localStorage.setItem('ervisio.mock.texts', JSON.stringify(texts));
    });
    await page.getByRole('button', { name: /^Save as/ }).click();
    await picker.getByRole('textbox', { name: 'Name', exact: true }).fill('other.txt');
    await picker.getByRole('button', { name: 'Save', exact: true }).click();
    const replace = page.getByRole('dialog', { name: 'Replace this file?', exact: true });
    await expect(replace).toBeVisible();
    expect(
      await page.evaluate(
        () =>
          JSON.parse(localStorage.getItem('ervisio.mock.texts')!)['/srv/project/other.txt'].content,
      ),
    ).toBe('keep me');
    await replace.getByRole('button', { name: 'Cancel', exact: true }).click();
    await picker.getByRole('button', { name: 'Cancel', exact: true }).click();
  });
  test(`F2 renames in place and Escape cancels in ${shell}`, async ({ page }) => {
    await fixture(page, shell);
    await page.goto('/files?path=/srv/project');
    const folder = page.locator('[data-path="/srv/project/src"]').first();
    await folder.click();
    await page.keyboard.press('F2');
    const name = page.getByRole('textbox', { name: 'Rename', exact: true });
    await expect(name).toBeVisible();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await name.fill('cancelled');
    await name.press('Escape');
    await expect(folder).toBeVisible();
    await folder.click();
    await page.keyboard.press('F2');
    await name.fill('renamed');
    await name.press('Enter');
    await expect(page.locator('[data-path="/srv/project/renamed"]').first()).toBeVisible();
    await expect(name).toHaveCount(0);
  });
  test(`Notepad Save As browses and saves to selected folder in ${shell}`, async ({ page }) => {
    await fixture(page, shell);
    await page.goto('/notepad?path=/srv/project/one.txt');
    await expect(page.locator('.editor-notepad')).toBeVisible();
    await page.getByRole('button', { name: /^Save as/ }).click();
    const picker = page.locator('.file-picker');
    await expect(picker).toBeVisible();
    await picker.getByRole('option').filter({ hasText: 'src' }).dblclick();
    await expect(picker.getByRole('textbox', { name: 'Folder path', exact: true })).toHaveValue(
      '/srv/project/src',
    );
    await picker.getByRole('textbox', { name: 'Name', exact: true }).fill('copy.txt');
    await picker.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(picker).toHaveCount(0);
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            JSON.parse(localStorage.getItem('ervisio.mock.texts')!)['/srv/project/src/copy.txt']
              ?.content,
        ),
      )
      .toBe('original');
  });
}

test('desktop virtual folders and folder shortcuts rename inline with F2', async ({ page }) => {
  await fixture(page, 'ervisios');
  await page.goto('/');
  const folder = page.locator('.eos-folder').filter({ hasText: 'Apps' });
  await folder.click();
  await page.keyboard.press('F2');
  const name = page.getByRole('textbox', { name: 'Rename', exact: true });
  await name.fill('Tools');
  await name.press('Enter');
  await expect(page.locator('.eos-folder').filter({ hasText: 'Tools' })).toBeVisible();
  await page.locator('.eos-directory-shortcut').click();
  await page.keyboard.press('F2');
  await name.fill('My project');
  await name.press('Enter');
  await expect(page.locator('.eos-directory-shortcut')).toContainText('My project');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(
    await page.evaluate(() => Object.keys(JSON.parse(localStorage.getItem('ervisio.mock.files')!))),
  ).toContain('/srv/project');
});

test('desktop Delete is immediate and Ctrl+Z expires after three minutes', async ({ page }) => {
  await fixture(page, 'ervisios');
  await page.goto('/');
  const folder = page.locator('.eos-folder').filter({ hasText: 'Apps' });
  await folder.click();
  await page.keyboard.press('Delete');
  await expect(folder).toHaveCount(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.keyboard.press('Control+z');
  await expect(folder).toBeVisible();
  await folder.click();
  await page.keyboard.press('Delete');
  await expect(folder).toHaveCount(0);
  await page.clock.install();
  await page.clock.setSystemTime(Date.now() + 181_000);
  await page.keyboard.press('Control+z');
  await expect(folder).toHaveCount(0);
});
