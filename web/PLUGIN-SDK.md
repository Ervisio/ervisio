# Plugin SDK (frontend), contract version 3

A plugin is a folder with `manifest.json` and one ES module (see `docs/ARCHITECTURE.md`, "Plugins", and
`docs/api/plugins.md` for the manifest). Plugin code never runs inside the app. Each page or widget of a plugin runs
in its own sandboxed frame:

```
app (http(s)://host)                              plugin frame (opaque origin "null")
  PluginFrame ── <iframe sandbox="allow-scripts allow-forms" src="/plugin-frame/<id>"> ──▶ runtime + your module
      │  postMessage: init {code, view, theme, lang}             │
      │◀──────────── req {op: exec | http | readFile | …} ────────┤
  broker (web/src/plugins/broker.ts): checks the request against your manifest,
      │  then calls plugins.exec / plugins.http / plugins.readFile / … (the daemon checks the manifest again)
```

* The frame has **no same-origin access**: no cookies, no `localStorage`, no access to the app's DOM, and it cannot
  call `/api/*`. Its Content-Security-Policy allows no network at all, except `https://`/`wss://` connections to the
  hosts in `capabilities.network`. It cannot open pop-ups or navigate the app; if it navigates itself away, the app
  stops it.
* The app fetches your entry module and hands its source to the frame, which imports it from a `blob:` URL.
  **The entry must be one self-contained ES module** (bundle your code). Relative `import`s do not work; load other
  files of your folder with `sdk.asset()`.
* There is no `sdk.api.call` / `sdk.api.stream` any more: a plugin can do exactly what its manifest declares.

`plugins.list` tells the app which plugins are enabled for the user. Disabled plugins, plugins blocked by the signature
policy and plugins whose `visibleTo` excludes the user are neither listed nor served (`/plugins/<id>/…` and
`/plugin-frame/<id>` answer 404).

## Module shape

```js
// plugins/docker/index.js
export default function activate(sdk) {
  const { react: React, ui } = sdk;
  const h = React.createElement;
  const { Page, StatCard, Table, Button, toast } = ui;

  sdk.registerStrings({
    en: { title: 'Containers', empty: 'No containers running.' },
    it: { title: 'Container', empty: 'Nessun container in esecuzione.' },
  });

  function Containers() {
    const [rows, setRows] = React.useState([]);
    React.useEffect(() => {
      sdk.api.exec('ps').then((r) => setRows(r.stdout.split('\n').filter(Boolean).map(JSON.parse)));
    }, []);
    return h(Page, { title: sdk.t('title'), hue: 'file' } /* , … */);
  }

  sdk.registerPage('docker', Containers);                 // = contributes.pages[].id
  sdk.registerWidget({ id: 'containers', render: Widget }); // = contributes.widgets[].id
}
```

`default` may be a function `(sdk) => void | Promise<void>`, an object `{ activate(sdk) }`, or a named export
`activate`. Each frame shows one view, so `activate` runs once per open page or widget; register everything every
time and the runtime renders the view the frame was opened for. Do not bundle React: use `sdk.react`.

Snippets are plain data now: declare them in `manifest.contributes.snippets`. `sdk.registerSnippet` is a no-op kept so
v1 plugins do not throw.

## The SDK object

