# Architecture

Working name: **LinuxAdmin**. The brand string lives in one place per side
(`server/internal/brand/brand.go`, `web/src/brand.ts`) so a rename is a small change.
Identifiers: Go module `github.com/Fonlogen/LinuxAdmin/server`, binaries `linuxadmind` and
`linuxadmin-bridge`, config `/etc/linuxadmin/linuxadmin.conf`, per-user data `~/.config/linuxadmin/`.

The visual design is fixed: see `docs/design/index.html` (approved screens) and the rules in
`docs/DESIGN-RULES.md`. Build what the screens show.

## Processes

```
browser ──HTTPS/WS──▶ linuxadmind (root, one per machine)
                        │  PAM login, sessions, routing, static files
                        ├─ stdio JSON ─▶ linuxadmin-bridge  (runs as the logged-in user)
                        └─ stdio JSON ─▶ linuxadmin-bridge  (runs as root via `sudo -S`, only after "unlock")
```

- **linuxadmind** (`server/cmd/linuxadmind`): HTTP(S) server on `:9090`, serves the built web app,
  authenticates with PAM, keeps sessions (cookie `la_session`, HttpOnly, SameSite=Strict),
  spawns one *user bridge* per session with the user's uid/gid/groups, and routes API calls and
  WebSocket streams to it. It never touches the system on behalf of a user itself.
- **linuxadmin-bridge** (`server/cmd/linuxadmin-bridge`): does all the system work (systemd, files,
  journal, packages, users...). Talks newline-delimited JSON on stdin/stdout.
- **Admin rights (sudo model)**: when the user unlocks, linuxadmind starts a second bridge with
  `sudo -S -p '' -- /path/linuxadmin-bridge --admin` *as the user*, writing the password to stdin.
  sudoers decides: if the user may not sudo, unlock fails with `forbidden`. The root bridge is
  stopped after 5 minutes without admin calls (configurable) or on lock/logout. Calls and streams
  in progress on it (a package transaction, an attached root terminal, a copy, a transfer) count
  as activity: it is never stopped for idleness while one runs, and the 5 minutes restart when it ends.
  If the logged-in user is root, the user bridge already is root and every call is admin.
- **Root login** is refused unless `allow_root = true` in the config.

### Dev mode

`linuxadmind --dev` listens on `127.0.0.1:9090` without TLS, does not need root, only lets the
user running the daemon sign in (PAM still checks the password), spawns the bridge without
changing uid, and proxies the web app to Vite (`http://127.0.0.1:5173`) unless `--web dist/`.
It passes `--dev --dev-plugins <cwd>/plugins` to the bridges, so they know about dev mode explicitly.
It answers only requests whose `Host` is a loopback name or address, and opens no PAM session
around the bridge (that needs root; outside dev the daemon opens one through
`linuxadmind --pam-session-helper`, see `docs/api/auth.md`). `--dev-insecure-noauth` (never as root)
prints a one-time sign-in URL instead of asking for a password.

## Wire protocols

### Browser ↔ linuxadmind

| What | How |
|---|---|
| Health (no auth) | `GET /api/health` → `{status:"ok",version,startedAt}`; polled while the daemon restarts after an update (`docs/api/updates.md`) |
| Public host info for the sign-in page | `GET /api/public/host` → `{hostname, ip?, distro:{id,name,color,logo?,logoUrl?}}`; `GET /api/public/logo` serves the distro logo |
| Sign in / out | `POST /api/auth/login {user,password,remember?}` → same object as session; `POST /api/auth/logout` |
| Sign in with an SSH key | `POST /api/auth/challenge {user,host}` → `{nonce,challenge,host,expires}`; the browser signs `challenge` with the private key (never sent); `POST /api/auth/login-key {user,publicKey,signature,nonce,remember?}` → same object as session. Client: `web/src/auth/sshkey` |
| Current session | `GET /api/auth/session` → `{user,name,uid,home,groups,isRoot,isAdmin,canSudo,unlockedUntil?,authMethod,keyFingerprint?}` or 401 (times in unix ms) |
| Unlock admin | `POST /api/auth/unlock {password}` → `{unlockedUntil}` (`password:""` tries sudo NOPASSWD); `POST /api/auth/lock` |
| Calls | `POST /api/rpc {method, params, admin?:bool}` → `{result}` or `{error:{code,message,data?}}` |
| Streams | `GET /api/ws` (WebSocket), multiplexed channels, see below |
| Downloads | `GET /api/files/download?path=…&admin=0|1` (streamed) |
| Uploads | `POST /api/files/upload?path=…&admin=0|1` raw body (streamed) |
| Plugin assets | `GET /plugins/<id>/<file>` (only plugins the user may use) |
| Plugin frame | `GET /plugin-frame/<id>`: host page of a plugin's sandboxed iframe (`docs/api/plugins.md`, "Isolation") |

