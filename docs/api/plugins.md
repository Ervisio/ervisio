# plugins.* (plugin management and execution)

Package `server/internal/modules/plugins`. Web side: `web/src/sections/plugins` (the loader is `web/src/plugins`,
SDK in `web/PLUGIN-SDK.md`). Error codes follow `docs/ARCHITECTURE.md`.

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
                  "admin":true,"adminUnlessGroup":"docker","timeoutSec":60}],
    "files": {"read": ["/srv","~/projects"], "write": []},
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
  run it as themselves. `timeoutSec` default 30, max 600.
* `visibleTo.groups` empty = everyone. Admins (root bridge, root, or members of `sudo`/`wheel`/`admin`) always see everything.

## Signing

`manifest.sig` holds the base64 ed25519 signature of `"linuxadmin-plugin-v1\n" + canonical(manifest.json)`, where the
canonical form is the JSON re-encoded with sorted keys and no whitespace. Because the manifest carries the sha256 of every
other file in `files`, the signature covers the whole folder. Verification also fails when a file differs from its hash,
when a listed file is missing, or when the folder holds a file that is not listed.

The trusted key is `plugins.TeamPublicKey` in `sign.go`: a **placeholder** whose private key does not exist. Replace it
(and re-sign first-party plugins) before release.

```sh
go build -o plugin-sign ./server/internal/modules/plugins/cmd/plugin-sign
plugin-sign -genkey team.key                  # prints the public key to embed
plugin-sign -key team.key plugins/docker      # writes "files" into manifest.json and manifest.sig
plugin-sign -verify -pub <base64> plugins/docker
```

`plugins.allow_unsigned = false` (config): only plugins with a valid signature are listed as enabled, run commands or
install. Present-but-invalid signatures are always refused at install time.

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
`blocked` = unsigned/invalid while `allow_unsigned` is off (`enabled` is then false). `updateAvailable` comes from the
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
Errors: `not_found` (plugin or command), `forbidden` (disabled, blocked, not visible to the user), `invalid` (argument rejected),
`needs_admin` (admin command from the user bridge, unless the user is in `adminUnlessGroup`: the web client unlocks and retries),
`unavailable` (program missing, timeout). The SDK shortcut is `sdk.api.exec(command, args)`.

### `plugins.execStream` (stream, user)
Same params and checks. Events: `{"stream":"stdout"|"stderr","line":"…"}` per line, then `{"exit":<code>}`; closing the stream kills the process.

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