| member | description |
|---|---|
| `version` | `3`. Check it if your plugin also supports older consoles: `if (sdk.version < 3) …` (v3 adds `api.http`, `api.httpStream`, `api.pty`, `files.mkdir`, `files.remove`). |
| `plugin` | `{ id, name, version }`. |
| `view` | `{ kind: 'page' \| 'widget', id }`: what this frame shows. |
| `react` | React 18, shared by the runtime and the UI kit. |
| `ui` | The app's own component kit (`Button`, `IconButton`, `Input`, `Select`, `Switch`, `Checkbox`, `Segmented`, `Table`, `Card`, `StatCard`, `Page`, `Panel`, `Dialog`, `ConfirmDialog`, `Sheet`, `Tabs`, `Badge`, `Chip`, `Progress`, `Skeleton`, `EmptyState`, `Menu`, `DropdownMenu`, `Tooltip`, `Icon`, `Sparkline`, `AreaChart`, `toast`, ...). `toast.ok/err/info(title, detail?)` shows the toast in the app, prefixed with your plugin's name. |
| `api.exec(command, args?)` | Runs a command declared in `capabilities.commands` → `{stdout, stderr, exitCode, truncated?}`. A non-zero exit is a normal result. Only declared commands; the daemon validates every argument against the declared pattern. For a command declared `admin` the app adds administrator rights when the user needs them (not when the user is in `adminUnlessGroup` or is root) and shows its normal "Administrator rights needed" dialog. A command not declared `admin` never runs as root. |
| `api.execStream(command, args, {onLine(stream, line), onExit(code), onError(err)})` | Same, streamed per line. Returns `{close()}`; closing kills the process. |
| `api.http(name, {method, path, query?, headers?, body?})` | v3. An HTTP request to the `capabilities.http` entry `name` → `{status, headers, body, json(), bytes()}`. See "HTTP APIs, terminals and admin folders". |
| `api.httpStream(name, {method, path, query?, headers?, body?}, {onStart?(status, headers), onData(chunk), onEnd(), onError(err)})` | v3. Same, with the body delivered as `Uint8Array` chunks as they arrive. Returns `{close()}`; closing ends the connection. |
| `api.pty(command, args, {cols, rows, onData(chunk), onExit(code), onError(err)})` | v3. Runs a command declared `pty: true` in a terminal → `{write(data), resize(cols, rows), close()}`. |
| `files.read(path)` / `files.readBytes(path)` | Reads a file inside a folder listed in `capabilities.files.read` or `files.write` (4 MiB max), with the user's own rights. `read` returns text, `readBytes` a `Uint8Array`. |
| `files.write(path, data)` | Writes (atomically replaces) a file inside a folder listed in `capabilities.files.write`. `data` is a string or `Uint8Array`, 4 MiB max. |
| `files.list(path)` | Lists a folder inside the declared folders: `[{name, type: 'file'\|'dir'\|'link'\|'other', size, mtime}]`. |
| `files.mkdir(path)` | v3. Creates a folder (and missing parents) inside a `files.write` folder. |
| `files.remove(path)` | v3. Removes a file or an empty folder inside a `files.write` folder (never the declared folder itself). |
| `asset(path)` | Fetches a file of your own plugin folder (relative path) and returns a `blob:` URL for `<img src>`, CSS, etc. |
| `open(pageId)` | Opens one of your own pages in the app (for example from a widget). |
| `openExternal(url)` | Opens an `http://` or `https://` address in a new browser tab (the frame cannot open pop-ups itself). Addresses with a user name or password are refused. Use it from a click handler, or the browser may block the tab. |
| `registerPage(id, view)` | `view` is a React component `({sdk}) => element` or `{ render(container, sdk) => cleanup? }` for framework-free code. The page renders in the app's content panel at `/p/<plugin>/<id>`; its rail entry comes from the manifest. |
| `registerWidget({id, render})` | A widget the Overview offers in its library ("From plugins"). Title and icon come from the manifest. It renders in a small frame that grows with its content (up to 720 px). |
| `registerStrings({ en: {...}, it: {...} })` | Your dictionaries; `sdk.t(key, vars?)` uses the app's language, falls back to `en`, then the key, and fills `{name}` placeholders. The frame re-renders when the user changes language. |
| `t(key, vars?)`, `lang()` | See above. |
| `theme.get()` / `theme.onChange(cb)` | `{ id, name, kind: 'dark'\|'light', vars }` with the resolved CSS variables; the runtime applies them to the frame's `:root`, so CSS variables just work. |

Errors from `api.*`, `files.*` and `asset` are `Error`s with a `code` (`forbidden`, `invalid`, `not_found`,
`needs_admin`, `unavailable`, ...) and a readable message.

## HTTP APIs, terminals and admin folders (SDK v3)

### HTTP over a unix socket

