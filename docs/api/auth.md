# Auth & session API (ervisiod)

All responses are JSON. Errors always look like
`{"error":{"code":"…","message":"…","data":{…}?}}` with an HTTP status derived from the code:

| code | status | | code | status |
|---|---|---|---|---|
| `unauthenticated` | 401 | | `invalid` | 400 |
| `needs_admin` | 403 | | `conflict` | 409 |
| `forbidden` | 403 (429 when rate limited) | | `unavailable` | 503 |
| `not_found` | 404 | | `internal` | 500 |

**Every POST** must send `X-Requested-With: ervisio` and `Content-Type: application/json`
(uploads: any content type). A present `Origin` header must match the host (in `--dev` the
Vite dev server given with `--vite` is accepted too; other local ports are not). Missing header → 403 `forbidden`.
In `--dev` every request whose `Host` is not `localhost`, `127.x.x.x` or `[::1]` (any port) gets 421
(DNS-rebinding protection).
On session routes the session is checked first, so a signed-out POST gets 401.

The session cookie is `ervisio_session` (HttpOnly, SameSite=Strict, Secure except in `--dev` and, in `tls.mode = "http"`, for plain-HTTP requests as described in `docs/api/config.md`).
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

Sign-in allowlist (`auth.allow_users`, `auth.allow_groups`, `auth.admins_only`, `docs/api/config.md`): a password
sign-in refused by it gets the same generic 401 as above (after PAM accepted the password; reason in the log only).
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
bridge runs inside a PAM session (`pam_setcred` + `pam_open_session` of the `ervisio`
service, falling back to `login`), opened by a root helper (`ervisiod --pam-session-helper`)
and closed when the bridge exits, so `pam_limits`, `pam_loginuid` and `pam_systemd` apply. In
`--dev` (no root) no PAM session is opened. Every 60 s, on unlock and before a bridge restart the
daemon checks the account again and ends the session (bridges stopped, WebSockets closed with
1008) when the account was removed, its uid changed, its shell became `nologin`/not allowed, it
left a group it had at sign-in, its password was locked or changed or the account expired
(read from `/etc/shadow` when running as root), or PAM account management refuses it (used when
the shadow file is not readable or has no entry, and always on unlock and restart). Groups added
after sign-in apply at the next sign-in.

## Sign in with an SSH key

Enabled by `auth.ssh_keys` (default `true`). The browser loads the private key, decrypts it with the
passphrase and signs a one-time challenge; **the private key and the passphrase never leave the
browser**. Only the public key and the signature are sent. Client: `web/src/auth/sshkey`
(contract below).

### `POST /api/auth/challenge`

