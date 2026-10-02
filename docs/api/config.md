# config.* (server configuration)

Package `server/internal/modules/config`. Reads/writes the daemon config file
(`/etc/ervisio/ervisio.conf`, or `--config`). ervisiod reloads the file within ~2 s
after a change; keys marked `restart` need a daemon restart.

## Keys

| key | type | default | notes |
|---|---|---|---|
| `listen` | string `host:port` | `0.0.0.0:9090` | restart |
| `allow_root` | bool | `false` | |
| `login.show_ip` | bool | `true` | |
| `login.max_failures` | int 1–1000 | `5` | per IP per 15 min (password and SSH-key failures together) |
| `auth.ssh_keys` | bool | `true` | sign in with an SSH key listed in the user's `authorized_keys` (`docs/api/auth.md`) |
| `auth.allow_users` | list of user names | `[]` | sign-in allowlist, see below |
| `auth.allow_groups` | list of group names | `[]` | members (primary or supplementary group) may sign in |
| `auth.admins_only` | bool | `false` | administrators (members of `sudo`, `wheel` or `admin`; root when `allow_root`) may sign in |
| `session.timeout` | duration 5m–720h | `"12h"` | idle timeout |
| `session.admin_unlock` | duration 30s–24h, or `"0s"` | `"5m"` | admin idle timeout; `"0s"` keeps admin rights until sign-out |
| `tls.mode` | enum `self-signed`/`letsencrypt`/`custom`/`http` | `self-signed` | restart; letsencrypt not implemented yet (falls back to self-signed); `http` serves plain HTTP behind a reverse proxy, see below |
| `tls.redirect` | bool | `true` | plain HTTP on the same port → redirect to HTTPS; restart; unused with `http` |
| `tls.cert`, `tls.key` | absolute path | `""` | for `custom`; restart |
| `plugins.allow_unsigned` | bool | `false` | unsigned plugins are blocked (dev folders still run in developer mode, marked "Unsigned, dev") |
| `plugins.dev` | bool | `false` | |
| `plugins.catalog_url` | string, `""` or an `https://` URL | `https://ervisio.github.io/plugins/catalog.json` | the marketplace catalog of Plugins › Browse; its signature is at the same address with `.sig` instead of `.json`, and an unsigned catalog is ignored (`docs/api/plugins.md`); `""` = no remote catalog |
| `plugins.catalog_key` | string, `""` or a base64 ed25519 public key | `""` | an extra key trusted for catalog signatures (a private catalog); plugins still need the team signature |
| `audit.enabled` | bool | `true` | record the activity log (sign-ins, administrator unlocks, plugin installs, settings, and every mutating plugin call); see `docs/api/plugins.md`, "Activity log" |
| `audit.retention_days` | integer 0–3650 | `90` | daily log files older than this are deleted; `0` keeps them forever |
| `updates.channel` | enum `stable`/`prerelease` | `stable` | which GitHub releases are offered (`docs/RELEASING.md`) |
| `updates.auto_check` | bool | `true` | the daemon checks every 6 h; admins get a bell notice |
| `updates.auto_install` | bool | `false` | the daemon installs a newer version every day at `auto_install_at` |
| `updates.auto_install_at` | string `"HH:MM"` (00:00–23:59, 24-hour) | `"03:30"` | local time of the automatic install |

Durations are Go duration strings (`"90s"`, `"5m"`, `"12h"`). `type` `list` is a list of strings.

### Who may sign in (`auth.allow_*`, `auth.admins_only`)

All three empty/false (the default) lets every local account with a valid login in (valid password or SSH key,
allowed shell, `allow_root` for uid 0). When any is set, an account may sign in only if it is **listed in
`allow_users`**, **or** is a member of **any group in `allow_groups`** (primary or supplementary group, resolved
from NSS at each sign-in), **or** `admins_only` is on and it is an administrator (`sudo`, `wheel`, `admin`
group; root counts only when `allow_root` is on). The rule is checked for password and SSH-key sign-in and again
for every live session each 60 s (`server.revalidate`), so removing a user from the list, or from a listed group,
ends their sessions within about a minute. Names must match `[A-Za-z0-9_][A-Za-z0-9_.@-]{0,63}$?`, at most 256
entries, no duplicates; anything else is `invalid`. Unknown users or groups are accepted (they may exist later or
come from LDAP).

Refusals: password sign-in gives the generic `unauthenticated` "wrong user name or password" (no password oracle;
the reason is in the log); SSH-key sign-in, after the signature verified and the key is listed in
`authorized_keys`, gives `403 forbidden`, `data.reason:"not_allowed"`, "This account may not sign in to
Ervisio". Mind that you can lock yourself out: keep your own account allowed.

### Plain HTTP behind a reverse proxy (`tls.mode = "http"`)

