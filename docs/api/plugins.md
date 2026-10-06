# plugins.* (plugin management and execution)

Package `server/internal/modules/plugins`. Web side: `web/src/sections/plugins` (the sandboxed frames and the broker are
in `web/src/plugins`). The SDK for plugin authors (types, build preset, template, guide) is
[Ervisio/plugin-sdk](https://github.com/Ervisio/plugin-sdk). Error codes follow `docs/ARCHITECTURE.md`.

## Marketplace

Ervisio ships no plugins. They come from the marketplace:

```
plugin repo (e.g. Ervisio/plugin-docker)      registry Ervisio/plugins                       Ervisio
  tag vX.Y.Z -> release workflow:               sync (every 6 h, or by hand): PR with the       plugins.catalog:
  <id>-X.Y.Z.tar.gz (unsigned manifest)  ─────▶  new version and its permission diff  ───▶     catalog.json + catalog.sig
  + .sha256, no secrets                         maintainers review and merge                    from GitHub Pages, signature
                                                publish: plugin-sign with the team key,         checked; Install downloads the
                                                release <id>-X.Y.Z, catalog.json signed,        signed tarball and verifies it
                                                GitHub Pages                                    again (plugins.install)
```

* Plugin repositories hold no secrets: the registry polls their releases. The release contract (asset names, one
  top-level folder `<id>/`, manifest version = tag) is in the SDK's `docs/publishing.md`.
* Only registry maintainers merge, and every listed plugin, first-party or third-party, is reviewed and then signed by
  the team key in the registry's CI, so it is `verified` in the catalog. The private key is only in the registry's
  secret `PLUGIN_SIGNING_KEY` and on the release maintainer's machine (`docs/PLUGIN-SIGNING.md`).
* The catalog is `https://ervisio.github.io/plugins/catalog.json`, its signature `catalog.sig` next to it.

### Plugins that left the core

The Docker plugin shipped in the packaged location up to 0.3.0, enabled by default; it is now
[Ervisio/plugin-docker](https://github.com/Ervisio/plugin-docker). After an update the packaged copy is gone (the new
version folder or package has none), so on start the daemon (root, not in `--dev`) runs `plugins.MigrateMoved`
(`moved.go`), once per machine:

| Situation | What happens | `/var/lib/ervisio/plugins-moved.json` |
|---|---|---|
| A `docker` plugin is present anywhere (installed, dev) | nothing | `installed` |
| `plugins-state.json` has it switched off | nothing | `skipped` |
| No Docker socket (`/var/run/docker.sock`, `/run/docker.sock`) | nothing | `not-needed` |
| Otherwise | installs it from the signed remote catalog into `/var/lib/ervisio/plugins/docker`, as `plugins.install` with the entry's `sha256` and `capabilities` as consent: the catalog signature, the checksum and the plugin signature (team key) are all checked | `installed`, or `pending` on failure (retried every hour) |

Automatic installation is limited to this case because the same plugin, with the same permissions, was already
installed and enabled on that machine. Everywhere else it is a normal Browse install with the consent dialog. While the
plugin is missing, the host has Docker and the outcome is not `skipped`, `plugins.catalog` lists it in `moved` and
Plugins › Installed shows a "moved to the marketplace" card with an Install button. `install.sh` records `skipped` with
`--no-plugins` (`ervisiod --skip-moved-plugins`) and installs it with `--with-docker-plugin` or when the user says yes
(`ervisiod --install-plugin docker`).

## Isolation

Plugin code never runs in the app's origin. ervisiod serves:

| Path | What | Checks |
|---|---|---|
| `GET /plugin-frame/<id>` | Empty host page of a plugin frame, embedded by the app in `<iframe sandbox="allow-scripts allow-forms">`. It loads `/plugin-runtime.js` (built from `web/src/plugins/frame`). | session; `plugins.access` |
| `GET /plugins/<id>/<file>` | Plugin files. The app fetches the entry module and hands it to the frame; `sdk.asset()` goes through the app too. | session; `plugins.access` |

`plugins.access` (below) runs on the session's user bridge: the plugin must exist, be enabled, pass the signature policy
and be visible to the user (`visibleTo`); otherwise both paths answer 404. Frame headers: `Content-Security-Policy:
default-src 'none'; script-src 'nonce-…' blob:; style-src 'unsafe-inline'; img-src data: blob:; font-src data:;
media-src data: blob:; connect-src <https:// and wss:// of capabilities.network, else 'none'>; form-action 'none';
base-uri 'none'; frame-ancestors 'self'; sandbox allow-scripts allow-forms`, no `X-Frame-Options`, `Cache-Control: no-store`.
Plugin files: `Content-Security-Policy: sandbox; default-src 'none'; frame-ancestors 'none'`, `X-Content-Type-Options:
nosniff`, `Cross-Origin-Resource-Policy: same-origin`.

The frame talks to the app only by `postMessage` (`web/src/plugins/protocol.ts`). The broker (`web/src/plugins/broker.ts`)
maps each request to exactly one daemon call, using the manifest from `plugins.list`:

| Frame request | Daemon call | Allowed when |
|---|---|---|
| `exec {command, args}` | `plugins.exec` | `command` is declared and not `pty`; `admin:true` only when the command is declared `admin` and the user is neither root nor in `adminUnlessGroup` (the app's unlock dialog then appears) |
| `stream-open {command, args}` | `plugins.execStream` (stream) | same |
| `stream-open {kind:"pty", command, args, cols, rows}`, then `stream-input {data \| resize}` | `plugins.pty` (stream) | `command` is declared `pty: true`; admin as for `exec` |
| `http {name, method, path, query?, headers?, body?, json?}` | `plugins.http` | `name` is a `capabilities.http` entry, a rule allows the method on the cleaned path, every header is in its `headers`; admin as for `exec` |
| `stream-open {kind:"http", req}` | `plugins.httpStream` (stream) | same |
| `readFile` / `listDir {path}` | `plugins.readFile` / `plugins.listDir` | `path` inside `files.read` or `files.write`; admin only for a folder declared `admin` (as for commands) |
| `writeFile {path, data, b64?}`, `mkdir {path}`, `remove {path}` | `plugins.writeFile` / `plugins.mkdir` / `plugins.remove` | `path` inside `files.write`; admin as above |
| `asset {path}` | `GET /plugins/<id>/<path>` | relative path inside the plugin folder |
| `toast {tone, title, detail?}` | none (app toast, prefixed with the plugin name) | |
| `open {page}` | none (navigates to `/p/<id>/<page>`) | `page` is one of the plugin's pages |
| `openUrl {url}` | none (the app opens a new tab, `noopener`) | `http:`/`https:` URL without credentials, at most 2048 characters |

Anything else is refused. The daemon repeats every check that matters (commands, HTTP rules and headers, levels, folders, enabled, signature,
visibility), so the broker is a first gate, not the only one. A frame has at most 32 streams open; all of them are closed
when the frame fails, navigates away or is removed.

## Where plugins live

| Location (`location`) | Folder | Notes |
|---|---|---|
| `system` | `/usr/share/ervisio/plugins/<id>` (versioned installs: `/usr/lib/ervisio/versions/<v>/plugins/<id>`) | packaged; empty since the Docker plugin moved to the marketplace |
| `installed` | `/var/lib/ervisio/plugins/<id>` | from Browse / `plugins.install`, removable |
| `dev` | `./plugins` of the daemon's working directory (daemon `--dev` only, passed to the bridge as `--dev --dev-plugins <dir>`), plus folders from `plugins.loadDev` | loaded folders only when `plugins.dev = true` or the daemon runs in `--dev` |

The folder name must equal the manifest `id`. On duplicate ids the order is dev, system, installed.
Enabled state: `/var/lib/ervisio/plugins-state.json` (`{"enabled":{"docker":false}}`, missing = enabled).

## manifest.json

Strict: unknown fields are rejected.

```json
{
  "id": "docker", "name": "Docker", "version": "1.4.0", "author": "…", "description": "…", "homepage": "…",
  "icon": "server", "color": "file", "entry": "index.js",
  "files": {"index.js": "<sha256 hex>"},
  "capabilities": {
    "commands": [{"name":"stop","argv":["docker","stop","{0}"],
                  "args":[{"pattern":"[a-zA-Z0-9][a-zA-Z0-9_.-]*","maxLen":128}],
                  "admin":true,"adminUnlessGroup":"docker","timeoutSec":60},
                 {"name":"shell","pty":true,"argv":["docker","exec","-it","{0}","/bin/sh"],
                  "args":[{"pattern":"[a-zA-Z0-9][a-zA-Z0-9_.-]*"}],"admin":true,"adminUnlessGroup":"docker"}],
    "http": [{"name":"docker","socket":"/var/run/docker.sock","admin":true,"adminUnlessGroup":"docker",
              "headers":["Content-Type","X-Registry-Auth"],
              "rules":[{"methods":["GET"],"path":"/v1\\.[0-9]+/containers/json"}],
              "maxBody":8388608,"timeoutSec":60}],
    "files": {"read": ["/srv","~/projects"],
              "write": [{"path":"/opt/stacks","admin":true,"adminUnlessGroup":"docker"},
                        {"path":"~/.config/ervisio/plugins/docker","create":true}]},
    "sockets": ["/var/run/docker.sock"],
    "network": ["example.org"],
    "notify": true,
    "jobs": [{"name":"poll","params":[{"name":"dir","pattern":"/opt/stacks/[a-z0-9_.-]+"}],
              "steps":[{"id":"fetch","command":"fetch","args":["{param.dir}"]}]}]
  },
  "contributes": {"pages":[{"id":"docker","title":"Docker","icon":"server"}],
                  "widgets":[{"id":"containers","title":"Containers"}],
                  "snippets":[{"name":"docker ps","command":"docker ps"}]},
  "visibleTo": {"groups": ["docker","wheel"]}
}
```

* `id` `^[a-z][a-z0-9-]{1,39}$`; `version` semver; `color` one of `ov term file log svc sw usr plg`.
* `entry`: relative `.js`/`.mjs` path that exists, is a regular file and (after resolving symlinks) stays inside the folder.
* `files` (optional unless signed): path to sha256 hex of every file except `manifest.json`/`manifest.sig`; must list `entry`.
* **Logo** (optional, not a manifest field): a `logo.svg` or `logo.png` (preferred in that order) at the root of the
  plugin folder, at most 64 KiB, is drawn instead of `icon` in the rail, the dock, the palette and Plugins. A signed
  plugin shows it only when `files` lists it (signing does that). Being a file and not a field, a package with a logo
  still loads on consoles older than 0.5.0, which show the icon. Draw it square, legible at 20 px, on a transparent
  background that works on dark and light themes.
* Command: `argv[0]` is fixed (bare name resolved in the daemon's safe PATH, or absolute). `{N}` slots (also inside an
  item, like `--name={0}`) take the Nth call argument. Every slot needs an `args[N]` with a `pattern`: a regular expression
  that must match the **whole** value. Values starting with `-` are refused unless `allowDash`; default `maxLen` 256.
  Every declared arg must be used. `admin: true` needs the root bridge; `adminUnlessGroup` lets members of that group
  run it as themselves. `timeoutSec` default 30, max 600. `pty: true` (SDK v3): the command runs only through
  `plugins.pty`, in a pseudo-terminal; `plugins.exec` / `plugins.execStream` refuse it, and `plugins.pty` refuses other
  commands.
* `visibleTo.groups` empty = everyone. Admins (root bridge, root, or members of `sudo`/`wheel`/`admin`) always see everything.
* `capabilities.files`: absolute paths or `~/…` (the user's home). The plugin may read inside `read` and `write` folders
  and write inside `write` folders through `plugins.readFile` / `plugins.writeFile` / `plugins.listDir` /
  `plugins.mkdir` / `plugins.remove`, with the user's own rights. Nothing else in the app gives a plugin file access.
  An entry is a path string, or (SDK v3) an object `{"path", "admin"?, "adminUnlessGroup"?, "create"?}` (unknown fields
  are rejected; entries without options are written back as strings, so v2 manifests and their signatures do not change).
  `admin: true`: the folder is used on the root bridge (with administrator rights, after the unlock dialog) unless the
  caller is root or in `adminUnlessGroup`; it must be an absolute path, not under `~`. A plain folder is never used on
  the root bridge. When a path falls in several entries the longest folder wins, and on a tie the entry without `admin`.
  `create: true`: a write (`writeFile`, `mkdir`) into the folder creates it first when it is missing (0700 under `~`,
  else 0755, with missing parents).
* `capabilities.http` (SDK v3): HTTP APIs on unix sockets, at most 16 entries:
  * `name` `^[a-z][a-z0-9-]{0,31}$`, unique. `socket`: a clean absolute path (at most 107 bytes, the `sun_path` size).
  * `rules` (1 to 256): `{"methods": […], "path": "<regexp>"}`. Methods are upper case, from `GET HEAD POST PUT PATCH
    DELETE OPTIONS`. `path` is a Go regular expression (at most 512 characters) that must match the **whole** decoded URL
    path (it is compiled as `^(?:…)$`).
  * `headers` (at most 32): the only request headers the plugin may set. Names are `^[A-Za-z][A-Za-z0-9-]{0,63}$`, and
    never `Host`, `Cookie`, `Authorization`, `Proxy-Authorization`, hop-by-hop or framing headers (`Connection`,
    `Upgrade`, `Transfer-Encoding`, `Content-Length`, `Keep-Alive`, `TE`, `Trailer`, `Expect`), `Forwarded`, `Via`,
    `Origin`, `Referer`, or `Proxy-*`, `Sec-*`, `X-Forwarded-*`.
  * `admin` / `adminUnlessGroup` as for commands: the socket is opened by the bridge process, so as the user, or as root
    on the root bridge. An entry not declared `admin` is never used on the root bridge.
  * `maxBody` caps the request and the response body of `plugins.http` / `plugins.httpStream` and the response body of
    an upload (default 8 MiB, max 64 MiB; a request body sent inline is at most 8 MiB, see "Limits of request bodies").
    `maxUpload` caps the file sent with `plugins.upload` (default 20 GiB, max 1 TiB; 0 = the default). `timeoutSec`
    (default 30, max 600) bounds `plugins.http`, and the wait for the response headers of `plugins.httpStream` and of
    the transfers.
* `capabilities.sockets`: informational. It lists the sockets the plugin's commands talk to (for example `docker`
  talking to `/var/run/docker.sock`). A plugin opens a socket itself only through `capabilities.http`.
* `capabilities.jobs` (SDK 0.2, core 0.5): named, declarative jobs the daemon runs in the background for instances
  the plugin creates at runtime: ordered steps over the plugin's own commands and HTTP APIs (and `notify` steps),
  with `{param.x}` / `{step.id.stdout}` placeholders and one condition per step. At most 16 jobs. The full schema, the
  validation rules, the run model, administrator approval and webhooks are in `docs/api/jobs.md`.
* `capabilities.notify` (SDK 0.2, core 0.5): `true` lets the plugin send notifications through `plugins.notify` and job
  `notify` steps, to the channels an administrator configured in Settings (`docs/api/notify.md`). Rate limited per plugin
  and user (job steps have their own budget per instance); the message names the user who sent it.
* `capabilities.network`: host names (`api.example.org`, `*.example.org`, `host:8443`; nothing else, the entries go into
  a CSP header). The plugin frame may connect to them over https/wss; the user's session cookie is never sent from the
  frame. It is a list, or `{"hosts": [...], "userHosts": true}`: with `userHosts` the plugin may ask an administrator to
  approve more hosts, one exact `host:port` at a time (`plugins.network.request`, below).
* `remote` (SDK 3, environments, see [environments.md](environments.md)): `"remote": "docker"` on a `capabilities.http`
  entry lets calls with an `env` go to a remote Docker host instead of `socket`. On a command it declares that the
  command may run against an environment: `argv[0]` must be `docker`, and `argv` must hold exactly one item equal to
  `{env}` (for example `["docker", "-H", "{env}", "compose", "up", "-d"]`). The item is the whole argv entry, never part
  of one, and `{env}` without `remote` is refused. Against an environment the call runs with the user's own rights and
  never as root, so `admin` and `adminUnlessGroup` do not apply to it.
* `platforms` (core 0.6.1): the systems the plugin works on, `["linux"]`, `["windows"]` or `["linux", "windows"]`. A
  manifest without it is a Linux plugin (every plugin written before Windows support was). On another system the plugin
  is refused like one that needs a newer core: not installed, not enabled, every call `forbidden` ("<Name> works only on
  Linux."); `plugins.list` and the catalog give `platforms` (never empty) and `incompatible`. Plugins and Browse show a
  Linux and/or Windows mark next to Install. The catalog entry carries the same field. In the frame, `sdk.platform` is
  `"linux"` or `"windows"`: commands, paths and sockets differ, so a plugin for both picks them from it (Windows has no
  unix-socket services such as Docker's; commands run without a shell, e.g. `["powershell.exe", "-NoProfile", ...]`).
* `minCore` / `requires` (core 0.5): the Ervisio version the plugin needs, `"minCore": "0.5.0"` or
  `"requires": {"ervisio": ">=0.5.0"}` (the same thing; give either or both, the higher counts; only `>=` or a bare
  `X.Y.Z` is understood, anything else is refused when the manifest is read). A core older than that refuses to install
  the plugin (`conflict`, "<Name> needs Ervisio 0.5.0 or newer; this server runs 0.4.0. Update Ervisio first. Nothing was
  installed."), will not enable it (`plugins.setEnabled`) and refuses every call to an already installed one (`forbidden`,
  same text); `plugins.list` shows it off with `incompatible: "<message>"`. A `--dev` daemon built from a checkout
  (version `vX.Y.Z-N-g<hash>`, i.e. between releases) accepts every plugin, so the next release can be tried before it
  is tagged. Older cores do not know the fields and refuse the manifest as "unknown field", which has the same effect.

## Background jobs and notifications

`plugins.jobs.*` (create, list, get, update, delete, runNow, history, webhooks) and `plugins.notify` are answered by the
daemon itself, not by a bridge, because jobs must run while no page is open. The broker (`web/src/plugins/brokerJobs.ts`)
fills in the plugin id and refuses undeclared jobs, but the daemon checks everything again. Reference:
`docs/api/jobs.md` (jobs, instances, webhooks `POST /hooks/<plugin>/<token>`) and `docs/api/notify.md` (channels,
`plugins.notify`). The SDK exposes them as `sdk.api.jobs` and `sdk.api.notify`; both are absent on consoles older than
0.5, so a plugin checks `sdk.api.jobs` before using it. The SDK contract version stays 3: the additions are backward
compatible.

## Signing

`manifest.sig` holds the base64 ed25519 signature of `"linuxadmin-plugin-v1\n" + canonical(manifest.json)`, where the
canonical form is the JSON re-encoded with sorted keys and no whitespace. Because the manifest carries the sha256 of every
other file in `files`, the signature covers the whole folder. Verification also fails when a file differs from its hash,
when a listed file is missing, or when the folder holds a file that is not listed.

The trusted key is `plugins.TeamPublicKey` in `sign.go`, the public half of the Ervisio team key. The private key
is never in the repository: where it lives, who may use it and how to rotate it is in `docs/PLUGIN-SIGNING.md`.
Marketplace plugins are signed with it by the registry's CI.

```sh
go build -o plugin-sign ./server/internal/modules/plugins/cmd/plugin-sign
plugin-sign -genkey team.key                  # prints the public key to embed
plugin-sign -key team.key dist/docker         # writes "files" into manifest.json and manifest.sig
plugin-sign -verify dist/docker               # against the embedded key (or -pub <base64>)
plugin-sign -key team.key -catalog catalog.json        # writes catalog.sig
plugin-sign -verify -catalog catalog.json              # checks catalog.sig (or -pub <base64>)
```

### Catalog signature

`catalog.sig` holds the base64 ed25519 signature of `"ervisio-catalog-v1\n" + canonical(catalog.json)` (the same
canonical form as manifests). The different prefix means a plugin signature can never pass for a catalog signature or
the other way round. Its address is the catalog's with a final `.json` replaced by `.sig` (`.sig` appended otherwise;
query and fragment dropped). Trusted keys: the team keys plus `plugins.catalog_key` (config, base64 ed25519 public key,
for a private catalog). A catalog key only makes a catalog's *listing* trusted: the plugins it lists still need a team
signature to install, unless `plugins.allow_unsigned` is on.

`plugins.allow_unsigned = false` (config, **the default**): only plugins with a valid signature are listed as enabled, run
commands, are served or install. Exception: plugins in the dev location (the repository's `./plugins` under `ervisiod
--dev`, or folders loaded with `plugins.loadDev`) run unsigned while developer mode is on; `plugins.list` marks them
`devUnsigned` and the UI shows an "Unsigned, dev" badge. Present-but-invalid signatures are always refused at install
time.

## Methods

### `plugins.list` (user)
Params `{}`. Result: array of

```json
{"id":"docker","name":"Docker","version":"1.4.0","author":"…","description":"…","icon":"server","logo":"logo.svg","color":"file",
 "entry":"index.js","enabled":true,"signed":true,"verified":true,"signatureError":"…","capabilities":{…},
 "contributes":{…},"visibleTo":{"groups":[]},"location":"installed","removable":true,"dir":"/var/lib/…",
 "unloadable":false,"updateAvailable":{"version":"1.5.0","notes":"…","newPermissions":false,"source":"https://…","sha256":"…"},
 "blocked":false,"error":""}
```
`logo` (omitted when none): the logo file, served at `/plugins/<id>/<logo>`. `capabilities` and `contributes` always carry arrays (never null). `signed` = a manifest.sig exists, `verified` = it is valid.
`blocked` = unsigned/invalid while `allow_unsigned` is off (`enabled` is then false); `devUnsigned` = an unsigned dev
plugin that runs because developer mode is on. `updateAvailable` comes from the
local catalog. Folders whose manifest fails validation are listed for admins only, with `error` set and `enabled:false`.
Other users only see plugins allowed by `visibleTo`.

### `plugins.setEnabled` (admin)
Params `{"id":"docker","enabled":false}` → `{id, enabled}`. `not_found` for an unknown id.

### `plugins.uninstall` (admin)
Params `{"id"}` → `{id}`. Only plugins in `/var/lib/ervisio/plugins`; others → `forbidden`, unknown → `not_found`, bad id → `invalid`.

### `plugins.install` (admin)
Params `{"source": "https://…/x.tar.gz" | "/abs/path/x.tar.gz", "sha256"?: "<hex>", "consent"?: <capabilities>}` → the new list entry.
Download: https only (redirects too), 60 s, 32 MiB. The archive is checked and unpacked into a temporary folder: only
regular files and directories, no absolute or `..` paths, no backslashes, no symlinks/hard links/special files, at most
2000 entries, 16 MiB per file and 64 MiB in total; one top-level folder is stripped. Then the manifest is validated, the
signature verified (invalid signature → `forbidden`; unsigned → `forbidden` unless `allow_unsigned`), `consent` (when sent)
must equal the manifest's `capabilities` exactly (else `conflict`: the user approved different permissions), and the folder is
moved atomically into place (an existing installed version is replaced = update). Packaged plugins cannot be replaced (`conflict`).
Errors: `invalid` (bad source, archive, manifest, checksum), `forbidden`, `conflict`, `not_found`, `unavailable` (download).

### `plugins.catalog` (user)
Params `{}` → `{"categories":[{id,name,icon,color}], "plugins":[{id,name,version,author,description,icon,logo?,color,category,verified,installs,featured?,notes?,source,sha256?,capabilities,contributes,visibleTo,installed,installedVersion?,minCore?,requires?,platforms,incompatible?}], "warning"?, "moved":[{id,name,version}]}`.
`logo` is a `data:image/svg+xml;base64,…` or `data:image/png;base64,…` URL of at most 64 KiB, embedded in the signed catalog by the registry; any other value is dropped (the entry keeps its icon).
Sources:

1. Local: the first existing `catalog.json` of `./plugins` (dev), `/var/lib/ervisio/plugins`, the packaged folder.
   Local files are trusted as they are (only root writes there). A sample is in
   `server/internal/modules/plugins/testdata/catalog-sample.json`.
2. Remote: the https URL on the first line of `/etc/ervisio/plugins-catalog.url` when that file exists and the line is
   not empty, else `plugins.catalog_url` (default `https://ervisio.github.io/plugins/catalog.json`; `""` = no remote
   catalog). Fetched with its `catalog.sig` (10 s each, 2 MiB / 1 KiB, https only, redirects only to https). It is used
   only when the signature verifies (see "Catalog signature"); otherwise it is ignored and `warning` says why. Results
   and failures are cached for 5 minutes per bridge. Remote entries replace local ones with the same id; the remote
   categories replace the local ones when it has any.

A catalog entry may carry `minCore` and/or `requires: {"ervisio": ">=X.Y.Z"}` (as in the manifest; the registry should copy
them from the plugin's manifest when it builds `catalog.json`). The daemon adds `incompatible` (the message) to an entry
this core is too old for, Browse then shows "Needs a newer Ervisio" instead of an Install button, and the installer
checks the downloaded manifest anyway. The registry (Ervisio/plugins) needs nothing else: unsigned fields are ignored by
older cores, and the entry's `capabilities` still have to match the manifest.

Malformed entries are skipped. `moved` lists plugins that left the core and are not installed although the host uses
them ("Plugins that left the core"). `plugins.list` computes `updateAvailable` from the same merged catalog, with the
remote part taken from the cache only (a stale cache is refreshed in the background), so listing never waits for the
network.

### `plugins.exec` (user; admin commands need the root bridge)
Params `{"plugin","command","args":[…]}` → `{"stdout","stderr","exitCode","truncated"?}`. A non-zero exit is a normal result.
Runs only the manifest's argv with the validated arguments, no shell, safe PATH, 4 MiB per stream, timeout `timeoutSec` (30 s default).
A command not declared `admin` is refused on the root bridge (`forbidden`): asking for admin never raises a plugin's rights.
Errors: `not_found` (plugin or command), `forbidden` (disabled, blocked, not visible to the user, user command on the root bridge), `invalid` (argument rejected),
`needs_admin` (admin command from the user bridge, unless the user is in `adminUnlessGroup`: the web client unlocks and retries),
`unavailable` (program missing, timeout). The SDK shortcut is `sdk.api.exec(command, args)`.

Params also take `env` (an environment id, handled by the daemon, see [environments.md](environments.md)) and, set
by the daemon only, `envSocket`: the command must declare `remote`, and `{env}` becomes `unix://<tunnel socket>`
(`unix:///var/run/docker.sock` when there is no `env`). The daemon removes any `envSocket` a browser sends.

### `plugins.execStream` (stream, user)
Same params and checks. Events: `{"stream":"stdout"|"stderr","line":"…"}` per line, then `{"exit":<code>}`; closing the stream kills the process.

### `plugins.pty` (stream, user; admin commands need the root bridge)
Params `{"plugin","command","args":[…],"cols","rows"}` (size 1 to 1000, default 80×24). Only commands declared
`pty: true`, with the same checks as `plugins.exec`; the process gets a new pseudo-terminal (terminal module code),
`TERM=xterm-256color`, the safe PATH and no persistent session. Events: the terminal output as binary chunks
(`b64`), then `{"type":"exit","code"}`. Input (as for `terminal.attach`): `{"type":"input","data":"<base64>"}` and
`{"type":"resize","cols","rows"}`. Closing the stream hangs up and then kills the process group.

### `plugins.http` (user; admin APIs need the root bridge)
Params `{"plugin","name","method","path","query"?,"headers"?:{…},"body"?,"b64"?,"json"?}` →
`{"status","headers":{…},"body","b64"?}`. A non-2xx status is a normal result.
* `path` is the URL path as sent (percent-encoded), without a query. It must start with a single `/` and hold no
  control characters, spaces, `?`, `#`, `\`; no encoded `/`, `\`, NUL, CR or LF (`%2f %5c %00 %0d %0a`); after
  decoding, no empty, `.` or `..` segment and no trailing `/`. The rules are matched against the **decoded** path. The
  request goes out with that path, so the API sees what the rule matched.
* `query` is the raw query string without `?`: printable ASCII without spaces or `#`, at most 8 KiB, passed through as is.
* `headers`: only names listed in the entry's `headers` (case-insensitive); values at most 8 KiB, no CR, LF or NUL.
  `Host` is always `localhost`; nothing else is forwarded (no cookies, no client headers). `json:true` adds
  `Content-Type: application/json` unless the plugin set a Content-Type.
* `body`: text, or base64 with `b64:true`; at most `maxBody` bytes.
* The bridge connects to `socket` with a dedicated transport: no proxy, no keep-alive, no compression, redirects are
  returned and never followed, 64 KiB of response headers at most.
* The result: response headers with repeated values joined by `, ` (`Set-Cookie` dropped); `body` is text, or base64 with
  `b64:true` when it is not UTF-8. The response body is capped at `maxBody` and at 11 MiB (the result travels in one
  protocol line); a larger response is `unavailable`: use `plugins.httpStream`.
* Limits of request bodies: see "Limits of request bodies" below. Anything larger goes through `plugins.upload`.

Errors: `not_found` (plugin or API), `forbidden` (disabled, blocked, not visible, user API on the root bridge, socket
permission denied), `invalid` (method, path, query, header or body not allowed), `needs_admin` (admin API from the user
bridge, unless the user is in `adminUnlessGroup`), `unavailable` (nothing listening, timeout, response too large).

Params also take `env` (and, from the daemon only, `envSocket`): the entry must declare `remote`; the call then
goes to the environment's tunnel socket and the entry's `admin` settings do not apply.

### `plugins.httpStream` (stream, user)
Same params and checks. Events: `{"status","headers"}` when the response starts, then the body as binary chunks as
they arrive (chunked and endless responses included), then `end`. No total timeout (`timeoutSec` bounds the wait for
the headers); closing the stream closes the connection.

#### Limits of request bodies

An inline request body travels inside one JSON message: the `/api/rpc` request of `plugins.http`, or the WebSocket
frame that opens `plugins.httpStream`. The daemon accepts 12 MiB for either (`MaxRPCBody`, `wsOpenLimit`), which carries
a body of up to **8 MiB** (the default `maxBody`) as base64 (11.2 MiB), under the 16 MiB line of the bridge protocol.
The broker refuses a bigger body up front with `invalid` and the way out ("Send large bodies with sdk.api.upload"),
and sends text bodies over 256 KiB as base64, so JSON escaping cannot make a message grow past the limit. Other
frames the browser sends over the WebSocket (stream input) stay at 512 KiB (a bigger one closes the socket with 1009).
A body over the API's own `maxBody` is `invalid` whatever the transport allows.
`plugins.writeFile` carries at most 4 MiB. For bigger bodies use `plugins.upload` (below), which has no such limit.

## Large transfers

`plugins.download` and `plugins.upload` (SDK 0.2) move files of any size between the browser and a plugin's service
without buffering them in memory and without the 16 MiB protocol line or the 8 MiB result cap. The page asks the daemon
for a one-time URL; the bytes then flow through the same bridge that holds the socket permission (the user's, or the
root bridge for `admin` APIs and commands).

### `POST /api/plugins/transfer` (session, `X-Requested-With`)

Asks for a transfer. Body (the page has already checked it against the manifest; the bridge checks it again):

```json
{"kind":"download","plugin":"docker","name":"docker","method":"GET","path":"/v1.43/images/busybox/get",
 "query":"…","headers":{…},"filename":"busybox.tar","admin":false}
{"kind":"download","plugin":"docker","command":"save","args":["busybox"],"filename":"busybox.tar"}
{"kind":"upload","plugin":"docker","name":"docker","method":"POST","path":"/v1.43/images/load","size":2230272,"stream":false}
```

* `download` of an HTTP API: `GET` only, the same method, path, query and header rules as `plugins.http`, the same
  admin requirements (the request is routed to the root bridge when `admin` is true and the API is declared `admin`;
  `needs_admin` if not unlocked, and the web client retries after the unlock dialog).
* `download` of a command: the standard output of a declared, non-pty command, with its argument rules; a command
  that fails before it writes anything is an error with its stderr, one that fails later breaks the download off.
* `upload`: `POST` or `PUT` only, rules checked as for `plugins.http`, `size` at most the API's `maxUpload`; the file
  becomes the request body with `Content-Length: size`; `body` is not allowed. `stream: true` makes the answer a stream
  (below).
* The bridge starts the transfer before this call answers (bridge streams `plugins.httpDownload`,
  `plugins.execDownload`, `plugins.httpUpload`, user level): a refused request is the error of this call. For a
  download, the response status is known too: anything outside 2xx is an error `unavailable` ("The service answered
  404: No such image") with `data.status`, and no file is started.
* Answer `{"result":{"url":"/api/plugins/transfer/<token>","expires":<ms>,"filename"?,"size"?,"status"?}}`. `filename` is
  cleaned (no path, quotes, control or shell characters, 200 bytes at most; `download` when empty), `size` is the
  service's `Content-Length` when it sent one.
* The URL is valid for 60 seconds, **single use**, and bound to the session that asked (another session gets 404 and
  does not use it up). After 60 seconds the stream to the service is closed.
* Limits per session: 8 transfers waiting or running, 30 started per minute (`unavailable` beyond). The origin and
  `X-Requested-With` are checked like every state-changing request.

### `GET /api/plugins/transfer/<token>` (download)

The browser's own download: `200`, `Content-Type: application/octet-stream`, `Content-Disposition: attachment;
filename="…"; filename*=UTF-8''…`, `Content-Length` when known, `Content-Security-Policy: sandbox`, `Cache-Control:
no-store`. The body is piped from the service chunk by chunk (flow control from the browser back to the service). If the
service breaks off, or ends short of its `Content-Length`, the connection is aborted so the browser does not keep a
partial file as complete. The web app starts it from the top-level page (a sandboxed plugin frame cannot download).

### `GET /api/plugins/transfer/<token>/status?wait=<seconds>` (session; downloads only)

The browser fetches a download by itself, so the page cannot see how it ended. The daemon records the outcome per
transfer when the stream ends (or when the link expires unused, the session ends, or the browser cuts it short) and this
endpoint reads it, for the session that started the transfer only (another session, an unknown token, an upload and a
record older than five minutes are all `not_found`). It waits up to `wait` seconds (at most 25) for the end; answer
`{"result":{"done":false}}` when it has not come (ask again) or `{"result":{"done":true,"ok":true|false,"bytes":N,"error"?}}`
with the bytes written to the browser. The web app asks for it when the plugin passed `onDone` and relays the answer to
the frame as `{t:"download-done", did, ok, bytes, error?}`; SDK `sdk.api.download(name, req, filename, { onDone })` and
`downloadCommand(command, args, filename, { env?, onDone })` call `onDone({ ok, bytes, error? })` once.

### `POST /api/plugins/transfer/<token>` (upload, `X-Requested-With`)

The raw file as the body, exactly `size` bytes (`Content-Length` must match when it is sent; more or fewer bytes is
`invalid`). Answer `{"result":{"done":true,"status","headers","body","b64"?,"truncated"?}}`: the service's answer like
`plugins.http` (a non-2xx status is a normal result), its body capped at `maxBody` (`truncated:true` when cut). The
browser reads the file from disk as it sends it, with progress (`XMLHttpRequest.upload.onprogress`); aborting the request
cancels the transfer in the bridge and the service.

With `"stream":true` the answer is `application/x-ndjson` and starts only when the service answers (so the browser keeps
sending the file until then), for services that report progress while they read the file (a Docker build): one JSON
line `{"start":{"status","headers"}}`, then `{"data":"<base64>"}` lines as the body arrives, then `{"done":true,"status",
"headers"}`, or `{"error":{"code","message"}}` if it breaks off. A request refused before the service answered is a
normal JSON error with its HTTP status.

### Bridge methods (not callable by the page directly)

* `plugins.httpDownload` (stream, user): `plugins.httpStream` for `GET` only.
* `plugins.execDownload` (stream, user): params of `plugins.exec`. Events: `{"status":200,"headers":{}}` with the
  first output, then the standard output as binary chunks; a non-zero exit ends the stream with `unavailable`.
* `plugins.httpUpload` (stream, user): params of `plugins.http` without `body`, plus `size` and `stream?`. Events:
  `{"ready":true}` once the rules passed, then it reads inputs `{"data":"<base64>"}` ... `{"eof":true}` (exactly `size`
  bytes, else `invalid`), then `{"done":true,"status","headers","body","b64"?,"truncated"?}`, or with `stream` the
  events `{"status","headers"}`, binary chunks, `{"done":true,"status","headers"}`.

### SDK (0.2)

`sdk.api.download(name, req, filename?)` and `sdk.api.downloadCommand(command, args, filename?)` resolve with
`{filename,size?,status?}` when the download starts; `sdk.api.upload(name, req, file, onProgress | {onProgress,
onResponseStart, onResponseData})` returns a promise with `cancel()`; `sdk.saveFile(filename, data, mime?)` (also
`sdk.api.saveFile`) saves data the plugin already holds; `sdk.audit.list(query)` reads the plugin's activity log
(below). Check `typeof sdk.api.download === "function"` to support older consoles.

### Saving data held in memory (`sdk.saveFile`)

A plugin often has the bytes already (a Blob from the archive API, an exported JSON). The sandboxed frame has no
`allow-downloads` and cannot use a `blob:` URL, so `sdk.saveFile(filename, data, mime?)` hands the data to the app,
which saves it from the top-level page. There is no daemon call (and no activity-log entry: nothing reaches a service).
* `data` is a string (saved as UTF-8), a `Uint8Array` or a `Blob`; at most **64 MiB** (`invalid` above, with a pointer to
  `sdk.api.download`, which streams from a service with no limit). The data crosses the frame boundary by structured
  clone and is held in memory in the frame and in the app until the download is handed to the browser.
* `filename` is cleaned like a download name: no path, no control or bidi characters, none of `< > : " / \ | ? * ; % `` ` ``
  `$`, no leading dots, 200 bytes at most, `download` when nothing is left. `mime` must look like `type/subtype`
  (default `application/octet-stream`); the file is always saved, never shown.
* No user gesture is needed, so it is rate limited per frame: at most 10 files and 256 MiB in any 30 seconds
  (`unavailable` beyond). The browser may still ask the user to allow several downloads.
* Resolves with `{filename,size}` once the download has started.

## Activity log

The daemon records what goes through it in an append-only log (`internal/audit`): one JSON object per line in
`/var/lib/ervisio/audit/audit-YYYY-MM-DD.jsonl` (one file per UTC day, mode 0600, folder 0700; a dev daemon uses
`$XDG_STATE_HOME/ervisio-dev/audit`). Settings `audit.enabled` (default `true`) and `audit.retention_days` (default
90; `0` keeps everything): files older than the retention are deleted when the daemon starts and once a day. Turning it
off stops recording immediately, nothing already written is removed.

```json
{"time":"2026-10-02T14:38:56.115Z","user":"ann","ip":"192.0.2.7","source":"plugin","plugin":"docker","action":"upload",
 "via":"docker","target":"POST /v1.43/images/load?quiet=0","result":"ok","code":200,"bytes":2230272}
```

| field | meaning |
|---|---|
| `time`, `user`, `ip` | when, the signed-in account, the client address (as for sign-in: `X-Forwarded-For` only from a trusted proxy) |
| `source` | `plugin` or `core` |
| `plugin` | the plugin id (also for the removal or switching of a plugin) |
| `action` | plugins: `command`, `pty`, `http`, `upload`, `download`, `file.write`, `file.mkdir`, `file.remove`; core: `login`, `login.failed`, `logout`, `unlock`, `lock`, `plugin.install`, `plugin.uninstall`, `plugin.enable`, `plugin.disable`, `settings`, `notify.channel.add`, `notify.channel.change`, `notify.channel.delete`; jobs (as the owner): `job.run`, `job.webhook`, `job.approve` plus `command`/`http` entries of job steps |
| `via` | the `capabilities.http` API a request went to |
| `target` | `METHOD /path?query`, `command arg arg`, a file path, the source of an install, or `key = value` of a setting |
| `result` | `ok`; `failed` (the call ran, but the command exited non-zero or the service answered 400 or more); `denied` (refused by the rules or the user's rights); `error` (could not run) |
| `code`, `bytes` | exit code or HTTP status; size transferred (uploads, downloads, file writes) |
| `admin`, `detail` | ran with administrator rights; the error message when `result` is not `ok` |
| `env`, `origin` | the environment a call was for when it was not this machine; `via <server> by <user>` when a paired Ervisio server proxied the call (`user` is then the account on this machine), `job <name>` or `webhook` for what a background job did. `env` and `origin` come from the `env` and `via` params of the call; the paired server records what it runs for another one |

What is recorded: every command (`plugins.exec`, `execStream`, `pty` at its first output), every HTTP request that is
not `GET` or `HEAD`, uploads, file writes, folders and removals, downloads (as read entries), and the core actions of
sign-in (success and failure after the limiter), sign-out, administrator unlock and lock, plugin install, removal and
switching, and `config.set`. Calls refused by the rules are recorded too (not the `needs_admin` that the web client
retries after the unlock dialog). Streams are written when they end (with the exit code or status); a download or an
upload when it ends, with the bytes moved.

**Secrets are never stored.** Request headers and bodies are not part of an entry (an `X-Registry-Auth` value cannot
end up in the log). In `target`: query values whose name looks like a secret (`password`, `token`, `secret`, `auth*`,
`credential`, `api key`, `private key`, `cookie`, `session`, `registry config`, `build args`) become `***`; a command's
`--password x` / `--token=x` / `NAME=x` arguments and passwords in URLs (`https://user:***@host`) likewise; each
argument is cut at 256 characters and a target at 1024. A setting's value is stored unless the key is secret.

Code inside the daemon records with `audit.Record(audit.Entry{…})` (`internal/audit`): it writes to the log the daemon
opened when it started serving, and does nothing before that. Background jobs and webhooks use it, with `source: plugin`,
the plugin id, the account the job runs as, and an empty `ip`.

### `audit.list` (user, answered by the daemon)

Params `{"plugin"?,"user"?,"source"?,"action"?,"text"?,"since"?,"until"?,"limit"?,"cursor"?}`: `since` / `until` are
RFC 3339 times, `YYYY-MM-DD`, or Unix milliseconds; `text` matches a part of the target or message (any case);
`limit` 1 to 1000 (default 100). → `{"entries":[…newest first],"next":"<cursor>"|"","enabled":bool}`; pass `next` as `cursor`
for the next page. **Visibility**: root, and an administrator with the unlock active, see every user's entries (and
may filter by `user`); everyone else gets only their own whatever `user` says.

### `plugins.audit.list` (user, answered by the daemon)

The same, but `plugin` is required and only that plugin's entries (`source: plugin`) are returned. The web broker sets
`plugin` to the calling plugin's id, so `sdk.audit.list(query)` is scoped to it; a plugin cannot read the console's
entries or another plugin's. Same visibility by user.

### `GET /api/audit/export?format=csv|json&plugin=&user=&source=&action=&text=&since=&until=`

Every matching entry the user may see, oldest first, as a download (`activity-log-<time>.csv|json`). CSV columns:
`time,user,ip,source,plugin,action,via,target,result,code,bytes,admin,detail,env,origin`; a value starting with `= + - @` gets a
leading `'` so a spreadsheet does not run it. Settings › Activity log (everyone: their own entries; administrators:
all) lists, filters and exports it, and Settings › Sign-in and security links to it.

### Files on a paired server (`env` on the files methods)
`plugins.readFile`, `writeFile`, `listDir`, `mkdir` and `remove` take `env` like `plugins.http` does, **only for an
environment of kind `ervisio`**. The call is relayed to the paired server, where its own user bridge (the mapped user,
never root) runs it under *that* server's manifest of the plugin: the declared folders, the 4 MiB limit and the admin rule
apply there. An `admin` folder works only when the mapped user is root or in the folder's `adminUnlessGroup` (the same as
for commands over a pairing: the pairing never gets administrator rights); otherwise the answer is `needs_admin`. `~` is
not expanded for an environment (use absolute paths). For `tcp-tls`, `ssh` and `portainer-agent` environments `env` on a
files call is refused (`invalid`, "Files are local for this kind of environment"): those kinds tunnel the Docker API only.
The plugin must also declare `remote` on some HTTP entry or command (it opts in to environments at all). Writes, folders
and removals are recorded in the activity log on both servers (`env` on this one, `origin` = `via <server> by <user>` on
the paired one); reads are not logged on either. SDK: `sdk.files.write(path, data, { env })` and the same option on
`read`, `readBytes`, `list`, `mkdir`, `remove`.

### `plugins.readFile` (user)
Params `{"plugin","path","b64"?}` → `{"path","size","data","b64"?}`. `path` is absolute or `~/…` and must be inside a folder of
`capabilities.files.read` or `files.write` (longest match). The file is opened through `os.Root` of that folder, so `..` and
symlinks cannot leave it. 4 MiB max. `data` is text, or base64 with `b64:true` (binary content, or asked for). Always with the
user's own rights (on the root bridge → `forbidden`), except in folders declared `admin`: those run on the root bridge, or as
the user when the user is root or in `adminUnlessGroup`, and answer `needs_admin` otherwise. Errors: `forbidden` (not
declared, outside the folder, disabled/blocked/hidden plugin, permission denied, plain folder on the root bridge),
`needs_admin`, `not_found`, `invalid` (not a regular file, too large).

### `plugins.writeFile` (user)
Params `{"plugin","path","data","b64"?}` → `{path,size}`. `path` must be inside `capabilities.files.write`. Writes a temporary file
in the same folder and renames it over the target (relative to the folder's descriptor: a symlink at the target is replaced, not
followed); keeps the mode of an existing file, else 0644. 4 MiB max (base64 in the 12 MiB `/api/rpc` request). A folder declared
`create: true` is created first when missing. Same errors as `readFile`.

### `plugins.mkdir` (user; SDK v3)
Params `{"plugin","path"}` → `{path}`. Creates the folder and missing parents (0755) inside a `files.write` folder, one
component at a time through `os.Root`; an existing folder is fine, an existing file is `invalid`. Same rules and errors as
`writeFile`.

### `plugins.remove` (user; SDK v3)
Params `{"plugin","path"}` → `{path}`. Removes a file, a symlink (not its target) or an empty folder inside a `files.write`
folder; never the declared folder itself. A folder that is not empty is `invalid`. Same rules and errors as `writeFile`.

### `plugins.listDir` (user)
Params `{"plugin","path"}` → `{"path","entries":[{"name","type":"file|dir|link|other","size","mtime"}]}` (at most 2000, sorted by
name). Same rules as `readFile`.

### `plugins.access` (user; used by the daemon)
Params `{"id"}` → `{"id","network":[…]}` when the plugin exists, is enabled, passes the signature policy and is visible to the
user; any refusal is `not_found`. The daemon asks it before serving `/plugins/<id>/…` or `/plugin-frame/<id>`.

### `plugins.envs.list` (user; answered by the daemon)
Params `{}` → `[{"id","name","kind","address"?,"status"?:{"reachable","engineVersion"?,"apiVersion"?,"latencyMs","error"?,"checked"}}]`: the
environments the user may use (access list), never any secret. `address` is for display: `host:port` (tcp-tls, portainer-agent),
`user@host:port` (ssh), the host of the other server's URL (ervisio; no path, no credentials). It is shown to every user
who may use the environment, which is acceptable: an administrator chose to give them the environment, the list is
already filtered by its access list, the address is not a secret (the keys, passphrases, tokens and certificates
pinned never leave the daemon), and the Docker plugin needs it to tell two hosts with the same name apart. See [environments.md](environments.md).

### `plugins.envCheck` (user; used by the daemon)
Params `{"method","kind","params"}` (the original call). For the files methods it succeeds only for `kind: "ervisio"` when the
plugin declares files and uses environments (above). Otherwise succeeds when the plugin may be used by this user and the HTTP
entry (`params.name`) or command (`params.command`) declares `remote` for a family the environment `kind` serves; else
`forbidden`. The daemon calls it before it hands out a tunnel or routes to a paired server.

### `plugins.network.request` (user)
Params `{"plugin","host","scheme"?:"https"|"http"}` → `{"status":"approved"|"pending","host","scheme"}`. The plugin must
declare `capabilities.network.userHosts`. `host` becomes an exact `host:port` (lower case; port 443, or 80 for http,
when omitted; no wildcard, scheme, path or credentials). `approved` when the manifest's own list covers it or an
administrator already approved it; `pending` otherwise: the web app then asks an administrator in a dialog it owns
(the frame cannot draw or answer it) and calls `plugins.network.approve`. SDK: `sdk.network.request(host)`.

### `plugins.network.approve` / `plugins.network.revoke` (admin)
Params `{"plugin","host","scheme"?}` / `{"plugin","host"}`. Approvals are kept in `/var/lib/ervisio/plugins-hosts.json` (root writes, 0644)
as `{plugin, host, scheme, by, at}`; it is world-readable because every user's bridge reads it, and holds nothing secret.
`plugins.access` adds the approved hosts to the frame's `network` list (an http approval as `http://host:port`, which the
frame's CSP turns into an `http://` source only: not `ws://`, which nobody approved); the web app reloads the plugin's
frames after an approval. An approval is for its scheme only: `plugins.network.request` for `https` stays `pending` when
only `http` was approved, and the other way round. `plugins.network.list` (user) returns `{"approved":[…]}` for Settings.

### `plugins.loadDev` (user)
Params `{"path":"~/projects/my-plugin"}` → `{path,id,name,linked,note?}`. Allowed only in dev mode (`plugins.dev = true` or daemon `--dev`),
else `forbidden`. Validates the folder and remembers it in `~/.config/ervisio/plugins-dev.json`; `linked` is always true
(kept for older clients). `conflict` when the id exists as a system/installed plugin. Works in production too (with `plugins.dev = true`):
the daemon serves `/plugins/<id>/…` of a loaded folder through `plugins.devAsset` on the session's user bridge (see below).

### `plugins.unloadDev` (user)
Params `{"path"}` → `{path}`. Forgets the folder.

### `plugins.devAsset` (stream, user; used by the daemon only)
Params `{"id","file"}`. When `/plugins/<id>/<file>` is not found in the packaged/installed (and, in `--dev`, the repo) folders,
ervisiod opens this stream on the requesting session's user bridge. The bridge looks the id up among the folders that user loaded
with `plugins.loadDev` (developer mode must be on), opens the file through `os.Root` (no `..`, absolute paths or symlinks out of the
folder) with the user's own rights, and sends `{"name","size","mime"}` then base64 chunks (16 MiB max). Any failure is `not_found`.
