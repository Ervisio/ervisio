# Ervisio server

Go module `github.com/ervisio/ervisio/server`. Read `docs/ARCHITECTURE.md` first.

```
cmd/ervisiod          daemon: HTTP(S), PAM sign-in, sessions, routing
cmd/ervisio-bridge    does the system work; stdio JSON; one per user (+ one root via sudo)
internal/brand           product name and every derived path/identifier
internal/config          TOML config: defaults, Load/Save (+ .bak), key table, diff
internal/rpc             bridge protocol: Registry/Serve (bridge side), Client (daemon side)
internal/sys             Run/Output/Stream commands, os-release, distro colours, primary IP
internal/modules/<name>  one package per section; all.go registers them
internal/server          HTTP handlers, sessions, rate limit, ws, files, plugins, TLS
internal/bridge          starting bridges (uid/gid switch, sudo unlock)
internal/pam             cgo wrapper around libpam
internal/account         user lookup (uid, groups, canSudo)
tools/devclient          tiny CLI to call /api/rpc and open WebSocket streams
```

## Build and test

```sh
make build-server        # → server/bin/ervisiod, server/bin/ervisio-bridge, server/bin/devclient
make test-server         # go vet + go test ./...
```
Needs Go ≥ 1.24, gcc and the PAM headers (`pam_appl.h`) for cgo.

## Running in development

```sh
make dev                 # ervisiod --dev  (http://127.0.0.1:9090, proxies the UI to Vite on :5173)
./server/bin/ervisiod --dev --web web/dist      # serve a built UI instead of proxying
```
`--dev`: plain HTTP on 127.0.0.1:9090, runs as your user, only **your** user can sign in (PAM still
checks the password), bridges run without changing uid. Unlocking admin rights runs the real
`sudo`. Plugins are also served from `./plugins` (relative to the working directory).

### `--dev-insecure-noauth` (hidden, development only)

```sh
make dev-noauth          # = ervisiod --dev --dev-insecure-noauth --listen 127.0.0.1:9090
```
Every request without a valid session cookie is treated as signed in as the daemon's own user
(one shared session, created at start). No password is needed, so you can test RPC and streams
with curl or `devclient`. It refuses to start without `--dev` and only binds loopback addresses.
POSTs still need `X-Requested-With: ervisio`. Admin calls still need a real unlock (sudo
password), so they answer `needs_admin`.

```sh
curl -s -X POST -H 'X-Requested-With: ervisio' -H 'Content-Type: application/json' \
     -d '{"method":"system.host"}' http://127.0.0.1:9090/api/rpc
go run ./tools/devclient rpc prefs.set '{"key":"theme","value":"oled"}'      # from server/
go run ./tools/devclient -n 3 stream system.metricsStream '{"interval":500}'
```
`devclient` defaults to `http://127.0.0.1:9090`; `-url`, `-cookie <ervisio_session>`, `-admin`.

## Flags (ervisiod)

| flag | default | |
|---|---|---|
| `--config` | `/etc/ervisio/ervisio.conf` | missing file = defaults |
| `--dev` | off | see above |
| `--listen` | config `listen` (`0.0.0.0:9090`; dev `127.0.0.1:9090`) | |
| `--web` | `/usr/share/ervisio/web` (dev: empty = proxy to Vite) | built web app, SPA fallback |
| `--vite` | `http://127.0.0.1:5173` | dev proxy target |
| `--bridge` | `ervisio-bridge` next to the daemon binary | |

The bridge accepts `--admin` (root bridge; refuses unless euid 0) and `--config`.

## Writing a module

```go
package services

func Register(r *rpc.Registry) {
	r.Handle("services.list", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		out, err := sys.Output(ctx, "systemctl", "list-units", "--output=json")
		…
	})
	r.Stream("services.follow", rpc.User, func(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
		var p struct{ Unit string `json:"unit"` }
		if err := c.Bind(&p); err != nil { return err }
		return sys.Stream(ctx, "journalctl", []string{"-fu", p.Unit, "-o", "json"}, func(line string) error {
			return s.Send(json.RawMessage(line))
		})
	})
}
```
- Never use a shell; pass argv. `sys.*` resolves commands in a fixed PATH, sets `LC_ALL=C.UTF-8`,
  bounds output (16 MiB) and time (2 min for Run/Output; Stream has no default timeout).
- Return `rpc.Errorf(code, …)`. Plain `fs` errors map automatically: EACCES → `needs_admin`
  (user bridge) / `forbidden` (root bridge), ENOENT → `not_found`, EEXIST → `conflict`.
- Streams: `s.Send(v)` / `s.SendBytes(b)` block while the client is behind (flow control), so
  just loop; return when `ctx` is done. `s.Input()` closes on cancel.
- The bridge's stdout is the protocol: anything printed to fd 1 is redirected to stderr (logged
  by the daemon), but don't rely on it.
- `c.Admin` tells whether you run in the root bridge.

## Security notes

- Sessions: 256-bit random tokens, stored only as SHA-256; idle timeout `session.timeout`;
  at most 32 sessions per user.
- CSRF: POSTs need `X-Requested-With: ervisio`; a present `Origin` must match the host.
- Login: PAM service `ervisio` (falls back to `login`), `pam_acct_mgmt`, root refused unless
  `allow_root`, failed attempts limited per IP (`login.max_failures` / 15 min), at most 8 PAM
  conversations at once.
- Bridges get the user's uid/gid/supplementary groups, a fresh session (`setsid`), a minimal env.
- Request limits: rpc body 1 MiB, login 16 KiB, ws frame 2 MiB, protocol line 16 MiB.