Request `{"user":"alice","host":"server.example:9090"}` (`host` = `location.host`; optional, the
request's `Host` is used without it). 200:
```json
{"nonce":"<43 chars base64url, 32 random bytes>",
 "challenge":"ervisio-ssh-auth-v1\nserver.example:9090\nalice\n<nonce>",
 "host":"server.example:9090","expires":1790879000000}
```
The nonce is valid for 60 s, **once**, for this user name and this client address (the address the
rate limiter sees, after `web.trusted_proxies`). The answer is the same whether the user exists or
not. At most 8 outstanding challenges per client key (older ones are dropped) and 4096 in total.

| status | body |
|---|---|
| 400 | `invalid`, `data.reason:"host_not_allowed"`: the server does not answer to `host` (same rules as the `Origin` check, see `web.allowed_origins`) |
| 400 | `invalid`: user name not valid |
| 403 | `forbidden`, `data.reason:"ssh_keys_disabled"` / `"root_disabled"` |
| 429 | `rate_limited` as for `/api/auth/login` (the client is locked out) |
| 503 | `unavailable`, `data.reason:"busy"`: 4096 challenges outstanding |

### Signed message

```
"ervisio-ssh-auth-v1" LF host LF user LF nonce        (UTF-8, no trailing newline)
```
The client builds this text itself and refuses to sign when the server's `challenge` differs.
Signature formats: `ssh-ed25519`; `ecdsa-sha2-nistp256/384/521` (SHA-256/384/512); for RSA keys
`rsa-sha2-256` or `rsa-sha2-512` (the client uses 512). `ssh-rsa` (SHA-1) signatures, DSA, FIDO
(`sk-*`) keys, certificates and RSA keys under 2048 bits are refused.

### `POST /api/auth/login-key`

Request:
```json
{"user":"alice","publicKey":"ssh-ed25519 AAAA… comment","signature":"<base64 SSH signature blob>",
 "nonce":"<from the challenge>","remember":true}
```
`signature` is the SSH wire blob `string format, string signature` (as in an SSH user-auth
request), base64. 200 → the session object (with `"authMethod":"ssh-key"` and `keyFingerprint`).

The server, in order: takes the nonce (unknown/used/expired/other user/other address →
`challenge_invalid`; the nonce is burnt either way), parses the public key, verifies the signature
over the message built with the host stored in the challenge, resolves the account, applies the
same refusals as the password login (uid 0 with `allow_root = false`, login shell not allowed,
`--dev` user), checks the key against the user's `authorized_keys`, runs **PAM account management**
(`pam_acct_mgmt`: expired/locked accounts, `pam_access`…) **without `pam_authenticate`**, then
opens the session exactly like a password sign-in (user bridge inside a PAM session).

`authorized_keys`: the files of sshd's global `AuthorizedKeysFile` in `/etc/ssh/sshd_config`
(`Include` followed; directives inside `Match` blocks are **not** applied; `%h %u %U %%`
expanded; `none` = no key works); default `.ssh/authorized_keys .ssh/authorized_keys2`. They are read
with the user's file-system identity (`setfsuid`), symlinks resolved, and must pass sshd's
`StrictModes` checks: regular file owned by the user or root and not group/world writable, every
directory up to the home directory owned by the user or root and not group/world writable.
`AuthorizedKeysCommand` is not used. Line options:

| option | effect |
|---|---|
| `from="…"` | the client address must match (CIDR, `*`/`?` wildcards, `!` negation); host names never match (no DNS) |
| `expiry-time="YYYYMMDD[HHMM[SS]][Z]"` | refused after that time |
| `command="…"` | **refused** (the key is meant for a forced command) |
| `cert-authority`, `principals=` | line skipped (certificates are not supported) |
| `restrict`, `no-pty`, `no-*-forwarding`, `no-user-rc`, `environment=`, `permitopen=`, `permitlisten=`, `tunnel=`… | ignored |
| anything else | line skipped (sshd also rejects lines with unknown options) |

As in sshd, a line that lists the key but whose options refuse the sign-in is skipped and the next
line listing the same key is tried.

Failures:
| status | body |
|---|---|
| 401 | `unauthenticated`, `data.reason:"key_refused"`: bad signature, key not listed or refused by its options, unknown user, PAM account check refused, shell not allowed, uid-0 alias. One answer for all, sent no sooner than 1 s after the request arrived, so neither the text nor the timing tells whether the user exists. The reason is in the server log. |
| 401 | `unauthenticated`, `data.reason:"challenge_invalid"` |
| 400 | `invalid`, `data.reason:"unsupported_key"` |
| 403 | `forbidden`, `data.reason:"ssh_keys_disabled"` / `"root_disabled"` / `"dev_mode_user"` |
| 403 | `forbidden`, `data.reason:"not_allowed"`: "This account may not sign in to Ervisio": the key signature verified and the key is listed, but the sign-in allowlist refuses the account. Only someone holding a key of the account sees it. |
| 429 / 503 | as for `/api/auth/login` (rate limit shared with password attempts, PAM slots) |

Every refused key sign-in counts as a failed attempt in the same per-client limiter as passwords
(`login.max_failures` / 15 min). Logged as `login "alice" from 1.2.3.4 method=ssh-key key=SHA256:…`
(password sign-ins log `method=password`).

Sessions signed in with a key: every 60 s (and on unlock / bridge restart) the daemon also checks
that the key is still authorized (removed from `authorized_keys`, `from=` / `expiry-time=` now
refusing → session ended). Like sshd, a **locked or changed password does not end a key session**
(`usermod -L` locks the password, not the keys); account expiry, PAM account refusal, removed
account, lost groups and a disallowed shell still do.

`--dev` only: `--dev-authorized-keys <file>` replaces the `authorized_keys` lookup so the flow can
be tried without touching `~/.ssh`. The daemon then refuses to start unless it listens on a loopback
address, and the file must be a regular file (not a symlink) owned by the daemon's user and not
writable by group or others (checked at start and at every read).

`from=` is matched against the browser's address: the TCP peer, or behind a trusted proxy the
`X-Forwarded-For` client. When a trusted proxy sends no usable `X-Forwarded-For` the address is
unknown and lines with `from=` refuse the sign-in (they are never matched against the proxy's own
loopback address). Patterns follow sshd: CIDR blocks and `*`/`?` wildcards; a malformed CIDR
refuses the whole line. Note that a local account can reach the daemon over loopback (a trusted
proxy by default) and choose `X-Forwarded-For`, so `from=` does not hold against someone who
already has a shell on the machine and holds the private key.

Live sessions also end when `allow_root` is turned off (uid-0 sessions) and when `auth.ssh_keys` is
turned off (sessions signed in with a key), at the next 60 s check.

### Admin unlock after a key sign-in

The server never learns the password of a key session. `POST /api/auth/unlock` with
`{"password":""}` runs `sudo -n -k` (no prompt): it succeeds when sudoers lets the user run the
root bridge without a password (`NOPASSWD`), otherwise it fails at once with 400 `invalid`,
`data.reason:"password_required"`, without running PAM authentication and without counting a
failed attempt. The UI should then ask for the account password as usual (`sudo -S`). A typical
flow for `authMethod: "ssh-key"` sessions: try `unlock("")` silently, show the password dialog on
`password_required`.

### Web client contract (`web/src/auth/sshkey`)

```ts
import { signInWithKey, needsPassphrase, describeKey, SshKeyError } from './auth/sshkey';
// or useSession().signInWithKey(args), which also updates the session state

needsPassphrase(keyText: string): boolean            // sync, never throws: show the passphrase field
describeKey(keyText, passphrase?) → Promise<{type, bits, comment, publicKey, fingerprint}>
signInWithKey({user, keyText, passphrase?, stay = true}) → Promise<{user, isAdmin, isRoot, authMethod, keyFingerprint}>
```
`keyText` is the pasted text or `await file.text()` of a picked file. Accepted: OpenSSH private keys
(unencrypted, or bcrypt_pbkdf + `aes{128,192,256}-ctr`, `aes{128,192,256}-cbc`,
`aes{128,256}-gcm@openssh.com`), PKCS#8 (`PRIVATE KEY`, and `ENCRYPTED PRIVATE KEY` with
PBES2/PBKDF2 + AES-CBC), PKCS#1 `RSA PRIVATE KEY` and SEC1 `EC PRIVATE KEY` (plain or OpenSSL
`DEK-Info: AES-*-CBC`). Key types ed25519, ECDSA P-256/384/521, RSA ≥ 2048. Needs WebCrypto
(a secure context: https or localhost); ed25519 uses WebCrypto's Ed25519 and falls back to
`@noble/ed25519` when the browser lacks it. Decrypting an OpenSSH key with the default 16 bcrypt
rounds takes about 1 s.

