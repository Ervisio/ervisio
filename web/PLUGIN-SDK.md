# Plugin SDK (frontend), contract version 2

A plugin is a folder with `manifest.json` and one ES module (see `docs/ARCHITECTURE.md`, "Plugins", and
`docs/api/plugins.md` for the manifest). Plugin code never runs inside the app. Each page or widget of a plugin runs
in its own sandboxed frame:

```
app (http(s)://host)                              plugin frame (opaque origin "null")
  PluginFrame ── <iframe sandbox="allow-scripts" src="/plugin-frame/<id>"> ──▶ runtime + your module
      │  postMessage: init {code, view, theme, lang}             │
      │◀──────────── req {op: exec | readFile | …} ───────────────┤
  broker (web/src/plugins/broker.ts): checks the request against your manifest,
      │  then calls plugins.exec / plugins.readFile / … (the daemon checks the manifest again)
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
| `version` | `2`. Check it if your plugin also supports older consoles: `if (sdk.version < 2) …`. |
| `plugin` | `{ id, name, version }`. |
| `view` | `{ kind: 'page' \| 'widget', id }`: what this frame shows. |
| `react` | React 18, shared by the runtime and the UI kit. |
| `ui` | The app's own component kit (`Button`, `IconButton`, `Input`, `Select`, `Switch`, `Checkbox`, `Segmented`, `Table`, `Card`, `StatCard`, `Page`, `Panel`, `Dialog`, `ConfirmDialog`, `Sheet`, `Tabs`, `Badge`, `Chip`, `Progress`, `Skeleton`, `EmptyState`, `Menu`, `DropdownMenu`, `Tooltip`, `Icon`, `Sparkline`, `AreaChart`, `toast`, ...). `toast.ok/err/info(title, detail?)` shows the toast in the app, prefixed with your plugin's name. |
| `api.exec(command, args?)` | Runs a command declared in `capabilities.commands` → `{stdout, stderr, exitCode, truncated?}`. A non-zero exit is a normal result. Only declared commands; the daemon validates every argument against the declared pattern. For a command declared `admin` the app adds administrator rights when the user needs them (not when the user is in `adminUnlessGroup` or is root) and shows its normal "Administrator rights needed" dialog. A command not declared `admin` never runs as root. |
| `api.execStream(command, args, {onLine(stream, line), onExit(code), onError(err)})` | Same, streamed per line. Returns `{close()}`; closing kills the process. |
| `files.read(path)` / `files.readBytes(path)` | Reads a file inside a folder listed in `capabilities.files.read` or `files.write` (4 MiB max), with the user's own rights. `read` returns text, `readBytes` a `Uint8Array`. |
| `files.write(path, data)` | Writes (atomically replaces) a file inside a folder listed in `capabilities.files.write`. `data` is a string or `Uint8Array`, 4 MiB max. |
| `files.list(path)` | Lists a folder inside the declared folders: `[{name, type: 'file'\|'dir'\|'link'\|'other', size, mtime}]`. |
| `asset(path)` | Fetches a file of your own plugin folder (relative path) and returns a `blob:` URL for `<img src>`, CSS, etc. |
| `open(pageId)` | Opens one of your own pages in the app (for example from a widget). |
| `registerPage(id, view)` | `view` is a React component `({sdk}) => element` or `{ render(container, sdk) => cleanup? }` for framework-free code. The page renders in the app's content panel at `/p/<plugin>/<id>`; its rail entry comes from the manifest. |
| `registerWidget({id, render})` | A widget the Overview offers in its library ("From plugins"). Title and icon come from the manifest. It renders in a small frame that grows with its content (up to 720 px). |
| `registerStrings({ en: {...}, it: {...} })` | Your dictionaries; `sdk.t(key, vars?)` uses the app's language, falls back to `en`, then the key, and fills `{name}` placeholders. The frame re-renders when the user changes language. |
| `t(key, vars?)`, `lang()` | See above. |
| `theme.get()` / `theme.onChange(cb)` | `{ id, name, kind: 'dark'\|'light', vars }` with the resolved CSS variables; the runtime applies them to the frame's `:root`, so CSS variables just work. |

Errors from `api.*`, `files.*` and `asset` are `Error`s with a `code` (`forbidden`, `invalid`, `not_found`,
`needs_admin`, `unavailable`, ...) and a readable message.

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
as the user or, for `admin` commands, with the administrator rights the user unlocks) and its declared folders (with the
user's own rights, confined with `os.Root` so symlinks cannot leave them). `capabilities.sockets` is informational:
sockets are reached only by the declared commands, never directly. The frame's own network access is limited to
`capabilities.network` hosts over https/wss, and requests from the frame never carry the user's session cookie.
A plugin can still show the user whatever it likes inside its frame, and it can send data it was given to a declared
network host, or away by navigating its own frame (the app then stops the frame). Install plugins you trust; signed
plugins are verified against the LinuxAdmin team key.
