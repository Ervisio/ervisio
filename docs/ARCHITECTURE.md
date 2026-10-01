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
  stopped after 5 minutes without admin calls (configurable) or on lock/logout.
  If the logged-in user is root, the user bridge already is root and every call is admin.
- **Root login** is refused unless `allow_root = true` in the config.

### Dev mode

`linuxadmind --dev` listens on `127.0.0.1:9090` without TLS, does not need root, only lets the
user running the daemon sign in (PAM still checks the password), spawns the bridge without
changing uid, and proxies the web app to Vite (`http://127.0.0.1:5173`) unless `--web dist/`.

## Wire protocols

### Browser ↔ linuxadmind

| What | How |
|---|---|
| Public host info for the sign-in page | `GET /api/public/host` → `{hostname, ip?, distro:{id,name,color,logo?}}` |
| Sign in / out | `POST /api/auth/login {user,password}` → `{user,isAdmin,isRoot}`; `POST /api/auth/logout` |
| Current session | `GET /api/auth/session` → `{user,uid,isAdmin,canSudo,unlockedUntil?}` or 401 |
| Unlock admin | `POST /api/auth/unlock {password}` → `{unlockedUntil}`; `POST /api/auth/lock` |
| Calls | `POST /api/rpc {method, params, admin?:bool}` → `{result}` or `{error:{code,message,data?}}` |
| Streams | `GET /api/ws` (WebSocket), multiplexed channels, see below |
| Downloads | `GET /api/files/download?path=…&admin=0|1` (streamed) |
| Uploads | `POST /api/files/upload?path=…&admin=0|1` raw body (streamed) |
| Plugin assets | `GET /plugins/<id>/<file>` |

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
← {"hello":{"version":"…","uid":1000,"admin":false,"methods":{"services.list":"user","services.restart":"admin",…}}}   // first line
```

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
[session]
timeout = "12h"
admin_unlock = "5m"
[tls]
mode = "self-signed"   # self-signed | letsencrypt | custom
redirect = true
[plugins]
allow_unsigned = true
dev = false
```

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
pattern), as user or admin as declared. Signed plugins carry `manifest.sig` (ed25519).

## Repository layout

```
server/            Go: cmd/linuxadmind, cmd/linuxadmin-bridge, internal/…
web/               React app
plugins/           first-party example plugins
docs/              ARCHITECTURE.md, DESIGN-RULES.md, api/<module>.md, design/
packaging/         systemd unit, PAM file, PKGBUILD (later)
```

## Ownership while building in parallel

Each section agent owns only `server/internal/modules/<name>/`, `web/src/sections/<name>/`,
`web/src/i18n/*/<name>.json` and `docs/api/<name>.md`. Shared code (`rpc`, `sys`, `api`, `ui`,
`shell`, `theme`) is owned by the foundation; if a section needs something there, it adds it in a
new file inside its own folder or reports it.
