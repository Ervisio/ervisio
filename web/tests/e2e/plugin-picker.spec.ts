import { expect, test } from '@playwright/test';

for (const shell of ['classic', 'ervisios'])
  test(`plugin chooser requires confirmation and grants no filesystem rights in ${shell}`, async ({
    page,
  }) => {
    await page.addInitScript((shell) => {
      localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
      localStorage.setItem('ervisio.mock.home', JSON.stringify('/srv/project'));
      localStorage.setItem(
        'ervisio.mock.prefs',
        JSON.stringify({ 'shell.mode': shell, language: 'en' }),
      );
      localStorage.setItem(
        'ervisio.mock.plugins',
        JSON.stringify([
          {
            id: 'sample',
            name: 'Example',
            version: '1.0.0',
            entry: 'index.js',
            capabilities: { files: { read: [], write: [] } },
            contributes: { pages: [{ id: 'main', title: 'Example' }] },
          },
        ]),
      );
      localStorage.setItem(
        'ervisio.mock.files',
        JSON.stringify({
          '/srv/project': [{ name: 'backups', type: 'dir', size: 0, mtime: 1 }],
          '/srv/project/backups': [],
        }),
      );
    }, shell);
    await page.route('**/plugins/sample/index.js', (route) =>
      route.fulfill({ contentType: 'text/javascript', body: 'export default()=>{}' }),
    );
    await page.route('**/plugin-frame/sample', (route) =>
      route.fulfill({
        contentType: 'text/html',
        body: `<!doctype html><button id="pick">Choose folder</button><output id="result"></output><output id="access"></output><script>
 const send=value=>parent.postMessage({la:'plugin',...value},'*');
 addEventListener('message',event=>{const message=event.data;if(message?.t==='init')send({t:'loaded'});if(message?.t==='res'&&message.id===1){document.querySelector('#result').textContent=message.value?.path||'Cancelled';if(message.value?.path)send({t:'req',id:2,op:'readFile',args:{path:message.value.path}});}if(message?.t==='res'&&message.id===2)document.querySelector('#access').textContent=message.error?.code||'granted';});
 document.querySelector('#pick').onclick=()=>send({t:'req',id:1,op:'pickFile',args:{mode:'folder',title:'Pick backup directory'}});
 setTimeout(()=>send({t:'ready'}),50);
 </script>`,
      }),
    );
    await page.goto('/p/sample/main');
    const frame = page.frameLocator('iframe[src*="/plugin-frame/sample"]');
    await frame.getByRole('button', { name: 'Choose folder', exact: true }).click();
    const picker = page.getByRole('dialog', {
      name: 'Example · Pick backup directory',
      exact: true,
    });
    await expect(picker).toBeVisible();
    await picker.getByRole('option').filter({ hasText: 'backups' }).click();
    await picker.getByRole('button', { name: 'Choose', exact: true }).click();
    await expect(frame.locator('#result')).toHaveText('/srv/project/backups');
    await expect(frame.locator('#access')).toHaveText('forbidden');
    await frame.getByRole('button', { name: 'Choose folder', exact: true }).click();
    await picker.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(frame.locator('#result')).toHaveText('Cancelled');
  });
