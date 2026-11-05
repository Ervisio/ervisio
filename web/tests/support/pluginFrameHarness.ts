import type { Page } from '@playwright/test';

/** Exercises the production sandbox runtime; only the broker/backend responses are fixtures. */
export async function pluginFrameHarness(
  page: Page,
  code: string,
  options: { page?: string; files?: Record<string, string> } = {},
) {
  const runtime = await (await page.request.get('/plugin-runtime.js')).text();
  await page.route('**/__plugin_visibility_host', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<!doctype html><iframe id="fixture" sandbox="allow-scripts allow-forms" style="width:1000px;height:750px"></iframe>',
    }),
  );
  await page.goto('/__plugin_visibility_host');
  await page.evaluate(
    ({ code, options, runtime }) => {
      const iframe = document.querySelector<HTMLIFrameElement>('#fixture')!;
      const files = new Map(Object.entries(options.files ?? {}));
      const host = {
        files,
        failed: '',
        loaded: false,
        setActive(active: boolean) {
          iframe.contentWindow!.postMessage({ la: 'plugin', t: 'visibility', active }, '*');
          iframe.style.display = active ? 'block' : 'none';
        },
      };
      Object.assign(window, { frameHarness: host });
      window.addEventListener('message', async (event) => {
        if (event.source !== iframe.contentWindow || event.data?.la !== 'plugin') return;
        const message = event.data;
        const post = (value: unknown) => iframe.contentWindow!.postMessage(value, '*');
        if (message.t === 'ready')
          post({
            la: 'plugin',
            t: 'init',
            plugin: { id: 'ai', name: 'Fixture', version: '1' },
            view: { kind: 'page', id: options.page ?? 'main', shell: 'ervisios' },
            appOrigin: location.origin,
            lang: 'en',
            platform: 'linux',
            active: true,
            theme: { id: 'test', name: 'Test', kind: 'dark', vars: {} },
            code,
          });
        if (message.t === 'loaded') host.loaded = true;
        if (message.t === 'failed') host.failed = message.message;
        if (message.t !== 'req') return;
        const args = message.args;
        let result: unknown = null;
        let error: { code: string; message: string } | undefined;
        switch (message.op) {
          case 'readFile':
            if (files.has(args.path)) result = { data: files.get(args.path) };
            else error = { code: 'not_found', message: 'No fixture file' };
            break;
          case 'writeFile':
            await new Promise((resolve) => setTimeout(resolve, 60));
            files.set(args.path, args.data);
            break;
          case 'listDir':
            result = {
              entries: [...files.keys()]
                .filter((path) => path.startsWith(args.path + '/'))
                .map((path) => ({
                  name: path.slice(args.path.length + 1),
                  type: 'file',
                  size: 0,
                  mtime: 0,
                })),
            };
            break;
          case 'mkdir':
            break;
          case 'shell.command':
            result = { handled: false };
            break;
          case 'exec':
            result = {
              stdout: JSON.stringify(
                args.args[0] === 'whoami'
                  ? { user: 'fixture', hostname: 'fixture', home: '/home/fixture', version: '1' }
                  : { tools: [], version: '1', policy: {} },
              ),
              stderr: '',
              exitCode: 0,
            };
            break;
        }
        post(
          error
            ? { la: 'plugin', t: 'res', id: message.id, ok: false, error }
            : { la: 'plugin', t: 'res', id: message.id, ok: true, value: result },
        );
      });
      // Inline the unmodified production bundle: Chrome's local-network policy blocks a srcdoc
      // opaque origin from fetching a script on loopback, even with CORS headers.
      iframe.srcdoc =
        '<!doctype html><div id="root"></div><script type="module">' +
        runtime.replace(/<\/script/gi, '<\\/script') +
        '</script>';
    },
    { code, options, runtime },
  );
  await page.waitForFunction(() => {
    const host = (window as unknown as { frameHarness: { loaded: boolean; failed: string } })
      .frameHarness;
    if (host.failed) throw new Error(host.failed);
    return host.loaded;
  });
  return page.frames().find((frame) => frame.parentFrame())!;
}
