// Isolated visual QA for built first-party plugins. Never connects to a real daemon.
// Run from web/: node scripts/qa-plugin-frames.mjs [port]
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

const web = fileURLToPath(new URL('..', import.meta.url));
const workspace = resolve(web, '../..');
const port = Number(process.argv[2] ?? 4177);
const ids = new Set(['docker', 'minecraft', 'caddy', 'ai']);
const server = createServer(async (request, response) => {
  const url = new URL(request.url, 'http://127.0.0.1');
  response.setHeader('Cache-Control', 'no-store');
  try {
    if (url.pathname === '/') {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      response.end('<!doctype html><html lang="en"><meta charset="UTF-8"><title>Plugin frame QA fixture</title><style>html,body{margin:0;height:100%;background:#15151a}iframe{display:block;width:100%;height:100%;border:0}</style><iframe id="frame" title="Plugin under test" sandbox="allow-scripts allow-forms" src="/frame"></iframe><script type="module" src="/host.js"></script></html>');
      return;
    }
    if (url.pathname === '/frame') {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      response.end('<!doctype html><html><meta charset="UTF-8"><div id="root"></div><script src="/runtime.js"></script></html>');
      return;
    }
    let path;
    if (url.pathname === '/runtime.js') path = resolve(web, 'public/plugin-runtime.js');
    if (url.pathname === '/host.js') path = resolve(web, 'tests/support/plugin-frame-qa.js');
    const asset = url.pathname.match(/^\/plugin\/([a-z]+)\/(manifest|bundle)$/);
    if (asset && ids.has(asset[1])) {
      path = resolve(workspace, `plugin-${asset[1]}/dist/${asset[1]}/${asset[2] === 'manifest' ? 'manifest.json' : 'index.js'}`);
    }
    if (!path) { response.writeHead(404); response.end(); return; }
    response.setHeader('Content-Type', path.endsWith('.json') ? 'application/json' : 'application/javascript; charset=utf-8');
    response.end(await readFile(path));
  } catch (error) {
    response.writeHead(404);
    response.end(error.message);
  }
});
server.listen(port, '127.0.0.1', () => console.log(`Plugin frame QA fixture: http://127.0.0.1:${port}/?plugin=docker&surface=ervisios`));