Every POST must carry `X-Requested-With: linuxadmin` (CSRF) and JSON bodies `Content-Type: application/json`.
Exact shapes, HTTP statuses and failure reasons: `docs/api/auth.md`.

Error codes (string): `needs_admin`, `forbidden`, `not_found`, `invalid`, `conflict`, `unavailable`,
`internal`, `unauthenticated`. The web client reacts to `needs_admin` by showing the
"Administrator rights needed" dialog, unlocking, and retrying the same call.

WebSocket frames are JSON text:
```
client → {"ch":7,"op":"open","method":"terminal.open","params":{…},"admin":false}
client → {"ch":7,"op":"input","data":…}          // stream input (terminal keys, resize…)
client → {"ch":7,"op":"close"}
server → {"ch":7,"op":"data","data":…}
server → {"ch":7,"op":"end"}  |  {"ch":7,"op":"error","error":{code,message}}
```
Binary-ish payloads (terminal output, file chunks) are base64 strings in `data` with `"b64":true`.

### linuxadmind ↔ bridge (stdio, one JSON object per line)

```
→ {"id":1,"method":"services.list","params":{}}
← {"id":1,"result":[…]}                    | {"id":1,"error":{"code":"…","message":"…"}}
→ {"id":2,"method":"logs.follow","params":{…},"stream":true}
← {"id":2,"event":"data","data":…}  …  {"id":2,"event":"end"}
→ {"id":2,"input":…}      → {"id":2,"cancel":true}
← {"hello":{"version":"…","uid":1000,"admin":false,"methods":{"services.list":"user","services.restart":"admin",…},"streams":["logs.follow",…]}}   // first line
← {"id":2,"event":"data","data":"<base64>","b64":true}   // binary chunk (Stream.SendBytes)
↔ {"id":2,"ack":n}                                       // flow control
```
A stream ends with `event:"end"` or with `{"id":2,"error":{…}}`; a cancelled stream ends with `end`.
Flow control: at most 64 unacknowledged stream messages per direction (events from the bridge,
inputs from the daemon); the receiver acks as its consumer takes them, and `Stream.Send` blocks
meanwhile, so a slow browser never stalls other calls on the same bridge.

## Bridge module API (Go)

```go
// server/internal/rpc
type Level int // rpc.User: runs in the user bridge; rpc.Admin: needs the root bridge
r.Handle("services.list", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) { … })
r.Stream("logs.follow", rpc.User, func(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
    s.Send(v)           // event data
    in := s.Input()     // <-chan json.RawMessage
})
c.Bind(&params)         // decode params, returns rpc.Invalid on error
rpc.Errorf(rpc.NotFound, "unit %s not found", name)
```

Each section is a package `server/internal/modules/<name>` exposing `func Register(r *rpc.Registry)`.
`server/internal/modules/all.go` registers every module (already wired, one line each).
Calls with `admin:true` go to the root bridge even for `User` methods (e.g. browsing `/root`).
Helpers for running commands live in `server/internal/sys` (`sys.Run`, `sys.Output`, `sys.Stream`).

## Web app

React 18 + TypeScript + Vite in `web/`. No UI library: own components in `web/src/ui/`
following `docs/DESIGN-RULES.md`. Routing with react-router. Terminal with `@xterm/xterm`.

- `web/src/api/` — `call<T>(method, params, {admin})`, `stream(method, params, handlers)`,
  auth helpers. Handles `needs_admin` → unlock dialog → retry.
- `web/src/theme/` — themes, colour modes, tokens as CSS variables on `:root`.
- `web/src/i18n/` — `useT('services')`, dictionaries `web/src/i18n/<lang>/<namespace>.json`.
  English is the default, Italian is complete. Every visible string goes through `t()`.
- `web/src/shell/` — rail, top bar, dock, More sheet, routing frame, host/user menu.
- `web/src/ui/` — Button, Input, Select, Switch, Checkbox, Radio, Segmented, Badge, Tabs, Panel,
  Sheet, Dialog, ConfirmDialog (type-to-confirm), Toast, Menu, Tooltip, EmptyState, Progress,
  Skeleton, Table (zebra rows, no lines), Icon.
- `web/src/sections/<name>/` — one folder per section, default export the page component.
  `web/src/sections/index.ts` lists every section (id, path, icon, colour, page). Already wired.
- `web/src/plugins/` — plugin loader and SDK.

## Preferences

Per Linux user, stored by the user bridge in `~/.config/linuxadmin/prefs.json`
(`prefs.get`, `prefs.set {key, value}`): theme, colourMode, language, density, reduceMotion,
terminal options, dashboard layout, file manager bookmarks, snippets, log watchers.
Theme/language are also cached in localStorage so the sign-in page renders in them.

## Server configuration

`/etc/linuxadmin/linuxadmin.conf` (TOML), read by linuxadmind, editable from Settings through
`config.get` / `config.set` (admin, writes a backup first):

