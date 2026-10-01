# updates.* (self-update)

Package `server/internal/modules/updates` (RPC) on top of `server/internal/update` (download, verification,
install layout, switch helper, automatic installs). Release process and layout: `docs/RELEASING.md`. UI:
Settings › About (`web/src/sections/settings/Updates.tsx`), bell notice for admins (`UpdateNotifier.tsx`).

## `updates.check` (user)

Params `{"force": false}`. Asks GitHub for the newest release of the configured channel
(`/repos/Fonlogen/LinuxAdmin/releases/latest` for `stable`, the highest semver of `/releases` for `prerelease`).
The answer is cached for 1 hour per bridge process and revalidated with `If-None-Match` afterwards; `force` skips the
hour. 15 s timeout.

```json
{"current":"1.0.0","channel":"stable","autoCheck":true,"checkedAt":1790000000000,"arch":"amd64",
 "newer":true,
 "latest":{"version":"1.1.0","tag":"v1.1.0","name":"LinuxAdmin 1.1.0","publishedAt":1789900000000,
           "notes":"## Changes\n- …","url":"https://github.com/…/releases/tag/v1.1.0","prerelease":false,
           "asset":"linuxadmin-1.1.0-linux-amd64.tar.gz","size":16432392}}
```
`latest` is `null` when nothing is published. `notes` is Markdown: render it as text (the UI never uses HTML).
`size` 0 = the release has no archive for this architecture. GitHub unreachable or rate-limited → `unavailable`.
`newer` compares semantic versions; a dev build (`0647974-dirty`) is older than any release.

## `updates.status` (user)

```json
{"current":"1.0.0","install":"versioned","canUpdate":true,"reason":"",
 "previous":"0.9.2","installed":["0.9.2","1.0.0"],
 "last":{"state":"ok","kind":"update","from":"0.9.2","to":"1.0.0","startedAt":…,"finishedAt":…,"auto":false},
 "running":false,"packageBusy":false,"arch":"amd64",
 "settings":{"channel":"stable","autoCheck":true,"autoInstall":false,"autoInstallAt":"03:30"}}
```
`install`: `versioned` | `flat` (old layout, migrated by the first update) | `none` (not installed, e.g. a build
folder). `canUpdate` false with a `reason` in `--dev`, when not installed in `/usr/lib/linuxadmin`, or without
`systemd-run`. `last.state`: `running` | `ok` | `failed` (nothing changed) | `rolled-back` (switched, the new version
did not answer, back on the old one); a `running` record whose helper died reads as `failed` / `interrupted`.
`packageBusy`: a Software transaction runs, updates wait.

## `updates.apply` (admin, stream)

Params `{"version":"1.1.0"}`: the version the user confirmed (`conflict` if the latest release is now another one).
Streams progress events, then ends:

```json
{"phase":"check"}
{"phase":"download","file":"SHA256SUMS"}
{"phase":"verify","file":"SHA256SUMS"}
{"phase":"download","file":"linuxadmin-1.1.0-linux-amd64.tar.gz","done":5242880,"total":16432392,"percent":31}
{"phase":"verify","file":"linuxadmin-1.1.0-linux-amd64.tar.gz"}
{"phase":"extract"}  {"phase":"test"}  {"phase":"install","version":"1.1.0"}
{"phase":"restart","version":"1.1.0","unit":"linuxadmin-update-1.1.0-1790000000"}
```
After `restart` the daemon goes down within seconds (the WebSocket closes); poll `GET /api/health` until it reports
the new version (or the old one with a new `startedAt`: rolled back), then reload the page. Every session ends.
Closing the browser does not abort a running download/install (bounded to 30 min).

Errors: `conflict` (another update runs, a package transaction runs, already up to date, the release changed),
`unavailable` (not installed / dev / no build for this architecture, GitHub unreachable), `internal` with the reason
for verification failures (`… does not match its sha256 …`, `the release signature does not match …`, unsafe
archive entries, a binary that does not run).

## `updates.rollback` (admin)

Params `{"version":"0.9.2"}` (must be `previous`; `conflict` otherwise, `not_found` without a previous version) →
`{"version":"0.9.2","unit":"linuxadmin-update-0.9.2-…"}`. Starts the switch helper (the running version's binary)
and returns; wait on `/api/health` as for `apply`.

## `GET /api/health` (no authentication)

`{"status":"ok","version":"1.1.0","startedAt":1790000000000}` (`Cache-Control: no-store`). Used by the switch helper
(loopback) and by the web app while the daemon restarts.

## Switch helper

`linuxadmind --apply-update <version> [--kind update|rollback] [--auto] [--config FILE]`, root only, started in a
transient unit by `updates.apply`, `updates.rollback` and the automatic install. Takes `/var/lib/linuxadmin/updates/lock`,
switches `current`, restarts `linuxadmin.service`, waits up to 30 s for `/api/health` to report the version printed by
`<version>/bin/linuxadmind --version` (an old build without it: any answer of `/api/public/host`), and rolls back
otherwise. Keeps `current` + `previous` only. Writes `last.json`. Logs to its unit's journal.
