# Terminal
Purpose: persistent browser terminals (local, root and SSH shells) backed by bridge-owned PTYs streamed to xterm.js.

## Entry points and public contracts
- Backend `server/internal/modules/terminal/terminal.go:Register`. User level: `terminal.create`, `terminal.list`, `terminal.rename`, `terminal.kill`; stream `terminal.attach`. No Admin-level method.
- Root shell: same methods called with `admin:true` (root bridge, own `mgr`); `create` kind `root` without admin returns `needs_admin`.
- `terminal.create` params: kind `local|root|ssh`, shell, cwd, cols, rows, name, host, user, port. Returns `{id}`.
- `attach` params `{id, cols, rows}`; input frames `{type:"input",data:b64}` / `{type:"resize",cols,rows}`; output is binary bytes; end is `{type:"exit",code}`.
- `run.go:RunPTY`: sessionless PTY for one stream, used by `plugins.pty`; same frames, kills the process group on close.
- Frontend `web/src/sections/terminal/`: `index.tsx` (tabs, layout, create/rename/kill, prefs), `TerminalView.tsx` (xterm + FitAddon + attach/reconnect), `Sidebar`, `Palette`, `Snippets`, `Dialogs`, `ExtraKeys` (mobile keys), `xtermTheme.ts`.
- Prefs: `terminal` (theme, fontSize, copyOnSelect, snippets), `terminal.hosts`, `terminal.history`, `terminal.snippets`; layout in `localStorage`.

## Flow
- `create` -> `buildSpec` -> `mgr.Create` -> `startPTY` -> `Session.pump` goroutine copies PTY output into a ring and to every subscriber.
- `attach` -> resize -> `Subscribe` returns replay + live channel atomically; replay is sent first, then live bytes.
- Session end: `pump` waits process, stores exit code, removes it from the manager, closes `done`; attach drains the channel, sends `exit`.
- UI lists sessions of both bridges (root list only when unlocked, `adminOk`) and tags root ones `admin:true`.
- `TerminalView` reconnects with backoff; `term.reset()` before each re-attach because the full scrollback is replayed.

## Invariants
- Sessions belong to the bridge process: they survive browser close but die with the bridge. Root sessions are held alive only by open streams (server.md hold rule); a detached root session may be lost on admin idle expiry (inferred, not tested).
- Max 16 sessions per bridge (`MaxSessions`); scrollback 256 KiB (`ScrollbackSize`); size 1..1000 cols/rows.
- Subscriber buffer 512 chunks; a slow client is dropped with `unavailable` and the UI re-attaches.
- Names <= 64 chars, no control chars; duplicates get "name 2" (`uniqueName`).
- SSH is run as the `ssh` client with argv (`--` before host), validated `hostRe` / `userRe` / port; `ServerAliveInterval=30`.

## Gotchas
- Unix: shell must be listed in `/etc/shells` and executable (`resolveShell`), default is the passwd login shell else `/bin/sh`; argv0 gets a leading `-` (login shell); env is rebuilt (`sys.SafePath`, TERM, LANG, XDG_RUNTIME_DIR), not inherited.
- Windows (`pty_windows.go`, `plat_windows.go`): ConPTY; shell must be an existing `.exe` (default pwsh, powershell, COMSPEC); only a whitelist of env vars is passed; no process groups, `hangupProc` closes the pseudo console and ignores `self`; ssh is the Windows OpenSSH client.
- Unix kill: SIGHUP to the process group, SIGKILL after grace (`Session.Kill` 2s, `RunPTY` 1s). PTY EOF on Linux is `EIO` (`isPTYClosed`).
- Cut replay: the ring can start mid escape sequence, hence the reset before replay.
- Reattaching an ended session returns `not_found`; UI shows "gone".
- `RunPTY` cmd must not set `SysProcAttr` (pty makes it a session leader).
- `Manager.Create` checks the session count and inserts under separate lock holds (small race beyond 16).
- Session title comes from OSC sequences parsed in `oscState.feed` (`sanitizeTitle`).

## Checks and references
- `cd server && go test ./internal/modules/terminal/` (`terminal_test.go`, unix-only `terminal_unix_test.go`).
- `cd web && npm run typecheck && npm run i18n:check`.
- Docs: `docs/api/terminal.md`.

Related: map/server.md, map/web.md
