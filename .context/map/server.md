# Server
Purpose: Go daemon and per-session bridges for authenticated host administration.

## Entry points and public contracts
- `server/cmd/ervisiod/main.go`: `main` / `run` load options and create `server.New(server.Options)`.
- `server/internal/server/server.go`: `Options`, `Server`, `New`, `Handler`, `Run`; HTTP routes and daemon lifecycle.
- `server/cmd/ervisio-bridge/main.go`: register modules, then `rpc.Serve` on stdin and protocol stdout.
- `server/internal/modules/all.go`: `RegisterAll`; modules expose `Register(*rpc.Registry)`.
- `server/internal/rpc/registry.go`: `Level` (`User` / `Admin`), `Call`, `Handler`, `StreamHandler`, `Stream`, `Registry`.
- `server/internal/rpc/client.go`: daemon-side `Client` / `ClientStream`; `protocol.go`: `Hello` / `Message` wire types.
- `server/internal/bridge/bridge.go`: `Spec`, `Proc`, `StartUser`, `StartAdmin`; process and activity lifecycle.
- `server/internal/config/config.go`: `Config`, defaults, validation, TOML load/save with backup.

## Flow
- Browser -> authenticated HTTP RPC or multiplexed WebSocket -> session routing -> user/admin bridge -> module -> system helpers.
- Bridge transport is newline-delimited JSON with an initial hello, method access levels, calls, streams, cancellation and acknowledgements.
- `server/internal/server/route.go` chooses the unlocked admin bridge for admin-level methods or `admin:true`; root sessions use their user bridge.
- Daemon-owned jobs, notifications, audit and environments are exceptions to normal bridge dispatch; inspect their glue before changing routing.
- `internal/jobs`, `internal/notify`, `internal/audit`, `internal/envs` retain daemon state; `internal/modules/plugins` handles bridge-side plugin capabilities.

## Invariants
- Keep privilege boundaries: Linux user bridges use the account's uid/gid/groups; admin unlock starts a separate bridge through sudo.
- Admin bridge activity includes calls and streams in progress; idle expiry must not stop busy work (`route.go`, `bridge.Proc.Hold`).
- Bridge stdout carries protocol only; use stderr for diagnostics.
- Streams apply backpressure: `Stream.Send` blocks at the flow-control window; honor context cancellation and input closure.
- Module commands use argv and `sys.Run` / `Output` / `Stream`, with fixed PATH; do not introduce shell interpretation.
- Register access levels explicitly; return `rpc.Errorf` codes so the browser can unlock and retry `needs_admin`.
- Preserve POST CSRF checks and session authentication; webhook token authentication is a deliberate separate route.
- Plugin commands and HTTP access are constrained by manifest declarations; environment secrets stay in the daemon.

## Gotchas
- Architecture and server README describe Linux first. Windows has platform-specific authentication, bridges and modules; `internal/pam/pam_windows.go` uses LogonUser, not libpam.
- `--dev` binds loopback by default, permits only the daemon user and proxies Vite unless `--web` is supplied; plugin paths depend on the working directory.
- `--dev-insecure-noauth` requires dev, refuses root and only listens on loopback; it does not grant arbitrary bridge admin rights.
- Bridge `--config` and dev plugin paths must be absolute. User bridge restarts revalidate the account.
- Go module is rooted in `server/`, not repository root; `server/go.mod` requires Go 1.24.

## Commands and references
- Repository root: `make build-server`; `make test-server` runs `go vet ./...` then `go test ./...` inside `server/`.
- Repository root: `make build-windows` builds pure-Go Windows daemon/bridge for amd64 and arm64; its loop requires a POSIX shell.
- From `server/`: `go vet ./...`, `go test ./...`; Linux cgo builds require gcc and PAM headers.
- Dev: `make dev` plus `make dev-web`; `tools/devclient` exercises RPC and streams. These are documented commands, not verification performed for this map.
- Read `server/README.md` for module implementation, dev flags and security limits.
- Read `docs/ARCHITECTURE.md` sections Processes, Wire protocols, Bridge module API, Server configuration, Plugins and Releases.
- Detailed contracts: `docs/api/auth.md`, `config.md`, `jobs.md`, `notify.md`, `environments.md`, `plugins.md`, `updates.md`; deployment: `docs/RELEASING.md`, `docs/PACKAGING.md`.
