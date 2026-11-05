import { expect, test, type Page } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    if (localStorage.getItem('software.height.fixture')) return;
    localStorage.setItem('software.height.fixture', '1');
    localStorage.setItem('ervisio.mock.user', JSON.stringify('alice'));
    localStorage.setItem(
      'ervisio.mock.prefs',
      JSON.stringify({ 'shell.mode': 'ervisios', language: 'en', theme: 'daylight' }),
    );
  });
  await page.route('**/src/api/mock.ts', async (route) => {
    const response = await route.fetch();
    const source = await response.text();
    const injection = `
      if (method.startsWith('software.')) {
        if (method === 'software.summary') return {manager:'apt',sources:['repo'],aurHelper:'',aurSupported:false,updates:33,counts:{repo:33,aur:0,flatpak:0},downloadSize:174483046,rebootNeeded:false,rebootPending:false,security:2,lastCheck:Date.now(),schedule:null,busy:false,warnings:[]};
        if (method === 'software.updates') return Array.from({length:33},(_,index)=>({name:'package-'+index,from:'2.8.3',to:'2.8.4ubuntu0.1',source:'noble-updates',kind:'repo',size:5242880,notes:index<2?['security']:[]}));
        if (method === 'software.status') return {busy:false,transaction:null};
        if (method === 'software.suggest' && globalThis.__softwareCatalogPopulated) return Array.from({length:24},(_,index)=>({name:'tool-'+index,title:'Tool '+index,version:'1.0',description:'A real-shaped catalog result for layout verification',kind:'repo',source:'noble',installed:false}));
        return [];
      }
    `;
    const replacement = source.replace(/(async function mockCall\([^)]*\)\s*\{)/, `$1${injection}`);
    expect(replacement).not.toBe(source);
    await route.fulfill({ response, body: replacement });
  });
});

async function expectBoundedSoftware(page: Page) {
  const geometry = await page.locator('.sw-root').evaluate((root) => {
    const outer = root.closest('.eos-window-body')!;
    const frame = root.closest('.eos-native-frame')!;
    const body = root.querySelector('.sw-body')!;
    const rect = body.getBoundingClientRect();
    // Include nested group margins, which may collapse beyond their wrapper's border box.
    const actualEnd = Math.max(
      0,
      ...Array.from(body.querySelectorAll('*')).map(
        (child) =>
          child.getBoundingClientRect().bottom -
          rect.top +
          body.scrollTop +
          parseFloat(getComputedStyle(child).marginBottom || '0'),
      ),
    );
    const contentHeight = actualEnd + parseFloat(getComputedStyle(body).paddingBottom || '0');
    return {
      outerHeight: outer.clientHeight,
      outerScroll: outer.scrollHeight,
      frameHeight: frame.clientHeight,
      frameScroll: frame.scrollHeight,
      bodyHeight: body.clientHeight,
      bodyScroll: body.scrollHeight,
      tail: Math.max(0, body.scrollHeight - Math.max(body.clientHeight, contentHeight)),
    };
  });
  expect(geometry.outerScroll).toBeLessThanOrEqual(geometry.outerHeight + 1);
  expect(geometry.frameScroll).toBeLessThanOrEqual(geometry.frameHeight + 1);
  expect(geometry.tail, JSON.stringify(geometry)).toBeLessThanOrEqual(2);
  return geometry;
}

test('native Software scrolls the actual update list inside restored and maximized windows', async ({
  page,
}) => {
  await page.goto('/software');
  await expect(page.locator('.sw-ur')).toHaveCount(6);
  await expectBoundedSoftware(page);
  await page.locator('.sw-more').click();
  await expect(page.locator('.sw-ur')).toHaveCount(33);
  const expanded = await expectBoundedSoftware(page);
  expect(expanded.bodyScroll).toBeGreaterThan(expanded.bodyHeight);
  const headingBefore = await page.locator('.eos-native-heading').boundingBox();
  await page.locator('.sw-body').evaluate((body) => {
    body.scrollTop = body.scrollHeight;
  });
  expect(await page.locator('.sw-body').evaluate((body) => body.scrollTop)).toBeGreaterThan(0);
  const headingAfter = await page.locator('.eos-native-heading').boundingBox();
  expect(headingAfter!.y).toBe(headingBefore!.y);
  await page
    .getByRole('region', { name: 'Software', exact: true })
    .getByRole('button', { name: 'Maximize', exact: true })
    .click();
  await expectBoundedSoftware(page);
  await expect(page.locator('.sw-ur')).toHaveCount(33);
});

test('Find software has no blank scroll tail and respects the actual resized app height', async ({
  page,
}) => {
  await page.goto('/software');
  await page
    .locator('.eos-native-sidebar')
    .getByRole('button', { name: 'Find software', exact: true })
    .click();
  await expect(page.locator('.sw-cats')).toBeVisible();
  await expect(page.locator('.sw-ur')).toHaveCount(0);
  const full = await expectBoundedSoftware(page);
  expect(full.bodyScroll).toBeLessThanOrEqual(full.bodyHeight + 1);
  await page.evaluate(() => {
    (window as any).__softwareCatalogPopulated = true;
  });
  await page.getByRole('button', { name: 'Development', exact: true }).click();
  await expect(page.locator('.sw-ap')).toHaveCount(24);
  const populated = await expectBoundedSoftware(page);
  expect(populated.bodyScroll).toBeGreaterThan(populated.bodyHeight);
  const resize = await page.locator('.eos-window .eos-resize').boundingBox();
  await page.mouse.move(resize!.x + 9, resize!.y + 9);
  await page.mouse.down();
  await page.mouse.move(resize!.x - 150, resize!.y - 240, { steps: 5 });
  await page.mouse.up();
  const small = await expectBoundedSoftware(page);
  expect(small.outerHeight).toBeLessThan(full.outerHeight - 150);
  expect(small.bodyScroll).toBeGreaterThan(small.bodyHeight);
  await page.locator('.sw-body').evaluate((body) => {
    body.scrollTop = body.scrollHeight;
  });
  await expectBoundedSoftware(page);
  await page.screenshot({ path: 'test-results/software-native-scroll.png' });
});
