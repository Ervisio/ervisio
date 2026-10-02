# Docker plugin v2: working plan and shared contract

Working document for the agents building Docker plugin v2. Delete it once the work ships.
Approved designs: `.variant-studio/rounds/025..031` (open the chosen variant's `.html`; generators in
`.variant-studio/gen/r25..r31*.py`). Chosen: 025 b (home), 026 a (container page), 027 a (stack page),
028 b (new container wizard), 030 b (templates), 031 a (images / volumes / cleanup).

## 1. Platform contract: plugin SDK v3

SDK v2 stays valid; v3 adds three capabilities. `sdk.version` becomes `3`.

### 1.1 `capabilities.http`: HTTP over a unix socket

```json
"http": [{
  "name": "docker",
  "socket": "/var/run/docker.sock",
  "admin": true,
  "adminUnlessGroup": "docker",
  "headers": ["Content-Type", "X-Registry-Auth"],
  "rules": [
    {"methods": ["GET"], "path": "/v1\\.[0-9]+/containers/json"},
    {"methods": ["POST"], "path": "/v1\\.[0-9]+/containers/[a-zA-Z0-9_.-]+/(start|stop|restart|kill|pause|unpause)"}
  ],
  "maxBody": 8388608,
  "timeoutSec": 60
}]
```

* `socket`: an absolute path. The daemon connects to it as the user, or as root when admin was unlocked.
* `rules[].path`: a Go regexp that must match the whole URL path (no query string). The query string is passed through
  as is. Methods are upper case.
* `headers`: the only request headers the plugin may set. `Host` is fixed, and nothing else is forwarded.
* `admin` / `adminUnlessGroup` work as for commands.
* `maxBody` caps the request and response bodies (default 8 MiB, max 64 MiB). `timeoutSec` defaults to 30 and is at
  most 600; streams have no total timeout.
* `capabilities.sockets` (a string array) stays informational.

SDK:

```ts
sdk.api.http(name, {method, path, query?: Record<string,string|string[]>|string, headers?, body?: string|Uint8Array|object})
  -> Promise<{status: number, headers: Record<string,string>, body: string, json(): any, bytes(): Uint8Array}>
```

An object `body` is sent as JSON with `Content-Type: application/json`. A non-2xx status is a normal result, not an
error.

```ts
sdk.api.httpStream(name, {method, path, query?, headers?, body?},
    {onStart?(status, headers), onData(chunk: Uint8Array), onEnd(), onError(err)}) -> {close()}
```

This covers logs `follow`, `stats` `stream=true`, `/events`, image pull progress, and `exec start` without a tty.
Docker frames multiplexed log streams, so the plugin demultiplexes them itself.

Daemon methods: `plugins.http` and `plugins.httpStream` (stream). Errors follow plugins.exec: `not_found`, `forbidden`,
`invalid` (rule or header not allowed), `needs_admin`, `unavailable`.

### 1.2 PTY commands

A command may set `"pty": true`. It can then run only through:

```ts
sdk.api.pty(command, args, {cols, rows, onData(chunk: Uint8Array), onExit(code), onError(err)})
  -> {write(data: string|Uint8Array), resize(cols, rows), close()}
```

The pty uses the terminal module's code. The Docker plugin declares, for example,
`{"name":"shell","pty":true,"argv":["docker","exec","-it","-e","TERM=xterm-256color","{0}","{1}"],"args":[{container id pattern},{"pattern":"/bin/(sh|bash|ash|zsh)"}],"admin":true,"adminUnlessGroup":"docker"}`.
The daemon method is `plugins.pty` (a bidirectional stream).

### 1.3 Admin folders and auto-created home folders

A `capabilities.files.read` / `.write` entry may be a string (as today) or an object:
`{"path":"/opt/stacks","admin":true,"adminUnlessGroup":"docker","create":true}`.

* An `admin` folder is accessed on the root bridge, unless the user is in `adminUnlessGroup`. When the user needs admin,
  the broker unlocks and retries, as for commands.
* With `create: true` the daemon creates the folder (0755, or 0700 under `~`) when a write targets it and it is missing.
* New SDK calls are `sdk.files.mkdir(path)` and `sdk.files.remove(path)` (a file, or an empty folder), only inside
  `write` folders.

### 1.4 As built (implementation notes)

The contract above is implemented as written; these are the details and limits it did not spell out.

* Transport limits: one `plugins.http` response body is at most `min(maxBody, 11 MiB)` (the result travels base64 in
  one 16 MiB protocol line); larger answers are `unavailable` and must use `httpStream`. A `plugins.http` request goes
  through `/api/rpc` (1 MiB per request), so request bodies are at most about 750 KiB as bytes (base64) or 1 MiB as
  text; the WebSocket frame opening `httpStream` is at most 512 KiB. Docker JSON bodies fit easily; build contexts or
  image loads would not.
* `path` is matched after percent-decoding and must be clean: one leading `/`, no `..`/`.`/empty segments, no trailing
  `/`, no encoded `/` `\` NUL CR LF, no `?`/`#`, spaces or control characters. Encode path parts with
  `encodeURIComponent`. The query string must be printable ASCII without spaces or `#` (≤ 8 KiB); the SDK encodes an
  object query with `URLSearchParams`.
