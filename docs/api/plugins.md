# plugins.* (plugin management and execution)

Package `server/internal/modules/plugins`. Web side: `web/src/sections/plugins` (the sandboxed frames and the broker are
in `web/src/plugins`, SDK in `web/PLUGIN-SDK.md`). Error codes follow `docs/ARCHITECTURE.md`.

## Isolation

Plugin code never runs in the app's origin. linuxadmind serves:

| Path | What | Checks |
|---|---|---|
| `GET /plugin-frame/<id>` | Empty host page of a plugin frame, embedded by the app in `<iframe sandbox="allow-scripts">`. It loads `/plugin-runtime.js` (built from `web/src/plugins/frame`). | session; `plugins.access` |
| `GET /plugins/<id>/<file>` | Plugin files. The app fetches the entry module and hands it to the frame; `sdk.asset()` goes through the app too. | session; `plugins.access` |

`plugins.access` (below) runs on the session's user bridge: the plugin must exist, be enabled, pass the signature policy
and be visible to the user (`visibleTo`); otherwise both paths answer 404. Frame headers: `Content-Security-Policy:
default-src 'none'; script-src 'nonce-…' blob:; style-src 'unsafe-inline'; img-src data: blob:; font-src data:;
media-src data: blob:; connect-src <https:// and wss:// of capabilities.network, else 'none'>; form-action 'none';
base-uri 'none'; frame-ancestors 'self'; sandbox allow-scripts`, no `X-Frame-Options`, `Cache-Control: no-store`.
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
| `system` | `/usr/share/linuxadmin/plugins/<id>` | packaged, removed by the package manager |
| `installed` | `/var/lib/linuxadmin/plugins/<id>` | from Browse / `plugins.install`, removable |
| `dev` | `./plugins` of the daemon's working directory (daemon `--dev` only, passed to the bridge as `--dev --dev-plugins <dir>`), plus folders from `plugins.loadDev` | loaded folders only when `plugins.dev = true` or the daemon runs in `--dev` |

The folder name must equal the manifest `id`. On duplicate ids the order is dev, system, installed.
Enabled state: `/var/lib/linuxadmin/plugins-state.json` (`{"enabled":{"docker":false}}`, missing = enabled).

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
                        {"path":"~/.config/linuxadmin/plugins/docker","create":true}]},
    "sockets": ["/var/run/docker.sock"],
    "network": ["example.org"]
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
  * `maxBody` caps the request and the response body (default 8 MiB, max 64 MiB). `timeoutSec` (default 30, max 600)
    bounds `plugins.http`, and the wait for the response headers of `plugins.httpStream`.
* `capabilities.sockets`: informational. It lists the sockets the plugin's commands talk to (for example `docker`
  talking to `/var/run/docker.sock`). A plugin opens a socket itself only through `capabilities.http`.
* `capabilities.network`: host names (`api.example.org`, `*.example.org`, `host:8443`; nothing else, the entries go into
  a CSP header). The plugin frame may connect to them over https/wss; the user's session cookie is never sent from the
  frame.

## Signing

`manifest.sig` holds the base64 ed25519 signature of `"linuxadmin-plugin-v1\n" + canonical(manifest.json)`, where the
canonical form is the JSON re-encoded with sorted keys and no whitespace. Because the manifest carries the sha256 of every
other file in `files`, the signature covers the whole folder. Verification also fails when a file differs from its hash,
when a listed file is missing, or when the folder holds a file that is not listed.

The trusted key is `plugins.TeamPublicKey` in `sign.go`, the public half of the LinuxAdmin team key. The private key
is never in the repository: where it lives, who may use it and how to rotate it is in `docs/PLUGIN-SIGNING.md`.
The first-party `plugins/docker` is signed with it (`TestShippedDockerSigned` checks this).

```sh
go build -o plugin-sign ./server/internal/modules/plugins/cmd/plugin-sign
plugin-sign -genkey team.key                  # prints the public key to embed
plugin-sign -key team.key plugins/docker      # writes "files" into manifest.json and manifest.sig
plugin-sign -verify plugins/docker            # against the embedded key (or -pub <base64>)
```

