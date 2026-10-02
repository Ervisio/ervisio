# Notification channels (`notify.*`, `plugins.notify`)

Settings › Notification channels sends alerts out of the console: email (SMTP), Telegram, a generic webhook, ntfy and
Gotify. The core's alerts, plugins and plugin jobs use the same channels.

Code: `server/internal/notify` (channels file, senders, rate limit), `server/internal/server/jobsglue.go` (methods),
`server/internal/server/alertwatch.go` (Overview alerts). Web: `web/src/sections/settings/Channels.tsx`.

## Channels

Stored in one file, `notify.json` next to the configuration (`/etc/ervisio/notify.json`; `--dev-state-dir` in dev),
mode **0600**, written by the daemon (root). It holds the secrets, so it is not part of `ervisio.conf`.

| `type` | fields | secret fields |
|---|---|---|
| `email` | `host`, `port`, `security` (`starttls` default, `tls`, `none`), `username`, `from`, `to` (1 to 10 addresses) | `password` |
| `telegram` | `chatId` (a number, or `@channel`) | `botToken` |
| `webhook` | `method` (POST, PUT, PATCH), `template`, `headerName` | `url`, `headerValue` |
| `ntfy` | `server` (default `https://ntfy.sh`), `topic` | `token` (optional) |
| `gotify` | `server` | `token` |

Common: `id` (assigned), `name`, `enabled`, `minLevel` (`info` default, `warn`, `error`) and `events`: which
notifications the channel gets:

| event | sent when |
|---|---|
| `alerts` | the Overview alerts (`overview.alerts`: a failed service, pending updates, failed SSH sign-ins, disk, swap) show a new warning or error, or a warning gets worse. The daemon asks a root bridge every 5 minutes while a channel subscribes; the first scan after a start only records. |
| `updates` | a new Ervisio release is found by the automatic update check (`updates.auto_check`) |
| `plugins` | a plugin calls `plugins.notify`, or a job `notify` step runs |
| `jobs` | a plugin job starts failing, and when it works again |

A new channel subscribes to all four.

**Webhook template**: JSON text with the placeholders `{title}`, `{body}`, `{level}`, `{link}`, `{source}`, `{time}`
(RFC 3339 UTC). Each value is JSON-escaped for you (no quotes added), so a title with a quote or a newline stays inside its
string. The default is `{"title":"{title}","body":"{body}","level":"{level}","link":"{link}","source":"{source}","time":"{time}"}`.
The template must be valid JSON once rendered. `headerName`/`headerValue` add one secret header (for example a
shared secret the receiver checks). Redirects are not followed; any answer outside 2xx is a failure.

**Email**: STARTTLS (587) or implicit TLS (465) with certificate checking, or none (25). Authentication is PLAIN, which Go
refuses to send over an unencrypted connection except to localhost. Subject and body are UTF-8 (the subject Q-encoded, the
body quoted-printable); header values are one line, so a title cannot add headers.

### Secrets

* Never returned. `notify.list` / `notify.save` return `Channel`, a type with **no secret fields**: `secrets` says which
  ones are set (`{"password": true}`), and a webhook's `urlHint` is its scheme and host only (service URLs such as Slack's
  carry a token).
* A secret left empty when saving keeps the stored one; changing the channel's `type` drops them.
* Delivery errors are redacted (a Telegram token sits in a URL that Go puts into its errors; the stored secrets are
  replaced by `***`) and cut to 300 characters.
* A plugin never sees any of it. `plugins.notify` returns only counts.

## Methods (daemon, admin level)

Without administrator rights they fail with `needs_admin`.

| Method | Params → result |
|---|---|
| `notify.list` | `{}` → `{channels: Channel[], file}` |
| `notify.save` | a channel: `{id?, name, type, enabled, minLevel?, events?, …fields, …secrets}` → `Channel`. No `id` creates one (at most 20). `invalid` with a message for people when something is wrong. |
| `notify.delete` | `{id}` → `{}` |
| `notify.test` | the same shape as `notify.save`, sends a test message through it **without saving**. With `id`, empty secrets come from the stored channel, and the result is shown as the channel's last result. On failure: `invalid` with what went wrong, without the secrets. |

`Channel.last` is the result of the last delivery or test since the daemon started: `{at, ok, error?}`.

## `plugins.notify` (user level; SDK `sdk.api.notify`)

`{plugin, title, body?, level?, link?}` → `{channels, delivered, failed}`.

* The manifest must declare `capabilities.notify: true` (`forbidden` otherwise) and the plugin must be enabled and visible
  to the caller. `title` up to 1000 characters (cut to 200), `body` up to 4000, `level` one of `info`, `success`,
  `warn`, `error` (default `info`), `link` an `http(s)` address or an app path (`/p/docker/stacks`), at most 500.
  Control characters are removed; the message's `source` is the plugin's name.
* **Rate limit per plugin**: 10 a minute and 60 an hour, shared with job `notify` steps. Over it:
  `unavailable`, `data: {reason: "rate_limited", retryAfter: seconds}`.
* Sent to the enabled channels subscribed to `plugins` whose `minLevel` the message reaches. With none configured the
  result is `{channels: 0, delivered: 0, failed: 0}`, not an error.