Declare the API in `capabilities.http` (full rules in `docs/api/plugins.md`):

```json
"http": [{
  "name": "docker", "socket": "/var/run/docker.sock", "admin": true, "adminUnlessGroup": "docker",
  "headers": ["Content-Type", "X-Registry-Auth"],
  "rules": [
    {"methods": ["GET"], "path": "/v1\\.[0-9]+/containers/json"},
    {"methods": ["POST"], "path": "/v1\\.[0-9]+/containers/[a-zA-Z0-9_.-]+/(start|stop|restart)"}
  ],
  "maxBody": 8388608, "timeoutSec": 60
}]
```

```js
const r = await sdk.api.http('docker', { method: 'GET', path: '/v1.43/containers/json', query: { all: '1' } });
if (r.status === 200) setRows(r.json());
await sdk.api.http('docker', { method: 'POST', path: `/v1.43/containers/${id}/start` });
await sdk.api.http('docker', { method: 'POST', path: '/v1.43/containers/create', query: { name }, body: { Image: 'nginx' } });

const logs = sdk.api.httpStream('docker', { method: 'GET', path: `/v1.43/containers/${id}/logs`, query: { follow: '1', stdout: '1' } }, {
  onStart(status, headers) {},
  onData(chunk) { /* Uint8Array; Docker's multiplexed log frames are yours to split */ },
  onEnd() {},
  onError(e) {},
});
// later: logs.close();
```

* `path` is the URL path only; it must match a rule's regular expression completely, after percent-decoding. Paths
  with `..`, `.`, empty segments, a trailing `/`, an encoded `/` (`%2f`), control characters, a query or a fragment are
  refused. Encode path parts yourself (`encodeURIComponent(name)`).
* `query` is an object (`{k: v}` or `{k: [v1, v2]}`, encoded with `URLSearchParams`) or a raw string.
* `headers` may only use the names in the entry's `headers`. `Host` is fixed; cookies and other client headers are never
  sent. A `body` object is sent as JSON with `Content-Type: application/json`; a string as is; a `Uint8Array` as bytes.
* The result: `status` (a non-2xx status is a normal result, not an error; redirects are returned, never followed),
  `headers` (one string per name), `body` (text), `json()`, `bytes()`. Bodies are capped at `maxBody` (and a single
  `http` response at 11 MiB; use `httpStream` for more). Request bodies are limited to about 750 KiB by the app's
  transport.
* Errors: `not_found` (no such API), `invalid` (method, path, query, header or body not allowed), `needs_admin`
  (only if the unlock dialog was dismissed), `forbidden`, `unavailable` (nothing listening, timeout, response too large).
* For an `admin` entry the app adds administrator rights when needed (not when the user is root or in
  `adminUnlessGroup`), exactly as for commands. An entry not declared `admin` never runs as root.

### Terminals

A command declared `"pty": true` runs only through `sdk.api.pty` (and `api.exec` refuses it):

```js
const term = sdk.api.pty('shell', [containerId, '/bin/sh'], {
  cols: 120, rows: 32,
  onData(chunk) { xterm.write(chunk); },     // Uint8Array
  onExit(code) {}, onError(e) {},
});
xterm.onData((s) => term.write(s));          // string or Uint8Array
fit.onResize?.(() => term.resize(xterm.cols, xterm.rows));
// term.close() hangs up and kills the process.
```

### Admin folders and created folders

`capabilities.files.read` / `.write` entries may be objects: `{"path": "/opt/stacks", "admin": true,
"adminUnlessGroup": "docker"}` is used with administrator rights (the app asks for them when needed, as for commands),
or as the user when the user is in `adminUnlessGroup`. `{"path": "~/.config/ervisio/plugins/docker", "create":
true}` is created (0700 under `~`, else 0755) the first time you write into it. `files.mkdir` and `files.remove` work
only inside `write` folders.

Every stream (`execStream`, `httpStream`, `pty`) is closed by the app when your frame goes away (the page is left, the
widget removed, the plugin reloaded or disabled), and a frame has at most 32 streams open at once.

