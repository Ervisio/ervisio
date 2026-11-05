import { expect, test, type Page } from '@playwright/test';

async function fixture(
  page: Page,
  shell: string,
  user = 'alice',
  elevated = false,
  legacy = false,
) {
  await page.addInitScript(
    ({ shell, user, elevated, legacy }) => {
      if (localStorage.getItem('files.qol2.fixture')) return;
      localStorage.setItem('files.qol2.fixture', '1');
      localStorage.setItem('ervisio.mock.user', JSON.stringify(user));
      localStorage.setItem('ervisio.mock.home', JSON.stringify('/home/alice'));
      const prefs: Record<string, unknown> = {
        'shell.mode': shell,
        language: 'en',
        theme: 'daylight',
      };
      if (legacy)
        prefs['files.tabs'] = {
          active: 'old',
          tabs: [
            {
              id: 'old',
              left: { loc: '/var/log', view: 'list' },
              right: { loc: '/home/alice', view: 'grid' },
              split: false,
            },
          ],
        };
      localStorage.setItem('ervisio.mock.prefs', JSON.stringify(prefs));
      const entry = (name: string, type = 'file') => ({
        name,
        type,
        size: 15,
        mode: type === 'dir' ? '0755' : '0644',
        perm: type === 'dir' ? 'drwxr-xr-x' : '-rw-r--r--',
        owner: 'alice',
        group: 'alice',
        uid: 1000,
        gid: 1000,
        mtime: 1,
      });
      localStorage.setItem(
        'ervisio.mock.files',
        JSON.stringify({
          '/home/alice': [
            entry('one.txt'),
            entry('two.txt'),
            entry('Copy', 'dir'),
            entry('Move', 'dir'),
          ],
          '/home/alice/Copy': [],
          '/home/alice/Move': [],
          '/var/log': [],
        }),
      );
      localStorage.setItem(
        'ervisio.mock.texts',
        JSON.stringify({
          '/home/alice/one.txt': { content: 'first', mtime: 1, encoding: 'utf-8' },
        }),
      );
      if (elevated)
        localStorage.setItem('ervisio.mock.files.requireAdmin', JSON.stringify(['/home/alice']));
      localStorage.setItem(
        'ervisio.mock.disks',
        JSON.stringify([
          {
            device: '/dev/vda1',
            mount: '/',
            fstype: 'ext4',
            total: 1073741824,
            used: 536870912,
            avail: 536870912,
          },
        ]),
      );
      localStorage.setItem(
        'ervisio.mock.remotes',
        JSON.stringify([
          {
            id: '0123456789abcdef',
            name: 'Storage Box',
            kind: 'smb',
            host: 'nas.example',
            share: 'backup',
            path: '/',
            user: 'deploy',
            hasPassword: true,
          },
          {
            id: 'fedcba9876543210',
            name: 'SFTP archive',
            kind: 'sftp',
            host: 'ssh.example',
            path: '/srv',
            user: 'deploy',
            hasPassword: true,
          },
        ]),
      );
      localStorage.setItem(
        'ervisio.mock.remoteCapacity',
        JSON.stringify({
          '0123456789abcdef': {
            supported: true,
            total: 1073741824,
            used: 536870912,
            free: 536870912,
            avail: 536870912,
          },
          fedcba9876543210: { supported: false },
        }),
      );
    },
    { shell, user, elevated, legacy },
  );
}

