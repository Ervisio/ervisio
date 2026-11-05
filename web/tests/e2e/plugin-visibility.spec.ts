import { expect, test } from '@playwright/test';
import { pluginFrameHarness } from '../support/pluginFrameHarness';

test('minimized plugin timers pause, async writes complete, and the same draft resumes', async ({
  page,
}) => {
  const frame = await pluginFrameHarness(
    page,
    `export default function activate(sdk) {
    sdk.registerPage('main', { render(root) {
      root.innerHTML = '<input aria-label="Draft" value="unsaved draft">';
      const state = window.fixture = { polls: 0, frames: 0, completed: false, hidden: [], documentEvents: [] };
      sdk.visibility.onChange((hidden) => state.hidden.push(hidden));
      document.addEventListener('visibilitychange', () => state.documentEvents.push(document.hidden));
      const interval = setInterval(() => state.polls++, 20);
      let raf;
      function draw() { state.frames++; raf = requestAnimationFrame(draw); }
      raf = requestAnimationFrame(draw);
      (async () => { await new Promise((resolve) => setTimeout(resolve, 400)); await sdk.files.write('~/chat.json', 'completed while minimized'); state.completed = true; })();
      return () => { clearInterval(interval); cancelAnimationFrame(raf); };
    }});
  }`,
  );
  await expect(frame.getByRole('textbox', { name: 'Draft' })).toHaveValue('unsaved draft');
  await frame.getByRole('textbox').fill('question still being composed');
  await page.waitForTimeout(100);
  await page.evaluate(() =>
    (
      window as unknown as { frameHarness: { setActive(active: boolean): void } }
    ).frameHarness.setActive(false),
  );
  await frame.waitForFunction(() => document.hidden, undefined, { polling: 25 });
  const snapshot = await frame.evaluate(() => {
    const state = (window as unknown as { fixture: { polls: number; frames: number } }).fixture;
    return { polls: state.polls, frames: state.frames };
  });
  await page.waitForTimeout(700);
  const paused = await frame.evaluate(
    () =>
      (window as unknown as { fixture: { polls: number; frames: number; completed: boolean } })
        .fixture,
  );
  expect({ polls: paused.polls, frames: paused.frames }).toEqual(snapshot);
  expect(paused.completed).toBe(true);
  expect(
    await page.evaluate(() =>
      (
        window as unknown as { frameHarness: { files: Map<string, string> } }
      ).frameHarness.files.get('~/chat.json'),
    ),
  ).toBe('completed while minimized');
  await page.evaluate(() =>
    (
      window as unknown as { frameHarness: { setActive(active: boolean): void } }
    ).frameHarness.setActive(true),
  );
  await expect(frame.getByRole('textbox', { name: 'Draft' })).toHaveValue(
    'question still being composed',
  );
  await frame.waitForFunction(
    (previous) =>
      (window as unknown as { fixture: { polls: number; frames: number } }).fixture.polls >
        previous.polls &&
      (window as unknown as { fixture: { frames: number } }).fixture.frames > previous.frames,
    snapshot,
  );
  expect(
    await frame.evaluate(
      () => (window as unknown as { fixture: { hidden: boolean[] } }).fixture.hidden,
    ),
  ).toEqual([true, false]);
});
