# Plugin SDK (frontend)

A plugin is a folder with `manifest.json` and an ES module (see `docs/ARCHITECTURE.md`, "Plugins").
The web app asks the daemon for the installed plugins with `plugins.list`, then loads every enabled
plugin with a dynamic `import('/plugins/<id>/<entry>')` and calls the module with the **SDK object**.
If `plugins.list` does not exist (yet) or fails, the app simply has no plugins.

`plugins.list` may return an array of manifests or `{plugins: [...]}`. Manifests with
`"enabled": false` are skipped. Fields used by the frontend: `id`, `name`, `version`, `entry`, `icon`,
`color` (a section hue: `ov term file log svc sw usr plg`, default `plg`), `contributes.pages`.

## Module shape

```js
// /plugins/docker/index.js
export default function activate(sdk) {
  const { react: React, ui, api } = sdk;
  const { Page, Card, Table, Badge, Button } = ui;

  sdk.registerStrings({
    en: { title: 'Containers', empty: 'No containers running.' },
    it: { title: 'Container', empty: 'Nessun container in esecuzione.' },
  });

  function Containers() {
    const [rows, setRows] = React.useState([]);
    React.useEffect(() => { sdk.api.exec('ps').then(setRows); }, []);
    return React.createElement(Page, { title: sdk.t('title'), hue: 'plg' }, /* ... */);
  }

  sdk.registerPage('docker', Containers);          // matches contributes.pages[].id
  sdk.registerWidget({ id: 'running', title: 'Running containers', cols: 3, render: Widget });
  sdk.registerSnippet({ name: 'docker ps', command: 'docker ps' });
}
```

`default` may be a function `(sdk) => void | Promise<void>`, an object `{ activate(sdk) }`, or a named
export `activate`. Plugins must not bundle their own React: use `sdk.react`.

## The SDK object

| member | description |
|---|---|
| `version` | SDK contract version, currently `1`. |
| `plugin` | `{ id, name, version, baseUrl }` (`baseUrl` = `/plugins/<id>/`, use it for assets). |
| `api.call(method, params?, {admin?})` | Same as the app's `call`; handles `needs_admin` by showing the unlock dialog and retrying. |
| `api.stream(method, params, {admin, onData, onEnd, onError, reopen})` | Same as the app's `stream`. Returns `{send, close}`. |
| `api.exec(command, args?, {admin?})` | Shortcut for `call('plugins.exec', {plugin, command, args})`. Runs only commands declared in the manifest. |
| `ui` | The component kit (`Button`, `Input`, `Select`, `Switch`, `Table`, `Card`, `StatCard`, `Panel`, `Dialog`, `ConfirmDialog`, `Tabs`, `Badge`, `Menu`, `toast`, `Icon`, `Page`, charts...). See `src/ui/index.ts`. |
| `react` | The host's `React` module. |
| `registerPage(id, page)` | `page` is a React component `({sdk}) => JSX` or `{ render(container, sdk) => cleanup? }` for framework-free code. The page opens at `/p/<plugin>/<id>`; its rail entry comes from the manifest. |
| `registerWidget({id, title, icon?, cols?, render})` | A widget the Overview can offer in its library ("from plugins"). `render` has the same two forms as a page. |
| `registerSnippet({name, command})` | A terminal snippet chip. |
| `registerStrings({ en: {...}, it: {...} })` | Your own dictionaries; `sdk.t(key, vars?)` uses the user's language and falls back to `en`, then the key. `{name}` placeholders are interpolated. |
| `t(key, vars?)` | See above. |
| `theme.get()` | `{ id, name, kind: 'dark'\|'light', vars }` with the resolved CSS variables. |
| `theme.onChange(cb)` | Subscribe to theme changes; returns an unsubscribe function. |

## Styling

All theme tokens are CSS variables on `:root`: `--bg --surface --sunk --ink --ink2 --ink3`, section
colours `--h-ov --h-term --h-file --h-log --h-svc --h-sw --h-usr --h-plg` (and `-s` soft fills), status
colours `--ok --warn --err --info` (and `-s`), accent `--acc`, `--on-acc`. Use them instead of fixed
colours so every theme works, follow `docs/DESIGN-RULES.md` (rounded rectangles, no grey divider
lines) and prefer the `ui` components. Wrap content in a hue scope with `className="hue-plg"` to get
`--h` and `--s`.

## Security notes

Plugin code runs in the page with the user's session: it can call any API the user can. Only install
plugins you trust. Plugins cannot run arbitrary commands on the server; `api.exec` is restricted to the
`argv` declared in the manifest, as user or admin as declared.
