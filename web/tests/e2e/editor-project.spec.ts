import { expect, test } from '@playwright/test';

for (const shell of ['classic', 'ervisios']) {
  test.describe(`project editor ${shell}`, () => {
    test.beforeEach(async ({ page }) => {
      await page.addInitScript((shell) => {
        localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
        localStorage.setItem('ervisio.mock.home', JSON.stringify('/srv/project'));
        localStorage.setItem(
          'ervisio.mock.prefs',
          JSON.stringify({ 'shell.mode': shell, language: 'en', theme: 'daylight' }),
        );
        const entry = (name: string, type = 'file') => ({
          name,
          type,
          size: 100,
          mtime: 1,
          mode: '0644',
          perm: 'rw-r--r--',
          owner: 'alice',
          group: 'alice',
        });
        localStorage.setItem(
          'ervisio.mock.files',
          JSON.stringify({
            '/srv/project': [entry('src', 'dir'), entry('app.js'), entry('node_modules', 'dir')],
            '/srv/project/src': [entry('worker.ts')],
            '/srv/project/node_modules': [entry('excluded.js')],
          }),
        );
        localStorage.setItem(
          'ervisio.mock.texts',
          JSON.stringify({
            '/srv/project/app.js': {
              content: 'const port = 8080;\n// needle main\n',
              mtime: 1,
              encoding: 'utf-8',
            },
            '/srv/project/src/worker.ts': {
              content: 'export const worker = true;\n// needle worker\n',
              mtime: 1,
              encoding: 'utf-8',
            },
            '/srv/project/node_modules/excluded.js': {
              content: '// needle excluded\n',
              mtime: 1,
              encoding: 'utf-8',
            },
          }),
        );
      }, shell);
    });

    test('directory tree expands lazily and switching files retains undo and cursor', async ({
      page,
    }) => {
      await page.goto('/editor?folder=%2Fsrv%2Fproject');
      const editor = page.locator('.editor-workspace--code');
      const tree = editor.getByRole('tree', { name: 'Files' });
      await expect(tree.locator('[data-project-path="/srv/project/src"]')).toBeVisible();
      await expect(tree.locator('[data-project-path="/srv/project/src/worker.ts"]')).toHaveCount(0);
      await tree.locator('[data-project-path="/srv/project/src"]').click();
      await expect(tree.locator('[data-project-path="/srv/project/src/worker.ts"]')).toBeVisible();
      await tree.locator('[data-project-path="/srv/project/app.js"]').evaluate((element) => {
        (element as HTMLElement).click();
        (element as HTMLElement).click();
      });
      const code = editor.locator('.cm-content');
      await expect(code).toContainText('const port = 8080;');
      await expect(editor.getByRole('tab', { name: 'app.js', exact: false })).toHaveCount(1);
      await code.press('Control+End');
      await page.keyboard.type('// retained');
      await code.press('Control+Home');
      await code.press('ArrowRight');
      await code.press('ArrowRight');
      await tree.locator('[data-project-path="/srv/project/src/worker.ts"]').click();
      await expect(code).toContainText('export const worker');
      await editor.getByRole('tab', { name: 'app.js', exact: false }).click();
      await expect(code).toContainText('// retained');
      await page.keyboard.type('X');
      await expect(code).toContainText('coXnst port');
      await code.press('Control+z');
      await expect(code).toContainText('const port');
      await code.press('Control+z');
      await expect(code).not.toContainText('// retained');
      await editor.getByRole('button', { name: 'Hide project sidebar', exact: true }).click();
      await expect(editor.getByRole('complementary', { name: 'Project' })).toHaveCount(0);
      await editor.getByRole('button', { name: 'Show project sidebar', exact: true }).click();
      await expect(editor.getByRole('tree', { name: 'Files' })).toBeVisible();
    });

    test('native Ctrl+A/S/F and Ctrl+Shift+F search the actual project and open matching lines', async ({
      page,
    }) => {
      await page.goto('/editor?folder=%2Fsrv%2Fproject&path=%2Fsrv%2Fproject%2Fapp.js');
      const editor = page.locator('.editor-workspace--code');
      const code = editor.locator('.cm-content');
      await expect(code).toContainText('const port = 8080;');
      await code.press('Control+a');
      await page.keyboard.type('const port = 9090;\n// needle saved\n');
      await code.press('Control+s');
      await expect
        .poll(() =>
          page.evaluate(
            () =>
              JSON.parse(localStorage.getItem('ervisio.mock.texts')!)['/srv/project/app.js']
                .content,
          ),
        )
        .toBe('const port = 9090;\n// needle saved\n');
      await code.press('Control+f');
      await expect(editor.locator('.cm-search')).toBeVisible();
      await editor.locator('.cm-search input[name="search"]').fill('needle');
      await page.keyboard.press('Escape');
      await code.press('Control+Shift+f');
      const search = editor.getByRole('textbox', { name: 'Search text in files', exact: true });
      await expect(search).toBeFocused();
      await search.fill('needle');
      await search.press('Enter');
      await expect(editor.locator('.editor-search-results > button')).toHaveCount(2);
      await expect(editor.locator('.editor-search-results')).not.toContainText('excluded.js');
      await editor
        .locator('.editor-search-results > button')
        .filter({ hasText: 'worker.ts' })
        .click();
      await expect(code).toContainText('export const worker');
      await expect(editor.locator('.editor-status')).toContainText('Ln 2, Col 4');
      await page.screenshot({ path: `../.qa/editor-project-${shell}.png` });
    });
  });
}

