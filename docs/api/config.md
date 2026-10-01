# config.* (server configuration)

Package `server/internal/modules/config`. Reads/writes the daemon config file
(`/etc/linuxadmin/linuxadmin.conf`, or `--config`). linuxadmind reloads the file within ~2 s
after a change; keys marked `restart` need a daemon restart.

## Keys

| key | type | default | notes |
|---|---|---|---|
| `listen` | string `host:port` | `0.0.0.0:9090` | restart |
| `allow_root` | bool | `false` | |
| `login.show_ip` | bool | `true` | |
| `login.max_failures` | int 1–1000 | `5` | per IP per 15 min |
| `session.timeout` | duration 5m–720h | `"12h"` | idle timeout |
| `session.admin_unlock` | duration 30s–24h, or `"0s"` | `"5m"` | admin idle timeout; `"0s"` keeps admin rights until sign-out |
| `tls.mode` | enum `self-signed`/`letsencrypt`/`custom` | `self-signed` | restart; letsencrypt not implemented yet (falls back to self-signed) |
| `tls.redirect` | bool | `true` | plain HTTP on the same port → redirect to HTTPS; restart |
| `tls.cert`, `tls.key` | absolute path | `""` | for `custom`; restart |
| `plugins.allow_unsigned` | bool | `false` | unsigned plugins are blocked (dev folders still run in developer mode, marked "Unsigned, dev") |
| `plugins.dev` | bool | `false` | |

Durations are Go duration strings (`"90s"`, `"5m"`, `"12h"`).

## `config.get` (user) → State

```json
{"path":"/etc/linuxadmin/linuxadmin.conf","exists":false,
 "values":{"listen":"0.0.0.0:9090","allow_root":false,"login.show_ip":true,"login.max_failures":5,
           "session.timeout":"12h","session.admin_unlock":"5m","tls.mode":"self-signed","tls.redirect":true,
           "tls.cert":"","tls.key":"","plugins.allow_unsigned":false,"plugins.dev":false},
 "defaults":{…same shape…},
 "keys":[{"key":"listen","type":"string","restart":true},{"key":"tls.mode","type":"enum","values":["self-signed","letsencrypt","custom"],"restart":true},…],
 "warnings":[]}
```
`type` ∈ `string|bool|int|duration|enum|path`. Secret keys (none yet) are never returned.
`warnings` lists unknown keys found in the file. An unparsable file → `conflict`.

## `config.set` (admin)

Params `{"key":"login.show_ip","value":false}` → the new State. Validates key and type
(`invalid` otherwise), copies the old file to `<file>.bak`, writes atomically (0644).
Comments in the file are not preserved (the backup keeps them).

## `config.rawDiff` (user)

Params `{"changes":{"login.show_ip":false,"session.timeout":"1h"}}` →
`{"current":"<toml>","proposed":"<toml>","diff":"<lines prefixed with ' ', '-', '+'>"}`.
Nothing is written. Invalid changes → `invalid`.
