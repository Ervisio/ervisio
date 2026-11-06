import { expect, test } from '@playwright/test';

for (const shell of ['classic', 'ervisios']) {
  test(`SMB network folders persist and reopen in ${shell}`, async ({ page }) => {
    await page.addInitScript((mode) => {
      localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
      localStorage.setItem('ervisio.mock.home', JSON.stringify('/home/alice'));
      localStorage.setItem(
        'ervisio.mock.prefs',
        JSON.stringify({ 'shell.mode': mode, language: 'en', theme: 'daylight' }),
      );
      localStorage.setItem(
        'ervisio.mock.remoteFiles',
        JSON.stringify({
          '/projects': [
            {
              name: 'backup.zip',
              path: '/projects/backup.zip',
              type: 'file',
              size: 120,
              mode: '0644',
              perm: '-rw-r--r--',
              owner: '',
              group: '',
              uid: 0,
              gid: 0,
              mtime: 1,
            },
          ],
          '/': [],
        }),
      );
    }, shell);
    await page.goto('/files');
    await page.getByText('Add network folder', { exact: true }).first().click();
    const dialog = page.getByRole('dialog', { name: 'Add network folder', exact: true });
    await dialog.getByLabel('Connection type').selectOption('smb');
    await dialog.getByLabel('Connection name').fill('Storage Box');
    await dialog
      .getByLabel('Network path', { exact: true })
      .fill('\\\\u705411.your-storagebox.de\\backup\\projects');
    await dialog.getByLabel('Username', { exact: true }).fill('u705411');
    await dialog.getByLabel('Password', { exact: true }).fill('fixture-smb-password');
    await dialog.getByRole('button', { name: 'Save and connect', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(
      page.locator('[data-path="remote:0123456789abcdef:/projects/backup.zip"]').first(),
    ).toBeVisible();
    const saved = await page.evaluate(() =>
      JSON.parse(localStorage.getItem('ervisio.mock.remotes')!),
    );
    expect(saved[0]).toMatchObject({
      kind: 'smb',
      host: 'u705411.your-storagebox.de',
      share: 'backup',
      path: '/projects',
      port: 445,
      user: 'u705411',
      hasPassword: true,
    });
    expect(JSON.stringify(saved)).not.toContain('fixture-smb-password');
    await page.reload();
    if (shell === 'ervisios') {
      await expect(page).toHaveURL(/\/$/);
      await page.getByRole('option', { name: 'File Explorer', exact: true }).dblclick();
    }
    await page
      .getByRole('link', { name: /Storage Box SMB/ })
      .first()
      .click();
    await expect(
      page.locator('[data-path="remote:0123456789abcdef:/projects/backup.zip"]').first(),
    ).toBeVisible();
    await page.getByRole('button', { name: 'Edit Storage Box', exact: true }).first().click();
    const edit = page.getByRole('dialog', { name: 'Edit network connection', exact: true });
    await expect(edit.getByLabel('Network path', { exact: true })).toHaveValue(
      '\\\\u705411.your-storagebox.de\\backup\\projects',
    );
    await expect(edit.getByLabel('Password', { exact: true })).toHaveValue('');
  });
  test(`SFTP trust, browse, create, download and forget work in ${shell}`, async ({ page }) => {
    await page.addInitScript((mode) => {
      localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
      localStorage.setItem('ervisio.mock.home', JSON.stringify('/home/alice'));
      localStorage.setItem(
        'ervisio.mock.prefs',
        JSON.stringify({ 'shell.mode': mode, language: 'en', theme: 'daylight' }),
      );
      const file = {
        name: 'app.yaml',
        path: '/srv/app.yaml',
        type: 'file',
        size: 12,
        mode: '0644',
        perm: '-rw-r--r--',
        owner: 'deploy',
        group: 'deploy',
        uid: 1000,
        gid: 1000,
        mtime: 1,
      };
      localStorage.setItem('ervisio.mock.remoteFiles', JSON.stringify({ '/srv': [file], '/': [] }));
      localStorage.setItem(
        'ervisio.mock.remoteText',
        JSON.stringify({ '/srv/app.yaml': 'port: 8080\n' }),
      );
    }, shell);
    await page.goto('/files');
    await page.getByText('Add network folder', { exact: true }).first().click();
    const dialog = page.getByRole('dialog', { name: 'Add network folder', exact: true });
    await dialog.getByLabel('Connection name').fill('Staging');
    await dialog.getByLabel('Host or IP address').fill('staging.example');
    await dialog.getByLabel('Username', { exact: true }).fill('deploy');
    await dialog.getByLabel('Password', { exact: true }).fill('fixture-only-password');
    await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
    await expect(dialog.getByText('Verify the server identity', { exact: true })).toBeVisible();
    await expect(
      dialog.getByText('SHA256:fixture-server-identity', { exact: false }),
    ).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem('ervisio.mock.remotes'))).toBeNull();
    await dialog
      .getByRole('button', { name: 'Trust this fingerprint and test', exact: true })
      .click();
    await expect(dialog.getByText('Connection tested successfully.')).toBeVisible();
    await dialog.getByRole('button', { name: 'Save and connect', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    const item = page.locator('[data-path="remote:0123456789abcdef:/srv/app.yaml"]').first();
    await expect(item).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem('ervisio.mock.remotes'))).not.toContain(
      'fixture-only-password',
    );
    await item.click({ button: 'right' });
    await expect(page.getByRole('menuitem', { name: 'Open in Terminal', exact: true })).toHaveCount(
      0,
    );
    await page.keyboard.press('Escape');
    await page
      .locator('.files-root')
      .getByRole('button', { name: 'New', exact: true })
      .first()
      .click();
    await page.getByRole('menuitem', { name: /^New folder/ }).click();
    const folder = page.getByRole('dialog', { name: 'New folder', exact: true });
    await folder.getByLabel('Name', { exact: true }).fill('config');
    await folder.getByRole('button', { name: 'Create', exact: true }).click();
    await expect(
      page.locator('[data-path="remote:0123456789abcdef:/srv/config"]').first(),
    ).toBeVisible();
    await page.context().route('**/api/files/download?**', async (route) => {
      await route.fulfill({
        status: 200,
        headers: { 'Content-Disposition': 'attachment; filename="app.yaml"' },
        body: 'port: 8080\n',
      });
    });
    await item.click({ button: 'right' });
    const download = page.waitForEvent('download');
    await page.getByRole('menuitem', { name: 'Download', exact: true }).click();
    const downloaded = await download;
    const url = new URL(downloaded.url());
    expect(url.searchParams.get('remote')).toBe('0123456789abcdef');
    expect(url.searchParams.get('path')).toBe('/srv/app.yaml');
    expect(url.searchParams.get('admin')).toBe('0');
    await page.getByRole('button', { name: 'Forget Staging', exact: true }).first().click();
    const forget = page.getByRole('alertdialog', { name: 'Forget Staging?', exact: true });
    await forget.getByRole('button', { name: 'Forget connection', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Forget Staging', exact: true })).toHaveCount(0);
  });
}