```toml
listen = "0.0.0.0:9090"
allow_root = false
[login]
show_ip = true
max_failures = 5
[auth]
ssh_keys = true        # sign in with a key from ~/.ssh/authorized_keys (signed in the browser)
[session]
timeout = "12h"
admin_unlock = "5m"
[tls]
mode = "self-signed"   # self-signed | letsencrypt | custom
redirect = true       # plain HTTP on the same port is redirected to HTTPS
cert = ""              # tls.mode = custom
key = ""
[plugins]
allow_unsigned = false
dev = false
[updates]
channel = "stable"     # stable | prerelease
auto_check = true
auto_install = false
auto_install_at = "03:30"
```
Key table, types and the `config.*` methods: `docs/api/config.md`.

## Plugins

A plugin is a folder with `manifest.json`, a frontend ES module and optional assets, installed in
`/usr/share/linuxadmin/plugins/<id>` (packaged) or `/var/lib/linuxadmin/plugins/<id>` (from Browse).

```json
{
  "id": "docker", "name": "Docker", "version": "1.4.0", "author": "…",
  "entry": "index.js", "icon": "server", "color": "file",
  "capabilities": {
    "commands": [{"name": "ps", "argv": ["docker","ps","--format","{{json .}}"], "admin": false}],
    "files": {"read": ["/srv"], "write": []},
    "sockets": ["/var/run/docker.sock"]
  },
  "contributes": {"pages": [{"id":"docker","title":"Docker","icon":"server"}], "widgets": […], "snippets": […]},
  "visibleTo": {"groups": ["docker","wheel"]}
}
```

Plugins never run arbitrary code on the server: they call `plugins.exec {plugin, command, args}`,
which runs only the `argv` declared in the manifest (with `{0}`-style argument slots validated by
pattern), as user or admin as declared. Since SDK v3 a command may be declared `pty` (run in a
pseudo-terminal through `plugins.pty`, reusing the terminal module's pty code), `capabilities.http`
lets a plugin call an HTTP API on a unix socket for declared methods, paths and headers only
(`plugins.http` / `plugins.httpStream`; the bridge connects as the user, or as root for `admin`
entries), and folders may be declared `admin` or `create` (`docs/api/plugins.md`). Their frontend never runs in the app either: each page or widget
runs in an `<iframe sandbox="allow-scripts allow-forms">` (opaque origin, strict CSP) and talks to the app only through a
`postMessage` broker that allows the declared commands and folders (`web/PLUGIN-SDK.md`). Signed plugins
carry `manifest.sig` (ed25519, team key: `docs/PLUGIN-SIGNING.md`); unsigned ones are blocked by default.

## Releases and self-update

Tags `vX.Y.Z` build signed release archives on GitHub (`.github/workflows/release.yml`). Installed consoles live in
`/usr/lib/linuxadmin/versions/<v>` with a `current` symlink; `updates.apply` downloads and verifies a release
(ed25519 over `SHA256SUMS`, key in `server/internal/update/sign.go`), installs it next to the running one and hands
off to `linuxadmind --apply-update` in a transient systemd unit, which switches `current`, restarts the service and
rolls back if `/api/health` does not report the new version within 30 s. Details: `docs/RELEASING.md`,
`docs/api/updates.md`. New machines are installed by `install.sh` into that layout. Distribution packages (`.deb`,
`.rpm`, AUR) use a flat layout instead (`/usr/bin/linuxadmind`, `/usr/lib/linuxadmin/linuxadmin-bridge`,
`/usr/share/linuxadmin/{web,plugins}`) and write `/usr/lib/linuxadmin/managed`, which turns self-update off:
`docs/PACKAGING.md`.

## Repository layout

```
server/            Go: cmd/linuxadmind, cmd/linuxadmin-bridge, internal/…
web/               React app
plugins/           first-party example plugins
docs/              ARCHITECTURE.md, DESIGN-RULES.md, api/<module>.md, design/
packaging/         systemd unit, PAM files per distribution, install scripts, build-release.sh, nfpm (.deb/.rpm), AUR
.github/workflows/ ci.yml (vet/test/lint/build), release.yml (tag → signed GitHub release)
```

## Ownership while building in parallel

Each section agent owns only `server/internal/modules/<name>/`, `web/src/sections/<name>/`,
`web/src/i18n/*/<name>.json` and `docs/api/<name>.md`. Shared code (`rpc`, `sys`, `api`, `ui`,
`shell`, `theme`) is owned by the foundation; if a section needs something there, it adds it in a
new file inside its own folder or reports it.

## Future: desktop app

A desktop build (Wails or Tauri, not Electron) may later wrap the same web app for Linux desktops.
Keep it possible: the web client takes its API base URL from one place (`web/src/api/base.ts`,
default same origin) and never assumes same-origin elsewhere; authentication is behind one
interface on both sides so a local mode can authenticate by Unix socket peer credentials
(SO_PEERCRED) and elevate with polkit instead of PAM + sudo.
