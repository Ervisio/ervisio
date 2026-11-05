/** Browser QA fixture broker. All data and writes stay in this page's memory. */
const query = new URLSearchParams(location.search);
const id = query.get('plugin') || 'docker';
const surface = query.get('surface') === 'classic' ? 'classic' : 'ervisios';
const lang = query.get('lang') === 'it' ? 'it' : 'en';
const frame = document.getElementById('frame');
window.qa = { phase: 'starting', requests: [], errors: [], shell: [] };
const storage = new Map();
let payload;
const hues = { ov: '#8B93FF', term: '#3DDC97', file: '#5AB0FF', log: '#FFB547', svc: '#FF6B81', sw: '#B98CFF', usr: '#3FD8DE', plg: '#FF7AC6' };
const vars = { '--bg': '#000000', '--surface': '#0E0E10', '--sunk': '#18181B', '--line': '#26262B', '--ink': '#F4F4F6', '--ink2': '#A3A3AD', '--ink3': '#6C6C76', '--ok': '#3DDC97', '--warn': '#FFB547', '--err': '#FF6B81', '--info': '#5AB0FF' };
for (const [hue, color] of Object.entries(hues)) { vars[`--h-${hue}`] = color; vars[`--h-${hue}-s`] = `color-mix(in srgb, ${color} 16%, #000000)`; }
const post = (message) => frame.contentWindow.postMessage({ la: 'plugin', ...message }, '*');

const containers = [{
  Id: 'a'.repeat(64), Names: ['/web'], Image: 'nginx:stable', ImageID: 'sha256:' + 'b'.repeat(64), Command: 'nginx -g daemon off;',
  Created: 1793876400, State: 'running', Status: 'Up 3 hours', Labels: { 'com.docker.compose.project': 'website' },
  Ports: [{ IP: '127.0.0.1', PrivatePort: 80, PublicPort: 8080, Type: 'tcp' }], HostConfig: { NetworkMode: 'bridge' }, NetworkSettings: { Networks: {} }, Mounts: [],
}];
const servers = [{ id: 'survival', name: 'Survival', engine: 'paper', version: '1.21.4', port: 25565, memoryMB: 4096, state: 'online', cpu: 1.2, rssMB: 1250, playersOnline: 2, maxPlayers: 20, tps: 20 }];

function dataForHttp(args) {
  const path = args.path || '';
  if (args.name === 'minecraft') {
    if (path === '/v1/servers') return { servers };
    if (path === '/v1/health') return { version: '0.1.0', java: '21', data: '/var/lib/ervisio-minecraft', uptime: 24000, disk: { total: 100e9, free: 70e9 } };
    if (path === '/v1/jobs') return { jobs: [] };
    if (path === '/v1/activity') return { events: [] };
    if (path.includes('/console')) return { lines: [], next: 0 };
    if (path.endsWith('/logs')) return { lines: [{ seq: 1, text: '[Server] Ready' }], cursor: 1 };
    if (path.endsWith('/worlds')) return { worlds: [{ name: 'world', path: 'world', active: true }] };
    if (path.endsWith('/backups')) return { backups: [] };
    if (path.endsWith('/players')) return { online: [], whitelist: [], ops: [], banned: [] };
    return { servers: [], jobs: [], events: [], entries: [], catalog: [], engines: [], versions: [] };
  }
  if (path.endsWith('/version') || path === '/version') return { Version: '29.0.0', ApiVersion: '1.48', Os: 'linux', Arch: 'amd64' };
  if (path.endsWith('/info')) return { Name: 'qa-server', NCPU: 4, MemTotal: 8e9, DockerRootDir: '/var/lib/docker', OperatingSystem: 'Ubuntu 24.04', Containers: 1, ContainersRunning: 1, ContainersStopped: 0, Images: 0 };
  if (path.endsWith('/containers/json')) return containers;
  if (path.endsWith('/images/json') || path.endsWith('/networks')) return [];
  if (path.endsWith('/volumes')) return { Volumes: [] };
  if (path.endsWith('/system/df')) return { LayersSize: 0, Images: [], Containers: [], Volumes: [], BuildCache: [] };
  return {};
}