* Response `headers`: one string per name (repeated values joined with `, `), `Set-Cookie` dropped. `Host` is
  `localhost`. Redirects are returned, never followed.
* `headers` in the manifest: any header name except `Host`, `Cookie`, `Authorization`, `Proxy-Authorization`,
  hop-by-hop/framing headers (`Connection`, `Upgrade`, `Transfer-Encoding`, `Content-Length`, `Keep-Alive`, `TE`,
  `Trailer`, `Expect`), `Forwarded`, `Via`, `Origin`, `Referer`, `Proxy-*`, `Sec-*`, `X-Forwarded-*`. An object `body`
  sets `Content-Type: application/json` even when `Content-Type` is not listed.
* `httpStream`: the first event is `onStart(status, headers)`; `timeoutSec` bounds the wait for the headers only.
* `socket` is at most 107 bytes. `http` entries: at most 16, each with 1 to 256 rules and at most 32 headers.
* `pty` commands are refused by `exec` / `execStream` (`invalid`), and `pty` refuses other commands. `cols`/`rows` 1 to
  1000 (default 80×24). `write()` splits large input into 64 KiB pieces.
* Admin folders cannot be under `~`. When one path falls in several entries, the longest folder wins (tie: the entry
  without `admin`). `create` also applies to `files.mkdir`. `files.remove` never removes the declared folder itself and
  removes a symlink, not its target.
* A frame may have 32 streams open (was 8); all are closed when the frame goes away.

## 2. Docker plugin manifest (target)

* `http`: one entry named `docker`. It covers the Engine API endpoints the UI uses: containers (json, inspect, create,
  start, stop, restart, kill, pause, unpause, remove, rename, update, logs, stats, top, exec create, exec start,
  changes), images (json, inspect, history, create, pull, tag, remove, prune, distribution), volumes, networks
  (including connect and disconnect), system (df, info, version, events, ping), containers prune and build prune.
  Admin unless in the `docker` group.
* `commands`:
  * `shell` (pty).
  * `compose-up`, `compose-down`, `compose-pull`, `compose-restart`, `compose-config` and `compose-ls` run
    `docker compose`, with the project name and folder validated by pattern: the project matches
    `[a-z0-9][a-z0-9_-]{0,62}`, and the file path is under `/opt/stacks/<project>/`. Streamed commands go through
    execStream.
  * `stacks-init`, an admin command: `install -d -m 2775 -g docker /opt/stacks`.
* `files`:
  * `{"path":"/opt/stacks","admin":true,"adminUnlessGroup":"docker"}` for read and write.
  * `{"path":"~/.config/linuxadmin/plugins/docker","create":true}` for write. It holds the settings, registries,
    alert rules and template sources as JSON (mode 0600 for the registries file).
* `network`: `raw.githubusercontent.com` and `gist.githubusercontent.com`, for Portainer-format template URLs.
* Pages: `docker` (one page; the plugin routes internally). The widget is `containers`.

## 3. Plugin source and build

* The source lives in `plugins-src/docker/` (TypeScript + React through `sdk.react`, never bundling React; the build
  maps `react` to a shim that re-exports `sdk.react`).
* The build output is the single file `plugins/docker/index.js`, built with Vite lib mode or esbuild from
  `web/node_modules`. `npm --prefix plugins-src/docker run build` must work.
* xterm (`@xterm/xterm`, `@xterm/addon-fit`) is bundled. Its CSS is injected with a `<style>`.
* `plugins/docker/templates.json` is our curated template catalog, signed with the plugin and loaded via `sdk.asset`.
* After a build, re-sign with `plugin-sign -key ~/.config/linuxadmin-signing/team.key plugins/docker`. Only the
  coordinator does this; agents never touch the key.

## 4. Feature map to designs

| Area | Design | Notes |
|---|---|---|
| Shell, inner nav Workloads / Resources / Tools, home with container cards grouped by stack, live CPU/mem | 025 b | bulk select + bulk bar (start/stop/restart/remove) |
| Container page: Overview / Logs / Stats / Shell / Inspect / Settings | 026 a | no dialogs except destructive confirms; logs live with search; stats charts; Settings = restart policy, rename, limits (update), recreate |
| Stacks list + stack page (compose.yml / .env / Diff editor, validation, deploy output) | 027 a | /opt/stacks managed; other compose projects detected through labels, read-only + "Move to /opt/stacks" |
| New container wizard (image, ports+storage, env, network+limits, review) and "Edit" = recreate with same form | 028 b | |
| Templates store + app page with install form | 030 b | own catalog + Portainer v2/v3 JSON URLs |
| Images / Volumes / Networks / Cleanup pages with the shared disk bar | 031 a | update check via `/distribution/{name}/json` digest vs local RepoDigests |
| Registries | – | stored credentials, sent as X-Registry-Auth on pull |
| Auto-update | – | deploys a watchtower container (image configurable, default `nickfedor/watchtower`) with schedule, label opt-in, cleanup |
| Alerts | – | rules (stopped, restart loop, unhealthy, CPU/mem thresholds) checked while LinuxAdmin is open, from `/events` + stats; app toasts |
