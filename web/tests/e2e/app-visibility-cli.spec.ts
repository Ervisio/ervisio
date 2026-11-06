import { expect, test } from '@playwright/test';

for (const shell of ['classic', 'ervisios']) {
  test(`app visibility changes presentation without removing contextual tools in ${shell}`, async ({ page }) => {
    await page.addInitScript(shell => {
      if (localStorage.getItem('visibility.fixture')) return;
      localStorage.setItem('visibility.fixture', '1');
      localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
      localStorage.setItem('ervisio.mock.prefs', JSON.stringify({ 'shell.mode': shell, language: 'en' }));
    }, shell);
    await page.goto('/settings#appearance');
    const visibility = page.getByRole('switch', { name: 'Photos & videos', exact: true });
    if (shell === 'classic') {
      await expect(page.getByRole('link', { name: 'Photos & videos', exact: true })).toHaveCount(0);
      await visibility.click();
      await expect(page.getByRole('link', { name: 'Photos & videos', exact: true })).toBeVisible();
      await visibility.click();
    } else {
      await expect(visibility).toBeChecked();
      await visibility.click();
      await page.getByRole('button', { name: 'Start', exact: true }).click();
      await expect(page.locator('.eos-apps').getByRole('button', { name: 'Photos & videos', exact: true })).toHaveCount(0);
    }
    await page.reload();
    if (shell === 'ervisios') await page.goto('/settings#appearance');
    await expect(page.getByRole('switch', { name: 'Photos & videos', exact: true })).not.toBeChecked();
    // Hiding a tool is not an access restriction: its contextual route remains.
    await page.goto('/media');
    await expect(page.locator('.media-viewer')).toBeVisible();
  });

  for (const server of [true, false]) test(`CLI default follows server metadata and account override persists in ${shell}, server=${server}`, async ({ page }) => {
    await page.addInitScript(shell => {
      if (localStorage.getItem('cli-filter.fixture')) return;
      localStorage.setItem('cli-filter.fixture', '1');
      localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
      localStorage.setItem('ervisio.mock.prefs', JSON.stringify({ 'shell.mode': shell, language: 'en' }));
    }, shell);
    await page.route('**/src/api/mock.ts', async route => {
      const response = await route.fetch(); const source = await response.text();
      const injection = `
      if(method==='software.summary')return {serverProfile:${server},manager:'apt',sources:['repo'],updates:0,counts:{repo:0},lastCheck:Date.now(),warnings:[]};
      if(['software.search','software.suggest'].includes(method))return [{name:'git',title:'Git CLI',cli:true,kind:'repo',source:'apt',installed:false,version:'1',description:'Command line'},{name:'graphical-editor',title:'Graphical editor',kind:'repo',source:'apt',installed:false,version:'1',description:'GUI'},{name:'unknown-package',title:'Unknown package',kind:'repo',source:'apt',installed:false,version:'1',description:'Unknown'}];
      if(['software.updates','software.apps','software.installed'].includes(method))return [];
      if(method==='software.status')return {busy:false,transaction:null};`;
      const body = source.replace(/(async function mockCall\([^)]*\)\s*\{)/, `$1${injection}`);
      expect(body).not.toBe(source); await route.fulfill({response,body});
    });
    await page.goto('/software?tab=store');
    const chip = page.getByRole('button', { name: 'CLI compatible only', exact: true });
    await expect(chip).toHaveAttribute('aria-pressed', String(server));
    await expect(page.getByText('Git CLI', {exact:true})).toBeVisible();
    if(server) await expect(page.getByText('Graphical editor', {exact:true})).toHaveCount(0);
    else await expect(page.getByText('Graphical editor', {exact:true})).toBeVisible();
    await chip.click();
    await expect(chip).toHaveAttribute('aria-pressed', String(!server));
    await page.reload();
    if (shell === 'ervisios') await page.goto('/software?tab=store');
    await expect(page.getByRole('button', { name: 'CLI compatible only', exact: true })).toHaveAttribute('aria-pressed', String(!server));
  });
}
