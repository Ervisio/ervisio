# Auth & session API (linuxadmind)

All responses are JSON. Errors always look like
`{"error":{"code":"…","message":"…","data":{…}?}}` with an HTTP status derived from the code:

| code | status | | code | status |
|---|---|---|---|---|
| `unauthenticated` | 401 | | `invalid` | 400 |
| `needs_admin` | 403 | | `conflict` | 409 |
| `forbidden` | 403 (429 when rate limited) | | `unavailable` | 503 |
| `not_found` | 404 | | `internal` | 500 |

**Every POST** must send `X-Requested-With: linuxadmin` and `Content-Type: application/json`
(uploads: any content type). A present `Origin` header must match the host (in `--dev` the
Vite dev server given with `--vite` is accepted too; other local ports are not). Missing header → 403 `forbidden`.
In `--dev` every request whose `Host` is not `localhost`, `127.x.x.x` or `[::1]` (any port) gets 421
(DNS-rebinding protection).
On session routes the session is checked first, so a signed-out POST gets 401.

The session cookie is `la_session` (HttpOnly, SameSite=Strict, Secure except in `--dev`).
Timestamps are **unix milliseconds**.

## `GET /api/public/host` (no auth)

```json
{"hostname":"arch","ip":"192.168.1.12",
 "distro":{"id":"arch","name":"Arch Linux","color":"#1793D1","logo":"archlinux-logo","logoUrl":"/api/public/logo"}}
```
`ip` is omitted when `login.show_ip = false`. `distro.name` is os-release `PRETTY_NAME`.
`logo` is the os-release `LOGO` icon name; `logoUrl` is present only when the icon file was found.

## `GET /api/public/logo` (no auth)

The distro logo (SVG or PNG from `/usr/share/pixmaps` or the hicolor theme), or 404.

## `POST /api/auth/login`

Request: `{"user":"alice","password":"…","remember":true}` (`remember` optional: when true the
cookie gets `Max-Age = session.timeout`, otherwise it is a browser-session cookie).

200 → the same object as `GET /api/auth/session` (below).

Failures:
| status | body |
|---|---|
| 401 | `{"error":{"code":"unauthenticated","message":"wrong user name or password"}}` — also when the password is right but the account may not sign in: PAM account check refused (locked, expired, password change required), login shell `nologin`/`false`/restricted or not in `/etc/shells`, or a uid-0 alias with `allow_root = false`. The precise reason is only in the server log, so the answer is no password oracle. |
| 403 | `code:"forbidden"`, `data.reason:"root_disabled"` — the user name is `root` and `allow_root=false` (checked before any password check) |
| 403 | `code:"forbidden"`, `data.reason:"dev_mode_user"` — `--dev` only allows the daemon's user |
| 429 | `code:"forbidden"`, `data:{"reason":"rate_limited","retryAfter":<seconds>}` + `Retry-After` header |
| 429 | `code:"forbidden"`, `data:{"reason":"busy","retryAfter":1}` — two attempts from this client are already being checked |
| 503 | `code:"unavailable"`, `data.reason:"busy"` — every PAM slot (8) stayed busy for 5 s |
| 503 | `code:"unavailable"` — the bridge could not be started |

Rate limit: `login.max_failures` failed attempts per client within 15 minutes (wrong unlock
passwords count too). The client key is the IPv4 address or the IPv6 /64 prefix. An attempt is
counted **before** PAM runs and taken back only when it succeeds, so parallel attempts cannot
exceed the limit; at most 2 attempts per client run at once, and 8 in total. With `pam_faillock`
in the stack, failures also count toward faillock's per-account lock (`deny=`): a client that
knows a user name can lock that account for faillock's `unlock_time`, within the limits above.

