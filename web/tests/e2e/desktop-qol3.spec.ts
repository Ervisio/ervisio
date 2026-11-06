import { expect, test, type Locator, type Page } from '@playwright/test';

async function fixture(page: Page, shell = 'ervisios') {
  await page.addInitScript((mode) => {
    if (localStorage.getItem('qol3.fixture')) return;
    localStorage.setItem('qol3.fixture', '1');
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem('ervisio.mock.home', JSON.stringify('/srv/projects'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': mode, language: 'en', theme: 'daylight' }),
    );
    const entry = (name: string, type = 'file') => ({
      name,
      type,
      size: 15,
      mode: '0644',
      perm: '-rw-r--r--',
      owner: 'alice',
      group: 'alice',
      uid: 1000,
      gid: 1000,
      mtime: 1,
    });
    localStorage.setItem(
      'ervisio.mock.files',
      JSON.stringify({
        '/srv/projects': [entry('one.txt'), entry('target', 'dir')],
        '/srv/projects/target': [],
        '/': [],
      }),
    );
    localStorage.setItem(
      'ervisio.mock.texts',
      JSON.stringify({
        '/srv/projects/one.txt': { content: 'server configuration', mtime: 1, encoding: 'utf-8' },
      }),
    );
    localStorage.setItem(
      'ervisio.mock.disks',
      JSON.stringify([
        { device: '/dev/vda1', mount: '/', fstype: 'ext4', total: 1024, used: 512, avail: 512 },
      ]),
    );
    localStorage.setItem(
      'ervisio.mock.remotes',
      JSON.stringify([
        {
          id: '0123456789abcdef',
          name: 'Network archive',
          kind: 'sftp',
          host: 'example.invalid',
          path: '/projects',
        },
      ]),
    );
    localStorage.setItem(
      'ervisio.mock.remoteFiles',
      JSON.stringify({ '/projects': [entry('network.txt')], '/': [] }),
    );
    localStorage.setItem(
      'ervisio.mock.plugins',
      JSON.stringify([
        {
          id: 'minecraft',
          name: 'Minecraft',
          version: '1.0.0',
          entry: 'main.js',
          contributes: { pages: [{ id: 'servers', title: 'Minecraft' }] },
        },
      ]),
    );
  }, shell);
}

async function oneFocusRing(input: Locator) {
  await input.focus();
  const style = await input.evaluate((element) => {
    const field = getComputedStyle(element.closest('.ui-in')!);
    const control = getComputedStyle(element);
    return {
      outline: control.outlineStyle,
      shadow: control.boxShadow,
      parentShadow: field.boxShadow,
    };
  });
  expect(style.outline).toBe('none');
  expect(style.shadow).toBe('none');
  expect(style.parentShadow).toContain('2px');
}

for (const shell of ['classic', 'ervisios']) {
  test(`Shift Delete focuses confirmation and Enter deletes in ${shell}`, async ({ page }) => {
    await fixture(page, shell);
    await page.goto('/files?path=/srv/projects');
    const entry = page.locator('[data-path="/srv/projects/one.txt"]').first();
    await entry.click();
    await page.keyboard.press('Shift+Delete');
    const dialog = page.getByRole('alertdialog', { name: 'Delete 1 item for good?', exact: true });
    await expect(
      dialog.getByRole('button', { name: 'Delete for good', exact: true }),
    ).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(dialog).toHaveCount(0);
    await expect(entry).toHaveCount(0);
    expect(
      await page.evaluate(() =>
        JSON.parse(localStorage.getItem('ervisio.mock.files')!)['/srv/projects'].map(
          (e: { name: string }) => e.name,
        ),
      ),
    ).toEqual(['target']);
  });

  test(`software and project searches show one accessible focus ring in ${shell}`, async ({
    page,
  }) => {
    await fixture(page, shell);
    await page.goto('/software');
    await page
      .getByRole(shell === 'classic' ? 'tab' : 'button', { name: 'Find software', exact: true })
      .click();
    await oneFocusRing(
      page.getByRole('textbox', { name: 'Search apps and packages', exact: true }),
    );
    await page.goto('/editor?folder=/srv/projects');
    await page
      .getByRole('button', { name: 'Search in project (Ctrl+Shift+F)', exact: true })
      .click();
    await oneFocusRing(page.getByRole('textbox', { name: 'Search text in files', exact: true }));
  });

  test(`internal same-directory Ctrl-drop never uploads or copies, cross-folder drag works in ${shell}`, async ({
    page,
  }) => {
    await fixture(page, shell);
    await page.goto('/files?path=/srv/projects');
    await expect(page.locator('[data-path="/srv/projects/one.txt"]').first()).toBeVisible();
    const original = await page.evaluate(() => localStorage.getItem('ervisio.mock.files'));
    await page
      .locator('.files-pn')
      .first()
      .evaluate((element) => {
        const dataTransfer = new DataTransfer();
        dataTransfer.setData(
          'application/x-ervisio-files',
          JSON.stringify(['/srv/projects/one.txt']),
        );
        dataTransfer.items.add(new File(['should never upload'], 'one.txt'));
        for (const type of ['dragover', 'drop'])
          element.dispatchEvent(
            new DragEvent(type, { bubbles: true, cancelable: true, ctrlKey: true, dataTransfer }),
          );
      });
    await expect(page.locator('.files-dropmsg')).toHaveCount(0);
    await expect(page.getByRole('dialog', { name: /Upload/ })).toHaveCount(0);
    expect(await page.evaluate(() => localStorage.getItem('ervisio.mock.files'))).toBe(original);
    await page
      .locator('[data-path="/srv/projects/one.txt"]')
      .first()
      .dragTo(page.locator('[data-path="/srv/projects/target"]').first());
    await page.locator('[data-path="/srv/projects/target"]').first().dblclick();
    await expect(page.locator('[data-path="/srv/projects/target/one.txt"]').first()).toBeVisible();
  });

  test(`This PC disk menus omit project actions while actual folders retain them in ${shell}`, async ({
    page,
  }) => {
    await fixture(page, shell);
    await page.goto('/files');
    await page.locator('[data-computer-card="/"]').click({ button: 'right' });
    await expect(
      page.getByRole('menuitem', { name: 'Open folder in Code editor', exact: true }),
    ).toHaveCount(0);
    await page.keyboard.press('Escape');
    await page.locator('[data-remote-drive="0123456789abcdef"]').click({ button: 'right' });
    await expect(
      page.getByRole('menuitem', { name: 'Open folder in Code editor', exact: true }),
    ).toHaveCount(0);
    await page.keyboard.press('Escape');
    await page
      .locator('.files-places')
      .first()
      .getByRole('link', { name: 'Home', exact: true })
      .click();
    await page.locator('[data-path="/srv/projects/target"]').first().click({ button: 'right' });
    await expect(
      page.getByRole('menuitem', { name: 'Open folder in Code editor', exact: true }),
    ).toBeVisible();
  });
}

test('real local and saved-network folder shortcuts persist without creating or removing host directories', async ({
  page,
}) => {
  await fixture(page);
  await page.goto('/');
  await expect(page.locator('.eos-desktop-grid')).toBeVisible();
  const original = await page.evaluate(() => localStorage.getItem('ervisio.mock.files'));
  const add = async (name: string, path: string, remote = '') => {
    await page.locator('.eos-desktop-grid').click({ button: 'right', position: { x: 400, y: 50 } });
    await page.getByRole('menuitem', { name: 'New folder shortcut', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'New folder shortcut', exact: true });
    if (remote) await dialog.getByLabel('Location', { exact: true }).selectOption(remote);
    await dialog.getByLabel('Existing folder path').fill(path);
    await dialog.getByLabel('Shortcut name (optional)').fill(name);
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(dialog).toHaveCount(0);
  };
  await add('Projects shortcut', '/srv/projects');
  await add('Network shortcut', '/projects', '0123456789abcdef');
  expect(await page.evaluate(() => localStorage.getItem('ervisio.mock.files'))).toBe(original);
  await page.reload();
  const local = page.locator('.eos-directory-shortcut').filter({ hasText: 'Projects shortcut' });
  await expect(local).toBeVisible();
  await local.dblclick();
  await expect(page.locator('[data-path="/srv/projects/one.txt"]').first()).toBeVisible();
  await page
    .getByRole('region', { name: 'File Explorer', exact: true })
    .getByRole('button', { name: 'Minimize', exact: true })
    .click();
  await page.locator('.eos-directory-shortcut').filter({ hasText: 'Network shortcut' }).dblclick();
  await expect(
    page.locator('[data-path="remote:0123456789abcdef:/projects/network.txt"]').first(),
  ).toBeVisible();
  await page
    .getByRole('region', { name: 'File Explorer', exact: true })
    .getByRole('button', { name: 'Minimize', exact: true })
    .click();
  await local.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Remove from desktop', exact: true }).click();
  await expect(local).toHaveCount(0);
  expect(await page.evaluate(() => localStorage.getItem('ervisio.mock.files'))).toBe(original);
});

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 816, height: 590 },
  { width: 390, height: 844 },
]) {
  test(`taskbar menu anchors above its icon and stays in ${viewport.width}px viewport`, async ({
    page,
  }) => {
    await fixture(page);
    await page.setViewportSize(viewport);
    await page.goto('/');
    const icon = page.locator('.eos-task-app[aria-label="File Explorer"]');
    await icon.click({ button: 'right' });
    const menu = page.locator('.eos-context');
    await expect(menu).toBeVisible();
    const box = await menu.boundingBox(),
      button = await icon.boundingBox();
    expect(box).not.toBeNull();
    expect(button).not.toBeNull();
    expect(button!.y - box!.y - box!.height).toBeGreaterThanOrEqual(7);
    expect(button!.y - box!.y - box!.height).toBeLessThanOrEqual(9);
    expect(box!.x).toBeGreaterThanOrEqual(8);
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width - 7);
    expect(box!.y).toBeGreaterThanOrEqual(8);
    await page.keyboard.press('Escape');
    await page
      .locator('.eos-desktop-grid')
      .click({ button: 'right', position: { x: viewport.width - 60, y: viewport.height - 200 } });
    const edge = await menu.boundingBox();
    expect(edge!.x + edge!.width).toBeLessThanOrEqual(viewport.width - 7);
    expect(edge!.y + edge!.height).toBeLessThanOrEqual(viewport.height - 7);
  });
}