`plugins.allow_unsigned = false` (config, **the default**): only plugins with a valid signature are listed as enabled, run
commands, are served or install. Exception: plugins in the dev location (the repository's `./plugins` under `linuxadmind
--dev`, or folders loaded with `plugins.loadDev`) run unsigned while developer mode is on; `plugins.list` marks them
`devUnsigned` and the UI shows an "Unsigned, dev" badge. Present-but-invalid signatures are always refused at install
time.

## Methods

### `plugins.list` (user)
Params `{}`. Result: array of

```json
{"id":"docker","name":"Docker","version":"1.4.0","author":"…","description":"…","icon":"server","color":"file",
 "entry":"index.js","enabled":true,"signed":true,"verified":true,"signatureError":"…","capabilities":{…},
 "contributes":{…},"visibleTo":{"groups":[]},"location":"installed","removable":true,"dir":"/var/lib/…",
 "unloadable":false,"updateAvailable":{"version":"1.5.0","notes":"…","newPermissions":false,"source":"https://…","sha256":"…"},
 "blocked":false,"error":""}
```
`capabilities` and `contributes` always carry arrays (never null). `signed` = a manifest.sig exists, `verified` = it is valid.
`blocked` = unsigned/invalid while `allow_unsigned` is off (`enabled` is then false); `devUnsigned` = an unsigned dev
plugin that runs because developer mode is on. `updateAvailable` comes from the
local catalog. Folders whose manifest fails validation are listed for admins only, with `error` set and `enabled:false`.
Other users only see plugins allowed by `visibleTo`.

### `plugins.setEnabled` (admin)
Params `{"id":"docker","enabled":false}` → `{id, enabled}`. `not_found` for an unknown id.

### `plugins.uninstall` (admin)
Params `{"id"}` → `{id}`. Only plugins in `/var/lib/linuxadmin/plugins`; others → `forbidden`, unknown → `not_found`, bad id → `invalid`.

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
Params `{}` → `{"categories":[{id,name,icon,color}], "plugins":[{id,name,version,author,description,icon,color,category,verified,installs,featured?,notes?,source,sha256?,capabilities,contributes,visibleTo,installed,installedVersion?}], "warning"?}`.
Sources: the first existing `catalog.json` of `./plugins` (dev), `/var/lib/linuxadmin/plugins`, `/usr/share/linuxadmin/plugins`
(the repo ships a sample in `plugins/catalog.json`), then, optionally, the https URL on the first line of
`/etc/linuxadmin/plugins-catalog.url` (5 s timeout, 2 MiB, cached 5 min; entries override local ones by id; a failure only sets `warning`).
Malformed entries are skipped.

### `plugins.exec` (user; admin commands need the root bridge)
Params `{"plugin","command","args":[…]}` → `{"stdout","stderr","exitCode","truncated"?}`. A non-zero exit is a normal result.
Runs only the manifest's argv with the validated arguments, no shell, safe PATH, 4 MiB per stream, timeout `timeoutSec` (30 s default).
A command not declared `admin` is refused on the root bridge (`forbidden`): asking for admin never raises a plugin's rights.
Errors: `not_found` (plugin or command), `forbidden` (disabled, blocked, not visible to the user, user command on the root bridge), `invalid` (argument rejected),
`needs_admin` (admin command from the user bridge, unless the user is in `adminUnlessGroup`: the web client unlocks and retries),
`unavailable` (program missing, timeout). The SDK shortcut is `sdk.api.exec(command, args)`.

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
* Transport limits: a `/api/rpc` request is at most 1 MiB, so a request body sent with `plugins.http` is at most about
  750 KiB (base64) or 1 MiB (text); the WebSocket frame that opens `plugins.httpStream` is at most 512 KiB.

Errors: `not_found` (plugin or API), `forbidden` (disabled, blocked, not visible, user API on the root bridge, socket
permission denied), `invalid` (method, path, query, header or body not allowed), `needs_admin` (admin API from the user
bridge, unless the user is in `adminUnlessGroup`), `unavailable` (nothing listening, timeout, response too large).

### `plugins.httpStream` (stream, user)
Same params and checks. Events: `{"status","headers"}` when the response starts, then the body as binary chunks as
they arrive (chunked and endless responses included), then `end`. No total timeout (`timeoutSec` bounds the wait for
the headers); closing the stream closes the connection.

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
followed); keeps the mode of an existing file, else 0644. 4 MiB max (and the 1 MiB `/api/rpc` request limit). A folder declared
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

### `plugins.loadDev` (user)
Params `{"path":"~/projects/my-plugin"}` → `{path,id,name,linked,note?}`. Allowed only in dev mode (`plugins.dev = true` or daemon `--dev`),
else `forbidden`. Validates the folder and remembers it in `~/.config/linuxadmin/plugins-dev.json`; `linked` is always true
(kept for older clients). `conflict` when the id exists as a system/installed plugin. Works in production too (with `plugins.dev = true`):
the daemon serves `/plugins/<id>/…` of a loaded folder through `plugins.devAsset` on the session's user bridge (see below).

### `plugins.unloadDev` (user)
Params `{"path"}` → `{path}`. Forgets the folder.

### `plugins.devAsset` (stream, user; used by the daemon only)
Params `{"id","file"}`. When `/plugins/<id>/<file>` is not found in the packaged/installed (and, in `--dev`, the repo) folders,
linuxadmind opens this stream on the requesting session's user bridge. The bridge looks the id up among the folders that user loaded
with `plugins.loadDev` (developer mode must be on), opens the file through `os.Root` (no `..`, absolute paths or symlinks out of the
folder) with the user's own rights, and sends `{"name","size","mime"}` then base64 chunks (16 MiB max). Any failure is `not_found`.
