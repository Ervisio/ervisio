# terminal.* (persistent terminal sessions)

Package `server/internal/modules/terminal`. Sessions are real ptys owned by the **bridge process**
that created them, so they keep running while the browser is closed ("detached") and are
re-attached later with the last 256 KB of output replayed.

## Where sessions live, and for how long

| Created with | Lives in | Dies when |
|---|---|---|
| `terminal.create` | the user bridge (the user's uid) | the bridge stops: the login session ends (sign out, or idle longer than `session.timeout`, default 12 h) or the daemon restarts |
| `terminal.create` with `admin:true` | the root bridge, kind `root` | the root bridge stops: admin rights lock, **5 minutes without admin calls** (`session.admin_unlock`), sign out |

So "detached" survives a closed tab or a lost connection, not a daemon restart or the end of
the login session. A root session also ends when the root bridge idles out, so a detached root
shell does not outlive the unlock window unless the root bridge keeps getting admin calls (the web
page lists root sessions with `terminal.list` + `admin:true` while unlocked, which keeps it alive
as long as the Terminal page is open). Each bridge holds at most 16 sessions.

Every method is level `user`. The web client passes `admin:true` for root sessions (create, list,
attach, rename, kill); it must list both bridges and merge, because each bridge only knows its own
sessions. Session ids are random (12 hex chars) and unique across bridges.

## Methods

### `terminal.create`
Params (all optional):
```json
{"kind":"local|root|ssh","shell":"/bin/zsh","cwd":"/srv","cols":100,"rows":30,"name":"build",
 "host":"web-01.example.com","user":"deploy","port":2222}
```
- `kind` `local` (default): login shell from `/etc/passwd` for the bridge's user (fallback `/bin/sh`;
  started as a login shell, `argv[0]` = `-bash`), in `cwd` (absolute, existing; default home).
  `shell` must be an executable listed in `/etc/shells`.
- `admin:true` on the call runs it in the root bridge and the session `kind` is `root`
  (root's login shell). `kind:"root"` without `admin:true` fails with `needs_admin`.
- `kind` `ssh`: runs `ssh -o ServerAliveInterval=30 [-p port] [-l user] -- host` in the pty (argv only).
  `host` matches `^[A-Za-z0-9]([A-Za-z0-9.:_-]{0,251}[A-Za-z0-9])?$` (so it can never start with `-`),
  `user` matches `^[A-Za-z_][A-Za-z0-9_.-]{0,31}$?`, `port` 1..65535.
- `cols`/`rows` 1..1000 (default 80x24). `name` up to 64 characters; default is `user@host` for
  shells (`user@host 2`, `3`... when taken) and the host for ssh.
- Environment: `TERM=xterm-256color`, `COLORTERM=truecolor`, `LANG` from the bridge environment or
  `/etc/locale.conf` (fallback `C.UTF-8`), `HOME`, `USER`, `LOGNAME`, `SHELL`, `PATH`, `XDG_RUNTIME_DIR`
  when `/run/user/<uid>` exists.

Result: `{"id":"28ef82cc7e64"}`.
Errors: `invalid` (bad host, user, port, size, shell, name, kind), `not_found` (cwd missing),
`needs_admin` (`kind:"root"` without admin), `conflict` (16 sessions open), `unavailable` (ssh not
installed, or the process could not start).

### `terminal.list`
Params: none. Result:
```json
{"sessions":[{"id":"28ef82cc7e64","name":"fonlogen@arch","cwd":"/home/fonlogen","createdAt":1790879531549,
              "attached":false,"kind":"local","title":"bash, /home/fonlogen"}]}
```
`createdAt` is unix ms. `cwd` is the shell's current directory (read from `/proc`, empty for ssh).
`attached` is true while at least one client is attached. `title` is the window title the program set
(OSC 0/2), else `<shell>, <cwd>`. Oldest session first. Sessions whose process ended are removed.

### `terminal.rename`
`{"id":"…","name":"build"}` → `{}`. Errors: `invalid` (empty or too long name), `not_found`.

### `terminal.kill`
`{"id":"…"}` → `{}`. Sends SIGHUP to the process group, SIGKILL after 2 s. Errors: `not_found`.

### `terminal.attach` (stream)
Open params: `{"id":"…","cols":100,"rows":30}` (the pty is resized to cols x rows first).
Several clients may attach to one session; all see the output and all may type.

- Server to client: binary frames (`b64:true`, arrive as bytes in the web client): first the
  scrollback replay (up to 256 KB), then live output. When the process ends, one JSON frame
  `{"type":"exit","code":0}` and the stream ends normally.
- Client to server (stream input):
  - `{"type":"input","data":"<base64 bytes>"}`: keystrokes or pasted text.
  - `{"type":"resize","cols":120,"rows":40}`: 1..1000.
- Closing the stream detaches; the session keeps running.
- Errors: `not_found` (session gone, e.g. the shell exited or the bridge restarted),
  `unavailable` (the client fell behind; re-attach to get a fresh replay).

To resume after a dropped websocket the client opens `terminal.attach` again with the same id and
clears its screen before the replay arrives (the web client does this with exponential backoff,
0.5 s up to 8 s).

## Preferences used by the web page (client side, `prefs.*`)

| Key | Value |
|---|---|
| `terminal` | the object Settings writes: `{theme: "app"\|"dark"\|"light", fontSize, copyOnSelect, snippets}` (`snippets` = show the chips) |
| `terminal.showSnippets` | boolean mirror of `terminal.snippets`, written together with it |
| `terminal.hosts` | `[{id, name, host, user?, port?}]` saved SSH hosts |
| `terminal.snippets` | `[{id, title, command}]`; when unset the page shows six built-in examples |
| `terminal.history` | `[{cmd, at}]` (unix ms), commands run from chips or the palette, newest first, max 50 |