test('ErvisiOS consumes plugin deep links, isolates app navigation and keeps browser URL/title/history stable', async ({
  page,
}) => {
  await fixture(page);
  await page.route('**/plugin-frame/minecraft', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<!doctype html><title>Plugin fixture</title>',
    }),
  );
  await page.goto('/p/minecraft/servers');
  await expect(page.getByRole('region', { name: 'Minecraft', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/$/);
  await expect(page).toHaveTitle('Ervisio');
  const before = await page.evaluate(() => history.length);
  await page.locator('.eos-task-app[aria-label="Settings"]').click();
  await page.locator('[data-settings-category="language"]').click();
  await expect(page.locator('.os-settings-category[data-gid="language"]')).toBeVisible();
  await expect(page).toHaveURL(/\/$/);
  await expect(page).toHaveTitle('Ervisio');
  expect(await page.evaluate(() => history.length)).toBe(before);
  await page.evaluate(async () => {
    const module = '/src/shell/commands.ts';
    await (await import(module)).runShellCommand('Open terminal');
  });
  await expect(page.getByRole('region', { name: 'Terminal', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/$/);
  expect(await page.evaluate(() => history.length)).toBe(before);
  await page.keyboard.press('Control+k');
  const palette = page.getByRole('dialog', { name: 'Command palette', exact: true });
  await palette.getByRole('combobox').fill('services');
  await palette.getByRole('combobox').press('Enter');
  await expect(page.getByRole('region', { name: 'Services', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/$/);
  expect(await page.evaluate(() => history.length)).toBe(before);
  await page.evaluate(() => {
    history.pushState({}, '', '/editor?folder=%2Fsrv%2Fprojects');
    window.dispatchEvent(new PopStateEvent('popstate'));
  });
  await expect(page.getByRole('region', { name: 'Code editor', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/$/);
  await expect(page).toHaveTitle('Ervisio');
  await expect(page.locator('.eos-window')).toHaveCount(5);
});

test('Classic retains normal URLs and section titles', async ({ page }) => {
  await fixture(page, 'classic');
  await page.goto('/editor?folder=%2Fsrv%2Fprojects');
  await expect(page.locator('.editor-workspace--code')).toBeVisible();
  await expect(page).toHaveURL(/\/editor/);
  await expect(page).toHaveTitle('Code editor · Ervisio');
});