Sessions: each session has an idle timeout (`session.timeout`) and an absolute lifetime of 24 h
(or `session.timeout` when longer and "stay signed in" was chosen). Outside `--dev`, the user
bridge runs inside a PAM session (`pam_setcred` + `pam_open_session` of the `linuxadmin`
service, falling back to `login`), opened by a root helper (`linuxadmind --pam-session-helper`)
and closed when the bridge exits, so `pam_limits`, `pam_loginuid` and `pam_systemd` apply. In
`--dev` (no root) no PAM session is opened. Every 60 s, on unlock and before a bridge restart the
daemon checks the account again and ends the session (bridges stopped, WebSockets closed with
1008) when the account was removed, its uid changed, its shell became `nologin`/not allowed, it
left a group it had at sign-in, its password was locked or changed or the account expired
(read from `/etc/shadow` when running as root), or PAM account management refuses it (used when
the shadow file is not readable or has no entry, and always on unlock and restart). Groups added
after sign-in apply at the next sign-in.

### Dev without password (`--dev --dev-insecure-noauth`)

Only with `--dev`, on a loopback listen address, and never as root. At start the daemon prints a
one-time URL `http://127.0.0.1:<port>/api/dev/noauth?token=…`: opening it signs that browser in
as the daemon's user (session cookie) and redirects to `/`. Each token works once; the daemon then
prints the next one. `server/tools/devclient login <url>` prints the session token for scripts.

## `POST /api/auth/logout`

Always 200 `{}`; clears the cookie and stops the session's bridges.

## `GET /api/auth/session`

200:
```json
{"user":"alice","name":"Alice Doe","uid":1000,"home":"/home/alice",
 "groups":["alice","wheel"],"isRoot":false,"isAdmin":false,"canSudo":true,
 "unlockedUntil":1790879000000}
```
- `isAdmin`: admin rights are usable right now (root, or unlocked and not expired).
- `canSudo`: hint only — root or member of `wheel`/`sudo`/`admin`. sudo decides on unlock.
- `unlockedUntil`: present only while unlocked; it slides forward with every admin call
  (idle timeout `session.admin_unlock`).
- `name` is the GECOS full name (may be empty).

401 `unauthenticated` when signed out or idle longer than `session.timeout`.

## `POST /api/auth/unlock`

Request `{"password":"…"}` → 200 `{"unlockedUntil":1790879000000}`.
For root sessions it returns 200 `{}` (root needs no unlock).

Failures: 400 `invalid` (wrong password), 403 `forbidden` (user may not use sudo, or sudo did not
give root), 409 `conflict` (an unlock is already running), 429 (rate limited), 503 `unavailable`
(sudo missing / timed out). A wrong password is **not** a 401: the session is still valid.

## `POST /api/auth/lock`

200 `{}`; stops the root bridge.

## `POST /api/rpc`

Request `{"method":"services.list","params":{…},"admin":false}` (body ≤ 1 MiB) →
200 `{"result":…}` or an error as above. Routing: root bridge when `admin:true` or the method is
admin-level; if not unlocked → 403 `needs_admin` with `data.method`. Unknown method → 404
`not_found`. Calling a stream method here → 400 `invalid`.

## `GET /api/ws`

WebSocket, frames as in ARCHITECTURE.md. Notes:
- Limits: frames ≤ 512 KiB (a bigger frame closes the socket with 1009); up to 64 open channels
  per connection and 128 per session; up to 8 WebSockets per session (more → 503 before the
  upgrade); `ch` must be a positive integer.
- Errors not tied to a channel (bad JSON) are sent with `"ch":0`.
- `{"op":"close"}` from the client is answered with `{"op":"end"}`.
- `input` frames: `data` is forwarded verbatim to the bridge stream (the `b64` flag is not
  interpreted on input). If a channel's input queue (64 frames) overflows, or the session has
  more than 8 MiB of input queued, the channel ends with `unavailable`.
- The socket is closed with status 1008 when the session ends (logout, expiry, account check).
- The `Origin` must match the host; in `--dev` the Vite dev server (`--vite`) is accepted too.