async function answer(operation, args = {}) {
  if (operation === 'envs') return [];
  if (operation === 'shell.command') { window.qa.shell.push(args.text); return { handled: true, message: lang === 'it' ? 'Apro Impostazioni.' : 'Opening Settings.' }; }
  if (operation === 'http') return { status: 200, headers: { 'content-type': 'application/json' }, text: JSON.stringify(dataForHttp(args)) };
  if (operation === 'readFile') {
    if (args.path === '/etc/caddy/Caddyfile') return { data: 'example.test {\n  reverse_proxy localhost:8080\n}\n' };
    if (storage.has(args.path)) return { data: storage.get(args.path) };
    throw { code: 'not_found', message: 'QA fixture: file does not exist.' };
  }
  if (operation === 'writeFile') { storage.set(args.path, args.data); return null; }
  if (operation === 'mkdir' || operation === 'remove' || operation === 'toast' || operation === 'open' || operation === 'notify') return null;
  if (operation === 'listDir') {
    const prefix = args.path.replace(/\/$/, '') + '/';
    return { entries: args.path === '/etc/caddy' ? [{ name: 'Caddyfile', type: 'file', size: 60, mtime: 0 }] : [...storage.keys()].filter((path) => path.startsWith(prefix) && !path.slice(prefix.length).includes('/')).map((path) => ({ name: path.slice(prefix.length), type: 'file', size: storage.get(path).length, mtime: Date.now() })) };
  }
  if (operation === 'auditList') return { entries: [], next: '', enabled: true };
  if (operation === 'jobs') return { jobs: [], instances: [], runs: [] };
  if (operation === 'exec') {
    if (args.command === 'service-show') return { stdout: 'LoadState=loaded\nActiveState=active\nExecStart={ path=/usr/bin/caddy ; argv[]=/usr/bin/caddy run --config /etc/caddy/Caddyfile ; }\n', stderr: '', exitCode: 0 };
    if (args.command === 'version') return { stdout: 'v2.11.0', stderr: '', exitCode: 0 };
    if (args.command === 'has-docker') return { stdout: '', stderr: '', exitCode: 1 };
    return { stdout: '', stderr: '', exitCode: 0 };
  }
  throw { code: 'forbidden', message: `QA fixture does not implement ${operation}.` };
}

window.addEventListener('message', async (event) => {
  if (event.source !== frame.contentWindow || event.data?.la !== 'plugin') return;
  const message = event.data;
  if (message.t === 'ready') { if (payload) post(payload); else window.qa.ready = true; }
  if (message.t === 'loaded') window.qa.phase = 'loaded';
  if (message.t === 'failed') { window.qa.phase = 'failed'; window.qa.errors.push(message.message); }
  if (message.t === 'req') {
    window.qa.requests.push({ op: message.op, args: message.args });
    try { post({ t: 'res', id: message.id, ok: true, value: await answer(message.op, message.args) }); }
    catch (error) { post({ t: 'res', id: message.id, ok: false, error: { code: error.code || 'unavailable', message: error.message } }); }
  }
  if (message.t === 'stream-open') { post({ t: 'stream', sid: message.sid, ev: 'start', status: 200, headers: {} }); }
});

const [manifest, code] = await Promise.all([
  fetch(`/plugin/${id}/manifest`).then((r) => { if (!r.ok) throw Error('Build the plugin first.'); return r.json(); }),
  fetch(`/plugin/${id}/bundle`).then((r) => r.text()),
]);
const page = manifest.contributes.pages[0];
payload = { t: 'init', plugin: { id: manifest.id, name: manifest.name, version: manifest.version }, view: { kind: 'page', id: surface === 'ervisios' ? page.ervisios?.view || page.id : page.id, shell: surface }, lang, theme: { id: 'oled', name: 'OLED', kind: 'dark', vars, density: 'comfortable' }, appOrigin: location.origin, platform: 'linux', code };
window.qa.view = payload.view;
if (window.qa.ready) post(payload);
