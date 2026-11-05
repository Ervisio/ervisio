import { expect, test } from '@playwright/test';
import type {} from '../support/window-activity-probe';

test('minimized windows stop timers and shared subscriptions while retaining independent UI state', async ({
  page,
}) => {
  await page.goto('/');
  await page.clock.install();
  await page.evaluate(async () => {
    const module = '/tests/support/window-activity-probe.tsx';
    (await import(module)).mountActivityProbe();
  });
  const inspect = () => page.evaluate(() => window.activityProbe.inspect());
  await expect.poll(async () => (await inspect()).subscribers).toBe(2);
  await page.getByLabel('one state').fill('keep my draft');
  await page.clock.fastForward(120);
  const running = await inspect();
  expect(running.one).toBeGreaterThan(0);
  expect(running.two).toBeGreaterThan(0);
  await page.evaluate(() => window.activityProbe.setActive('one', false));
  await expect.poll(async () => (await inspect()).subscribers).toBe(1);
  const pausedOne = await inspect();
  await page.clock.fastForward(120);
  await page.evaluate(() => window.activityProbe.refresh());
  expect((await inspect()).one).toBe(pausedOne.one);
  expect((await inspect()).two).toBeGreaterThan(pausedOne.two);
  await expect(page.getByLabel('one state')).toHaveValue('keep my draft');
  await page.evaluate(() => window.activityProbe.setActive('two', false));
  await expect.poll(async () => (await inspect()).subscribers).toBe(0);
  const pausedBoth = await inspect();
  await page.evaluate(() => {
    window.activityProbe.publish();
    window.activityProbe.refresh();
  });
  await page.clock.fastForward(1000);
  expect(await inspect()).toEqual(pausedBoth);
  await expect(page.getByTestId('one-snapshot')).toHaveText('0');
  await page.evaluate(() => window.activityProbe.setActive('one', true));
  await expect.poll(async () => (await inspect()).subscribers).toBe(1);
  await expect(page.getByTestId('one-snapshot')).toHaveText('1');
  await expect(page.getByLabel('one state')).toHaveValue('keep my draft');
  expect((await inspect()).one).toBeGreaterThan(pausedBoth.one);
  expect((await inspect()).two).toBe(pausedBoth.two);
});

test('browser visibility also suspends active windows and refreshes on return', async ({
  page,
}) => {
  await page.goto('/');
  await page.clock.install();
  await page.evaluate(async () => {
    const module = '/tests/support/window-activity-probe.tsx';
    (await import(module)).mountActivityProbe();
  });
  const inspect = () => page.evaluate(() => window.activityProbe.inspect());
  await expect.poll(async () => (await inspect()).subscribers).toBe(2);
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: true });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await expect.poll(async () => (await inspect()).subscribers).toBe(0);
  const paused = await inspect();
  await page.clock.fastForward(1000);
  expect(await inspect()).toEqual(paused);
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: false });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await expect.poll(async () => (await inspect()).subscribers).toBe(2);
  expect((await inspect()).one).toBeGreaterThan(paused.one);
  expect((await inspect()).two).toBeGreaterThan(paused.two);
});
