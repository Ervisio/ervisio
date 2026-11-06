import { expect, test } from '@playwright/test';

for (const shell of ['classic', 'ervisios']) test(`floating AI is optional and preserves its frame and draft in ${shell}`, async ({ page }) => {
  await page.addInitScript(shell => {
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem('ervisio.mock.home', JSON.stringify('/home/alice'));
    localStorage.setItem('ervisio.mock.prefs', JSON.stringify({ 'shell.mode': shell, language: 'en' }));
    localStorage.setItem('ervisio.mock.plugins', JSON.stringify([{ id: 'ai', name: 'AI', version: '0.3.0', entry: 'index.js', capabilities: {}, contributes: { pages: [{ id: 'ai', title: 'AI', ervisios: { view: 'ai-ervisios' } }] } }]));
  }, shell);
  await page.route('**/plugins/ai/index.js', route => route.fulfill({ contentType: 'text/javascript', body: 'export default()=>{}' }));
  await page.route('**/plugin-frame/ai', route => route.fulfill({ contentType: 'text/html', body: `<!doctype html><input aria-label="Draft"><output id="surface"></output><output id="activity"></output><button id="navigate">Open Files</button><script>
const send=value=>parent.postMessage({la:'plugin',...value},'*');
addEventListener('message',event=>{const message=event.data;if(message?.t==='init'){document.querySelector('#surface').textContent=message.view.surface;send({t:'loaded'});}if(message?.t==='visibility')document.querySelector('#activity').textContent=message.active?'visible':'hidden';});
document.querySelector('#navigate').onclick=()=>send({t:'req',id:1,op:'shell.command',args:{text:'open files'}});
setTimeout(()=>send({t:'ready'}),50);</script>` }));
  await page.goto('/settings#appearance');
  const toggle = page.getByRole('button', { name: 'Open or minimize AI chat', exact: true });
  if (shell === 'ervisios') {
    await expect(toggle).toHaveCount(0);
    await page.getByRole('switch', { name: 'Floating AI chat', exact: true }).click();
  }
  await expect(toggle).toBeVisible();
  await expect(page.locator('iframe[src*="/plugin-frame/ai"]')).toHaveCount(0);
  await toggle.click();
  const frame = page.frameLocator('iframe[src*="/plugin-frame/ai"]');
  await expect(frame.locator('#surface')).toHaveText('assistant');
  await frame.getByRole('textbox', { name: 'Draft' }).fill('Keep this draft');
  const panel = page.locator('.floating-assistant-panel');
  const before = await panel.boundingBox();
  await page.getByRole('button', { name: 'Expand AI chat', exact: true }).click();
  expect((await panel.boundingBox())!.width).toBeGreaterThan(before!.width);
  await expect(frame.getByRole('textbox', { name: 'Draft' })).toHaveValue('Keep this draft');
  await frame.getByRole('button', { name: 'Open Files', exact: true }).click();
  if (shell === 'classic') await expect(page).toHaveURL(/\/files$/);
  else await expect(page.getByRole('region', { name: 'File Explorer', exact: true })).toBeVisible();
  await expect(frame.getByRole('textbox', { name: 'Draft' })).toHaveValue('Keep this draft');
  await page.getByRole('button', { name: 'Minimize AI chat', exact: true }).click();
  await expect(panel).toBeHidden();
  await expect(page.locator('iframe[src*="/plugin-frame/ai"]')).toHaveCount(1);
  await toggle.click();
  await expect(frame.getByRole('textbox', { name: 'Draft' })).toHaveValue('Keep this draft');
  await page.screenshot({ path: `../.qa/floating-assistant-${shell}.png` });
});

test('floating assistant is absent without the AI plugin', async ({ page }) => {
  await page.addInitScript(() => { localStorage.setItem('ervisio.mock.user', JSON.stringify('alice')); localStorage.setItem('ervisio.mock.plugins', '[]'); });
  await page.goto('/');
  await expect(page.getByRole('button', { name: 'Open or minimize AI chat', exact: true })).toHaveCount(0);
});