test('network folder projects browse and search through their saved connection', async ({
  page,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'en' }),
    );
    localStorage.setItem(
      'ervisio.mock.remotes',
      JSON.stringify([
        {
          id: '0123456789abcdef',
          name: 'Staging',
          kind: 'sftp',
          host: 'staging.example',
          path: '/project',
        },
      ]),
    );
    localStorage.setItem(
      'ervisio.mock.remoteFiles',
      JSON.stringify({
        '/project': [
          { name: 'config.yaml', path: '/project/config.yaml', type: 'file', size: 30, mtime: 1 },
        ],
      }),
    );
    localStorage.setItem(
      'ervisio.mock.remoteText',
      JSON.stringify({ '/project/config.yaml': 'remote: needle\n' }),
    );
  });
  await page.goto('/editor?folder=' + encodeURIComponent('remote:0123456789abcdef:/project'));
  const editor = page.locator('.editor-workspace--code');
  await editor
    .locator('[data-project-path="remote:0123456789abcdef:/project/config.yaml"]')
    .click();
  await expect(editor.locator('.cm-content')).toContainText('remote: needle');
  await editor.locator('.cm-content').press('Control+Shift+f');
  const search = editor.getByRole('textbox', { name: 'Search text in files', exact: true });
  await search.fill('needle');
  await search.press('Enter');
  await expect(editor.locator('.editor-search-results > button')).toHaveCount(1);
  await editor.locator('.editor-search-results > button').click();
  await expect(editor.locator('.editor-path')).toContainText('/project/config.yaml');
  await expect(editor.locator('.editor-status')).not.toContainText('Administrator');
});

test('stopping project search prevents later batches from changing its results', async ({
  page,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'classic', language: 'en' }),
    );
    const entries = Array.from({ length: 80 }, (_, i) => ({
      name: `file-${i}.txt`,
      type: 'file',
      size: 30,
    }));
    localStorage.setItem('ervisio.mock.files', JSON.stringify({ '/srv/search': entries }));
    localStorage.setItem(
      'ervisio.mock.texts',
      JSON.stringify(
        Object.fromEntries(
          entries.map((entry) => [`/srv/search/${entry.name}`, { content: 'needle\n', mtime: 1 }]),
        ),
      ),
    );
  });
  await page.goto('/editor?folder=%2Fsrv%2Fsearch');
  const editor = page.locator('.editor-workspace--code');
  await editor
    .getByRole('button', { name: 'Search in project (Ctrl+Shift+F)', exact: true })
    .click();
  const query = editor.getByRole('textbox', { name: 'Search text in files', exact: true });
  await query.fill('needle');
  await query.press('Enter');
  await expect(editor.locator('.editor-search-results > button').first()).toBeVisible();
  await editor.getByRole('button', { name: 'Stop', exact: true }).click();
  const count = await editor.locator('.editor-search-results > button').count();
  expect(count).toBeLessThan(80);
  await page.waitForTimeout(150);
  await expect(editor.locator('.editor-search-results > button')).toHaveCount(count);
  await expect(editor.getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);
});