## Styling

The frame already carries the app's tokens, the UI kit CSS and the Figtree / JetBrains Mono fonts. All theme tokens are
CSS variables on `:root`: `--bg --surface --sunk --ink --ink2 --ink3`, section colours
`--h-ov --h-term --h-file --h-log --h-svc --h-sw --h-usr --h-plg` (and `-s` soft fills), status colours
`--ok --warn --err --info` (and `-s`), accent `--acc`, `--on-acc`. Use them instead of fixed colours so every theme works,
follow `docs/DESIGN-RULES.md` (rounded rectangles, no grey divider lines) and prefer the `ui` components. Wrap content in
a hue scope with `className="hue-plg"` to get `--h` and `--s`. The frame background is transparent: a page sits on the
app's content panel, a widget inside its Overview card. Inline styles and `<style>` elements work; external stylesheets
do not (use `sdk.asset()` and a `<style>` with the text if you must).

## Developing

* Put the folder in the repository's `./plugins` and run the daemon with `--dev`, or load it from Plugins › Developer
  (`plugins.loadDev`, needs developer mode). Dev folders may be unsigned while developer mode is on; they carry an
  "Unsigned, dev" badge. Everywhere else, `plugins.allow_unsigned = false` (the default) blocks unsigned plugins.
* "Reload" in Plugins › Developer restarts every open plugin frame with the new code.
* Sign a release with `plugin-sign` (see `docs/PLUGIN-SIGNING.md` and `docs/api/plugins.md`, "Signing"). Signing
  records the sha256 of every file in `manifest.json`, so sign after the last change.

## Migrating from SDK v2

Nothing to change: v2 plugins run unchanged and their manifests stay valid (and keep their signatures). v3 only adds
`capabilities.http`, `pty` commands and object entries in `capabilities.files`, with the SDK calls above.

## Migrating from SDK v1

| v1 | v2 |
|---|---|
| module imported into the app page | module runs in a sandboxed frame; one self-contained file |
| `sdk.api.call(method, …)`, `sdk.api.stream(…)` | removed; use `sdk.api.exec` / `sdk.api.execStream` with declared commands, `sdk.files.*` for declared folders |
| `sdk.api.exec(cmd, args, {admin})` | `sdk.api.exec(cmd, args)`: admin follows the manifest |
| `sdk.plugin.baseUrl` + `fetch`/`<img src>` | `await sdk.asset('img/logo.png')` |
| `sdk.registerSnippet(...)` | `manifest.contributes.snippets` |
| `registerWidget({id, title, icon, cols, render})` | `registerWidget({id, render})`; title and icon from the manifest |
| `ui.toast` inside the page | same call; the toast appears in the app |

## Security notes

The sandbox is the boundary: a plugin can reach the machine only through its declared commands (validated by the daemon,
as the user or, for `admin` commands, with the administrator rights the user unlocks), its declared HTTP APIs and its
declared folders (with the user's own rights, or administrator rights for folders declared `admin`, confined with
`os.Root` so symlinks cannot leave them). `capabilities.sockets` is informational, but **`capabilities.http` is not: each
entry grants API access to a local service** through its socket, limited to the declared methods, paths and headers.
How much that is depends on the service, and the consent dialog says so: **access to the Docker socket (or membership
of the `docker` group, or an `admin` entry) is equivalent to root on the machine**, because the Docker API can start a
privileged container that mounts `/`. The path rules narrow what the plugin can do, but a rule as broad as "create
containers" already gives a plugin root. Treat `admin` and `adminUnlessGroup` HTTP APIs, `admin` pty commands and
`admin` folders exactly like `admin` commands when you decide whether to install a plugin. The frame's own network
access is limited to
`capabilities.network` hosts over https/wss, and requests from the frame never carry the user's session cookie.
A plugin can still show the user whatever it likes inside its frame, and it can send data it was given to a declared
network host, or away by navigating its own frame (the app then stops the frame). Install plugins you trust; signed
plugins are verified against the Ervisio team key.