for (const shell of ['classic', 'ervisios']) {
  test(`folder project action follows Terminal and opens the real editor in ${shell}`, async ({
    page,
  }) => {
    await fixture(page, shell);
    await page.goto('/files?path=/home/alice');
    await page.locator('[data-path="/home/alice/Copy"]').first().click({ button: 'right' });
    const items = await page.getByRole('menuitem').allTextContents();
    const terminal = items.findIndex((item) => item.includes('Open in Terminal'));
    expect(terminal).toBeGreaterThanOrEqual(0);
    expect(items[terminal + 1]).toContain('Open folder in Code editor');
    await page.getByRole('menuitem', { name: 'Open folder in Code editor', exact: true }).click();
    await expect(page.locator('.editor-project-tree')).toBeVisible();
    await expect(page.locator('.editor-tree-root [title="/home/alice/Copy"]')).toBeVisible();
  });
  test(`This PC defaults, network drive capacity and context actions in ${shell}`, async ({
    page,
  }) => {
    await fixture(page, shell);
    await page.goto('/files');
    const computer = page.locator('.files-computer');
    await expect(computer).toBeVisible();
    const places = page.locator('.files-places').first();
    const links = await places.getByRole('link').allTextContents();
    expect(links.findIndex((text) => text.trim() === 'This PC')).toBeLessThan(
      links.findIndex((text) => text.trim() === 'Home'),
    );
    const storage = computer.locator('[data-remote-drive="0123456789abcdef"]');
    await expect(storage).toContainText('512.0 MB free of 1.0 GB');
    await expect(computer.locator('[data-remote-drive="fedcba9876543210"]')).toContainText(
      'The server does not report available space',
    );
    await storage.click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Properties', exact: true }).click();
    const properties = page.getByRole('dialog', { name: 'Properties: Storage Box', exact: true });
    await expect(properties).toContainText('SMB');
    await expect(properties).toContainText('1.0 GB');
    await properties.getByRole('button', { name: 'Close', exact: true }).click();
    await computer.locator('[data-computer-card="/"]').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Format…', exact: true }).click();
    const format = page.getByRole('dialog', { name: /^Format/ });
    await expect(format.getByRole('button', { name: 'Format volume', exact: true })).toBeDisabled();
    await expect(format).toContainText('Formatting is blocked.');
    await format.getByRole('button', { name: 'Cancel', exact: true }).click();
    await computer.click({ button: 'right', position: { x: 8, y: 8 } });
    await page.getByRole('menuitem', { name: 'Add network folder', exact: true }).click();
    await expect(
      page.getByRole('dialog', { name: 'Add network folder', exact: true }),
    ).toBeVisible();
  });

  test(`copy, cut, paste, select all and Shift Delete focus correctly in ${shell}`, async ({
    page,
  }) => {
    await fixture(page, shell);
    await page.goto('/files');
    await page
      .locator('.files-places')
      .first()
      .getByRole('link', { name: 'Home', exact: true })
      .click();
    const one = page.locator('[data-path="/home/alice/one.txt"]').first();
    await one.click();
    await page.keyboard.press('Control+c');
    await page.locator('[data-path="/home/alice/Copy"]').first().dblclick();
    await page.keyboard.press('Control+v');
    await expect(page.locator('[data-path="/home/alice/Copy/one.txt"]').first()).toBeVisible();
    await page.keyboard.press('Alt+ArrowUp');
    await one.click();
    await page.keyboard.press('Control+x');
    await page.locator('[data-path="/home/alice/Move"]').first().dblclick();
    await page.keyboard.press('Control+v');
    const moved = page.locator('[data-path="/home/alice/Move/one.txt"]').first();
    await expect(moved).toBeVisible();
    await moved.click();
    await page.keyboard.press('Shift+Delete');
    const remove = page.getByRole('alertdialog', { name: 'Delete 1 item for good?', exact: true });
    await expect(remove).toBeVisible();
    await expect(remove.getByRole('textbox')).toHaveCount(0);
    await remove.getByRole('button', { name: 'Cancel', exact: true }).click();
    await page.keyboard.press('Alt+ArrowUp');
    await expect(one).toHaveCount(0);
    await page.locator('[data-path="/home/alice/two.txt"]').first().click();
    await page.keyboard.press('Control+a');
    await expect(page.locator('.files-bulk')).toContainText('3 selected');
  });

  test(`Delete trashes and Ctrl Z restores exact same bridge in ${shell}`, async ({ page }) => {
    await fixture(page, shell, 'alice', true);
    await page.goto('/files');
    await page
      .locator('.files-places')
      .first()
      .getByRole('link', { name: 'Home', exact: true })
      .click();
    const one = page.locator('[data-path="/home/alice/one.txt"]').first();
    await one.click();
    await page.keyboard.press('Delete');
    const unlock = page.getByRole('dialog', { name: 'Administrator rights needed', exact: true });
    await unlock.getByLabel('Your password').fill('fixture-password');
    await unlock.getByRole('button', { name: 'Unlock', exact: true }).click();
    await expect(one).toHaveCount(0);
    await expect(page.getByRole('alertdialog')).toHaveCount(0);
    await expect(
      page
        .locator('.files-places')
        .first()
        .getByRole('link', { name: /Administrator Trash/ }),
    ).toBeVisible();
    await page
      .locator('.files-pn')
      .first()
      .click({ position: { x: 20, y: 100 } });
    await page.keyboard.press('Control+z');
    await expect(one).toBeVisible();
    const calls = await page.evaluate(() =>
      JSON.parse(localStorage.getItem('ervisio.mock.fileMutations')!),
    );
    const restore = calls.findLast((call: any) => call.method === 'files.restore');
    expect(restore.admin).toBe(true);
    expect(restore.params.ids).toHaveLength(1);
    expect(calls.filter((call: any) => call.method === 'files.delete').at(-1)).toMatchObject({
      admin: true,
      params: { trash: true },
    });
  });
}

test('root Delete uses Trash and Undo toast restores without a permanent-delete dialog', async ({
  page,
}) => {
  await fixture(page, 'ervisios', 'root');
  await page.goto('/files?path=/home/alice/one.txt');
  const one = page.locator('[data-path="/home/alice/one.txt"]').first();
  await one.click();
  await page.keyboard.press('Delete');
  await expect(one).toHaveCount(0);
  await expect(page.getByRole('alertdialog')).toHaveCount(0);
  await page.locator('.ui-toasts').getByRole('button', { name: 'Undo', exact: true }).click();
  await expect(one).toBeVisible();
  const calls = await page.evaluate(() =>
    JSON.parse(localStorage.getItem('ervisio.mock.fileMutations')!),
  );
  expect(calls.filter((call: any) => call.method === 'files.delete').at(-1).params.trash).toBe(
    true,
  );
  expect(calls.findLast((call: any) => call.method === 'files.restore').admin).toBe(false);
});

test('This PC landing migration keeps saved folders and explicit file links take precedence', async ({
  page,
}) => {
  await fixture(page, 'classic', 'alice', false, true);
  await page.goto('/files');
  await expect(page.locator('.files-computer')).toBeVisible();
  await expect(page.getByRole('tab', { name: 'log Close log', exact: true })).toBeVisible();
  await page.goto('/files?path=/home/alice/one.txt');
  await expect(page.locator('[data-path="/home/alice/one.txt"]').first()).toBeVisible();
  await expect(page.locator('.files-computer')).toHaveCount(0);
});
