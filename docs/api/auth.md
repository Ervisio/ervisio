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
(uploads: any content type). A present `Origin` header must match the host (in `--dev` any
loopback origin is accepted). Missing header → 403 `forbidden`.
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
| 401 | `{"error":{"code":"unauthenticated","message":"wrong user name or password"}}` |
| 403 | `code:"forbidden"`, `data.reason:"root_disabled"` — root sign-in disabled (`allow_root=false`) |
| 403 | `code:"forbidden"`, `data.reason:"account"` — PAM account check refused (expired/locked) |
| 403 | `code:"forbidden"`, `data.reason:"dev_mode_user"` — `--dev` only allows the daemon's user |
| 429 | `code:"forbidden"`, `data:{"reason":"rate_limited","retryAfter":<seconds>}` + `Retry-After` header |
| 503 | `code:"unavailable"` — the bridge could not be started |

Rate limit: `login.max_failures` failed attempts per client IP within 15 minutes (wrong unlock
passwords count too).

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
- Up to 64 open channels per connection; frames ≤ 2 MiB; `ch` must be a positive integer.
- Errors not tied to a channel (bad JSON) are sent with `"ch":0`.
- `{"op":"close"}` from the client is answered with `{"op":"end"}`.
- `input` frames: `data` is forwarded verbatim to the bridge stream (the `b64` flag is not
  interpreted on input). If a channel's input queue (256 frames) overflows it ends with
  `unavailable`.
- The socket is closed with status 1008 when the session ends (logout, expiry).
- In production the `Origin` must match the host; in `--dev` loopback origins are accepted.