The daemon serves plain HTTP, with no TLS and no redirect listener, for a reverse proxy on the same machine that
provides HTTPS. It is **only accepted when `listen` is a loopback IP address** (`127.0.0.0/8` or `[::1]`; an empty
host, `0.0.0.0` and other addresses are rejected): `config.set` answers `invalid`, `ervisiod` and
`--check-config` refuse the file, and `--listen` on the command line is checked the same way. (Unix sockets are not
supported as listen addresses.) Set `listen` first, then `tls.mode`.

Session cookies (`SameSite=Strict`, `HttpOnly`) keep the `Secure` flag in every mode except `http`, and always
on a request that arrived over TLS. In `http` mode the flag fails closed: when a trusted proxy
(`web.trusted_proxies`, loopback by default) sends `X-Forwarded-Proto`, `Secure` is set unless it says `http`;
without that header (or from a peer that is not a trusted proxy, whose header is ignored) `Secure` is set unless
the browser addressed a loopback name (`http://127.0.0.1:PORT`, `http://localhost:PORT`, an SSH tunnel), so the
console also works there. A TLS proxy that forgets `X-Forwarded-Proto` therefore still gets `Secure` cookies.
The proxy should also send `X-Forwarded-For`: without it every browser looks like the proxy's loopback address to
the rate limiter and to PAM (`rhost`), and `from=` options in `authorized_keys` refuse SSH-key sign-in.
Origin checks compare host names only, not schemes, so a browser at
`https://admin.example.com` is accepted when the proxy forwards `Host: admin.example.com` or sends
`X-Forwarded-Host` (or the origin is in `web.allowed_origins`). The proxy must also pass WebSocket upgrades. Caddy:

```
admin.example.com {
    reverse_proxy 127.0.0.1:9090
}
```

nginx needs `proxy_set_header Host $host; proxy_set_header X-Forwarded-Proto $scheme; proxy_set_header X-Forwarded-For $remote_addr;`
and the `Upgrade`/`Connection` headers for `/api/ws`. A proxy in a Docker container cannot reach the host's
loopback: keep HTTPS there (`reverse_proxy https://HOST:PORT` with `tls_insecure_skip_verify`) or use host networking.

### Checking a file: `ervisiod --check-config [path]`

Parses and validates the configuration (default `--config`, `/etc/ervisio/ervisio.conf`) without starting
anything or needing root beyond reading the file. Prints `OK: <path>` (exit 0), or the errors on stderr (exit 1);
unknown keys are printed as warnings. Besides the key rules it checks that `tls.mode = "custom"` has `tls.cert` and
`tls.key` and that the files exist, and that a missing file is an error. `install.sh` runs it before restarting the
service and puts the previous file back when it fails.

## `config.get` (user) → State

```json
{"path":"/etc/ervisio/ervisio.conf","exists":false,
 "values":{"listen":"0.0.0.0:9090","allow_root":false,"login.show_ip":true,"login.max_failures":5,"auth.ssh_keys":true,"auth.allow_users":[],"auth.allow_groups":[],"auth.admins_only":false,
           "session.timeout":"12h","session.admin_unlock":"5m","tls.mode":"self-signed","tls.redirect":true,
           "tls.cert":"","tls.key":"","plugins.allow_unsigned":false,"plugins.dev":false,
           "plugins.catalog_url":"https://ervisio.github.io/plugins/catalog.json","plugins.catalog_key":"",
           "audit.enabled":true,"audit.retention_days":90,
           "updates.channel":"stable","updates.auto_check":true,"updates.auto_install":false,"updates.auto_install_at":"03:30"},
 "defaults":{…same shape…},
 "keys":[{"key":"listen","type":"string","restart":true},{"key":"tls.mode","type":"enum","values":["self-signed","letsencrypt","custom"],"restart":true},…],
 "warnings":[]}
```
`type` ∈ `string|bool|int|duration|enum|path|list`. Secret keys (none yet) are never returned.
`warnings` lists unknown keys found in the file. An unparsable file → `conflict`.

## `config.set` (admin)

Params `{"key":"login.show_ip","value":false}` → the new State. Validates key and type
(`invalid` otherwise), checks the whole resulting configuration (`invalid` for rules that span keys, such as plain HTTP needing a loopback `listen`), copies the old file to `<file>.bak`, writes atomically (0644).
Comments in the file are not preserved (the backup keeps them).

## `config.rawDiff` (user)

Params `{"changes":{"login.show_ip":false,"session.timeout":"1h"}}` →
`{"current":"<toml>","proposed":"<toml>","diff":"<lines prefixed with ' ', '-', '+'>"}`.
Nothing is written. Invalid changes → `invalid`.

## [web]

| key | type | default | meaning |
|---|---|---|---|
| `web.allowed_origins` | list of origins | `[]` | extra browser origins (`https://host[:port]`) accepted for API calls and WebSockets; file only, not editable from Settings |
| `web.trusted_proxies` | list of IPs/CIDRs | `["127.0.0.0/8","::1/128"]` | peers whose `X-Forwarded-Host`, `X-Forwarded-Proto` and `X-Forwarded-For` are believed (origin check, client address for rate limits and logs) |
