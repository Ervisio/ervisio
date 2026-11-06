import { expect, test } from '@playwright/test';

for (const shell of ['classic', 'ervisios']) {
  test.describe(shell, () => {
    test.beforeEach(async ({ page }) => {
      await page.addInitScript((shell) => {
        if (localStorage.getItem('editor.fixture')) return;
        localStorage.setItem('editor.fixture', '1');
        localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
        localStorage.setItem('ervisio.mock.home', JSON.stringify('/srv/demo'));
        localStorage.setItem(
          'ervisio.mock.prefs',
          JSON.stringify({ 'shell.mode': shell, language: 'en', theme: 'daylight' }),
        );
        const entry = (name: string, type = 'file') => ({
          name,
          type,
          size: 80,
          mtime: 1,
          mode: type === 'dir' ? 'drwxr-xr-x' : '-rw-r--r--',
          perm: '644',
          owner: 'alice',
          group: 'alice',
        });
        localStorage.setItem(
          'ervisio.mock.files',
          JSON.stringify({
            '/srv/demo': [entry('app.js'), entry('notes.txt'), entry('project folder', 'dir')],
          }),
        );
        localStorage.setItem(
          'ervisio.mock.texts',
          JSON.stringify({
            '/srv/demo/app.js': {
              content: 'const port = 8080;\r\nconsole.log(port);\r\n',
              mtime: 1,
              encoding: 'utf-8',
            },
            '/srv/demo/notes.txt': { content: 'hello backend\n', mtime: 1, encoding: 'utf-8' },
          }),
        );
      }, shell);
    });
    test('CodeMirror opens from Explorer, highlights syntax and saves CRLF to the server', async ({
      page,
    }) => {
      const errors: string[] = [];
      page.on('pageerror', (e) => errors.push(e.message));
      await page.goto('/files?path=%2Fsrv%2Fdemo');
      await page.locator('[data-path="/srv/demo/app.js"]').first().click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Open in Code editor', exact: true }).click();
      const editor = page.locator('.editor-workspace--code');
      const content = editor.locator('.cm-content');
      await expect(content).toContainText('const port = 8080;');
      await expect(content.locator('span').first()).toBeVisible();
      await content.fill('const port = 9090;\nconsole.log(port);\n');
      await content.press('Control+s');
      await expect
        .poll(() =>
          page.evaluate(
            () =>
              JSON.parse(localStorage.getItem('ervisio.mock.texts')!)['/srv/demo/app.js'].content,
          ),
        )
        .toBe('const port = 9090;\r\nconsole.log(port);\r\n');
      await expect(editor.getByLabel('Unsaved changes')).toHaveCount(0);
      await page.screenshot({ path: `../.qa/editor-${shell}.png` });
      expect(errors).toEqual([]);
    });
    test('Notepad is a real editor and protects unsaved documents', async ({ page }) => {
      await page.goto('/files?path=%2Fsrv%2Fdemo');
      await page.locator('[data-path="/srv/demo/notes.txt"]').first().dblclick();
      const editor = page.locator('.editor-workspace--text');
      const text = editor.getByRole('textbox', { name: 'Notepad text' });
      await expect(text).toHaveValue('hello backend\n');
      await text.fill('updated notes\n');
      if (shell === 'ervisios') {
        await page
          .getByRole('region', { name: 'Notepad', exact: true })
          .getByRole('button', { name: 'Close', exact: true })
          .click();
        const confirm = page.getByRole('dialog', { name: 'Keep your changes?' });
        await expect(confirm).toBeVisible();
        await confirm.getByRole('button', { name: 'Keep editing' }).click();
        await expect(text).toHaveValue('updated notes\n');
      }
      await text.press('Control+s');
      await expect
        .poll(() =>
          page.evaluate(
            () =>
              JSON.parse(localStorage.getItem('ervisio.mock.texts')!)['/srv/demo/notes.txt']
                .content,
          ),
        )
        .toBe('updated notes\n');
      await (
        shell === 'ervisios' ? page.getByRole('region', { name: 'Notepad', exact: true }) : editor
      )
        .getByRole('button', { name: 'New tab', exact: true })
        .click();
      await text.fill('new file\n');
      await text.press('Control+s');
      const dialog = page.getByRole('dialog', { name: 'Save to the server' });
      await dialog.getByRole('textbox', { name: 'Folder path', exact: true }).fill('/srv/demo');
      await dialog.getByRole('button', { name: 'Go', exact: true }).click();
      await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill('new.txt');
      await dialog.getByRole('button', { name: 'Save', exact: true }).click();
      await expect
        .poll(() =>
          page.evaluate(
            () =>
              JSON.parse(localStorage.getItem('ervisio.mock.texts')!)['/srv/demo/new.txt']?.content,
          ),
        )
        .toBe('new file\n');
    });
    test('Explorer opens terminal in the selected directory, preserving spaces', async ({
      page,
    }) => {
      await page.goto('/files?path=%2Fsrv%2Fdemo');
      await page
        .locator('[data-path="/srv/demo/project folder"]')
        .first()
        .click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Open in Terminal', exact: true }).click();
      await expect
        .poll(() =>
          page.evaluate(
            () =>
              JSON.parse(localStorage.getItem('ervisio.mock.terminals') || '[]').filter(
                (s: { cwd: string }) => s.cwd === '/srv/demo/project folder',
              ).length,
          ),
        )
        .toBe(1);
    });
    test('a truncated preview cannot be saved through keyboard shortcuts', async ({ page }) => {
      await page.goto('/files?path=%2Fsrv%2Fdemo');
      await page.evaluate(() =>
        localStorage.setItem(
          'ervisio.mock.texts',
          JSON.stringify({
            '/srv/demo/large.txt': {
              content: 'truncated prefix',
              mtime: 1,
              encoding: 'utf-8',
              truncated: true,
            },
          }),
        ),
      );
      await page.goto('/notepad?path=%2Fsrv%2Fdemo%2Flarge.txt');
      const text = page.getByRole('textbox', { name: 'Notepad text' });
      await expect(text).toHaveValue('truncated prefix');
      await expect(text).toHaveAttribute('readonly', '');
      await text.press('Control+Shift+s');
      await expect(page.getByRole('dialog')).toHaveCount(0);
      await text.press('Control+s');
      expect(
        await page.evaluate(
          () =>
            JSON.parse(localStorage.getItem('ervisio.mock.texts')!)['/srv/demo/large.txt'].mtime,
        ),
      ).toBe(1);
    });
    test('network files use the same code editor and stay on their remote connection', async ({
      page,
    }) => {
      await page.goto('/files?path=%2Fsrv%2Fdemo');
      await page.evaluate(() => {
        localStorage.setItem(
          'ervisio.mock.remotes',
          JSON.stringify([
            {
              id: '0123456789abcdef',
              name: 'Storage',
              kind: 'smb',
              host: 'storage.example',
              share: 'backup',
              path: '/srv',
              hasPassword: true,
            },
          ]),
        );
        localStorage.setItem(
          'ervisio.mock.remoteText',
          JSON.stringify({ '/srv/app.py': 'print("remote")\n' }),
        );
      });
      await page.goto('/editor?path=' + encodeURIComponent('remote:0123456789abcdef:/srv/app.py'));
      const editor = page.locator('.editor-workspace--code');
      const content = editor.locator('.cm-content');
      await expect(content).toContainText('print("remote")');
      await content.fill('print("updated")\n');
      await content.press('Control+s');
      await expect
        .poll(() =>
          page.evaluate(
            () => JSON.parse(localStorage.getItem('ervisio.mock.remoteText')!)['/srv/app.py'],
          ),
        )
        .toBe('print("updated")\n');
      await expect(editor.locator('.editor-path')).toContainText('Network folder');
      expect(
        await page.evaluate(
          () =>
            JSON.parse(localStorage.getItem('ervisio.mock.texts')!)[
              'remote:0123456789abcdef:/srv/app.py'
            ],
        ),
      ).toBeUndefined();
    });
    test('server conflicts never overwrite another edit and drafts survive navigation', async ({
      page,
    }) => {
      await page.goto('/notepad?path=%2Fsrv%2Fdemo%2Fnotes.txt');
      const text = page.getByRole('textbox', { name: 'Notepad text' });
      await expect(text).toHaveValue('hello backend\n');
      await text.fill('my unsaved draft');
      await page.evaluate(() => {
        const files = JSON.parse(localStorage.getItem('ervisio.mock.texts')!);
        files['/srv/demo/notes.txt'] = {
          content: 'someone else changed this',
          mtime: 99,
          encoding: 'utf-8',
        };
        localStorage.setItem('ervisio.mock.texts', JSON.stringify(files));
      });
      await text.press('Control+s');
      await expect(
        page.getByRole('alert').filter({ hasText: 'The file changed on the server.' }),
      ).toBeVisible();
      expect(
        await page.evaluate(
          () =>
            JSON.parse(localStorage.getItem('ervisio.mock.texts')!)['/srv/demo/notes.txt'].content,
        ),
      ).toBe('someone else changed this');
      if (shell !== 'classic') {
        await page.getByRole('button', { name: 'Start', exact: true }).click();
        await page
          .getByRole('button', { name: 'Switch to classic interface', exact: true })
          .click();
        await expect(page.locator('.eos')).toHaveCount(0);
        await expect(
          page.locator('.app').getByText('Ryzen 7 5800X', { exact: true }),
        ).toBeVisible();
      }
      await expect(page.locator('.app')).toBeVisible();
      await page.locator('.app .rail').getByRole('link', { name: 'Files', exact: true }).click();
      await page
        .locator('.files-places')
        .first()
        .getByRole('link', { name: 'Home', exact: true })
        .click();
      await page.locator('[data-path="/srv/demo/notes.txt"]').first().dblclick();
      await expect(text).toHaveValue('my unsaved draft');
    });
  });
}