Errors are `SshKeyError` with `code`:

| code | meaning (suggested message) |
|---|---|
| `not_a_key` | not a private key / empty |
| `public_key_given` | a `.pub` / authorized_keys line was given: choose the private key |
| `unsupported_format` | PuTTY `.ppk`, SSH2 private key…: export in OpenSSH format |
| `passphrase_required` | encrypted key, no passphrase |
| `bad_passphrase` | wrong passphrase |
| `unsupported_key_type` | DSA, FIDO `sk-*`, RSA < 2048, unknown curve |
| `unsupported_cipher` | e.g. `chacha20-poly1305@openssh.com`: `ssh-keygen -p -Z aes256-ctr -f key` |
| `corrupt_key` | damaged key data, or private and public halves do not match |
| `crypto_unavailable` | no WebCrypto here (plain http on a non-local address) |
| `key_refused` | not accepted for this user (one code: never says whether the user exists) |
| `challenge_invalid` | took longer than 60 s, or replayed: retry |
| `ssh_keys_disabled`, `root_disabled`, `dev_mode_user` | server policy |
| `host_not_allowed` | the address in the address bar is not one the server answers to (`web.allowed_origins`) |
| `rate_limited`, `busy` | wait `retryAfter` seconds |
| `network` | server unreachable |
| `server_error` | anything else (session could not start, PAM error, unexpected challenge) |

