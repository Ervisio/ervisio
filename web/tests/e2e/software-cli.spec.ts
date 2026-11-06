import { expect, test } from '@playwright/test';

for (const shell of ['classic', 'ervisios']) {
  test(`${shell}: managed CLI install confirms Node, stays user-scoped, supports uninstall and terminal`, async ({
    page,
  }) => {
    await page.addInitScript((mode) => {
      if (localStorage.getItem('cli.fixture')) return;
      localStorage.setItem('cli.fixture', '1');
      localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
      localStorage.setItem(
        'ervisio.mock.prefs',
        JSON.stringify({ 'shell.mode': mode, language: 'en', theme: 'daylight' }),
      );
    }, shell);
    await page.route('**/src/api/mock.ts', async (route) => {
      const response = await route.fetch();
      const source = await response.text();
      const body = source.replace(
        /(async function mockCall\([^)]*\)\s*\{)/,
        `$1
        if(method==='software.summary')return {manager:'apt',sources:['repo'],updates:0,counts:{repo:0,aur:0,flatpak:0},warnings:[],lastCheck:Date.now()};
        if(['software.updates','software.installed','software.apps','software.suggest','software.history'].includes(method))return [];
        if(method==='software.status')return {busy:false,transaction:null};
      `,
      );
      expect(body).not.toBe(source);
      await route.fulfill({ response, body });
    });
    await page.goto('/software?tab=store');
    const tools = page.getByRole('region', { name: 'Command-line tools', exact: true });
    const codex = tools
      .locator('article')
      .filter({ has: page.getByText('Codex CLI', { exact: true }) });
    await expect(codex).toBeVisible();
    await codex.getByRole('button', { name: 'Install', exact: true }).click();
    let dialog = page.getByRole('alertdialog', { name: 'Install Codex CLI?', exact: true });
    await expect(dialog).toContainText('Also install the official Node.js LTS runtime');
    await dialog.getByRole('button', { name: 'Install', exact: true }).click();
    await expect(codex.getByText('Managed · your account', { exact: true })).toBeVisible();
    const calls = await page.evaluate(() =>
      JSON.parse(localStorage.getItem('ervisio.mock.cli.calls') || '[]'),
    );
    expect(calls).toEqual([
      { op: 'install', id: 'node-lts', admin: false },
      { op: 'install', id: 'codex', admin: false },
    ]);
    await page.locator('.ui-panel').locator('button[aria-label="Close"]').click();
    await codex.getByRole('button', { name: 'Uninstall', exact: true }).click();
    dialog = page.getByRole('alertdialog', { name: 'Uninstall Codex CLI?', exact: true });
    await expect(dialog).toContainText('Keep credentials');
    await dialog.getByRole('button', { name: 'Uninstall', exact: true }).click();
    await expect(codex.getByRole('button', { name: 'Install', exact: true })).toBeVisible();
    await page.locator('.ui-panel').locator('button[aria-label="Close"]').click();
    await codex.getByRole('button', { name: 'Install', exact: true }).click();
    dialog = page.getByRole('alertdialog', { name: 'Install Codex CLI?', exact: true });
    await expect(dialog).not.toContainText('Also install the official Node.js LTS runtime');
    await dialog.getByRole('button', { name: 'Install', exact: true }).click();
    await expect(codex.getByText('Managed · your account', { exact: true })).toBeVisible();
    await page.locator('.ui-panel').locator('button[aria-label="Close"]').click();
    await codex.getByRole('button', { name: 'Open terminal', exact: true }).click();
    if (shell === 'ervisios')
      await expect(page.getByRole('region', { name: 'Terminal', exact: true })).toBeVisible();
    else await expect(page.locator('.terminal-body')).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => JSON.parse(localStorage.getItem('ervisio.mock.terminals') || '[]').length,
        ),
      )
      .toBeGreaterThan(0);
    const sessions = await page.evaluate(() =>
      JSON.parse(localStorage.getItem('ervisio.mock.terminals') || '[]'),
    );
    expect(sessions.at(-1).admin).toBe(false);
    expect(sessions.at(-1).cmd).toBeUndefined();
    if (shell === 'ervisios') {
      await expect(page).toHaveURL('/');
      await expect(page).toHaveTitle('Ervisio');
    }
  });
}