Local errors (all codes up to `crypto_unavailable`) are raised before any request is made.

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
- `unlockedForever`: true while unlocked with `session.admin_unlock = "0s"`; then `unlockedUntil` is the session end and the UI shows no countdown.
  (idle timeout `session.admin_unlock`).
- `name` is the GECOS full name (may be empty).
- `authMethod`: `"password"` or `"ssh-key"`; `keyFingerprint` (`"SHA256:…"`) only for key sessions.

401 `unauthenticated` when signed out or idle longer than `session.timeout`.

## `POST /api/auth/unlock`

Request `{"password":"…"}` → 200 `{"unlockedUntil":1790879000000,"unlockedForever":false}`.
`{"password":""}` tries sudo without a password (`sudo -n`, NOPASSWD rules only): 400 `invalid` with
`data.reason:"password_required"` when sudo needs one, not counted as a failed attempt (see
"Admin unlock after a key sign-in").
For root sessions it returns 200 `{}` (root needs no unlock).

Failures: 400 `invalid` (wrong password), 403 `forbidden` (user may not use sudo, or sudo did not
give root), 409 `conflict` (an unlock is already running), 429 (rate limited), 503 `unavailable`
(sudo missing / timed out). A wrong password is **not** a 401: the session is still valid.

## `POST /api/auth/lock`

200 `{}`; stops the root bridge.

## `POST /api/rpc`

Request `{"method":"services.list","params":{…},"admin":false}` (body ≤ 12 MiB, so a `plugins.http` request body of 8 MiB fits as base64; larger bodies are 400 `invalid` and use `plugins.upload`; a session runs at most 2 calls with a body over 1 MiB, or of unknown length, at once: more wait up to 30 s, then 503 `unavailable`) →
200 `{"result":…}` or an error as above. Routing: root bridge when `admin:true` or the method is
admin-level; if not unlocked → 403 `needs_admin` with `data.method`. Unknown method → 404
`not_found`. Calling a stream method here → 400 `invalid`.

## `GET /api/ws`

WebSocket, frames as in ARCHITECTURE.md. Notes:
- Limits: frames ≤ 512 KiB, except the `open` frame of a stream, which carries the params and may be up to 12 MiB
  (a `plugins.httpStream` request body of up to 8 MiB as base64); a bigger frame closes the socket with 1009. The daemon
  reads only the first 512 KiB of a frame before it knows: `"op":"open"` must come before `params` (the web client
  writes `ch` and `op` first). A frame over 512 KiB takes one of the session's 2 large-body slots (shared with
  `/api/rpc`) while it is opened; up to 64 open channels
  per connection and 128 per session; up to 8 WebSockets per session (more → 503 before the
  upgrade); `ch` must be a positive integer.
- Errors not tied to a channel (bad JSON) are sent with `"ch":0`.
- `{"op":"close"}` from the client is answered with `{"op":"end"}`.
- `input` frames: `data` is forwarded verbatim to the bridge stream (the `b64` flag is not
  interpreted on input). If a channel's input queue (64 frames) overflows, or the session has
  more than 8 MiB of input queued, the channel ends with `unavailable`.
- The socket is closed with status 1008 when the session ends (logout, expiry, account check).
- The `Origin` must match the host; in `--dev` the Vite dev server (`--vite`) is accepted too.
