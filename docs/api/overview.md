# overview.* (Overview dashboard)

Package `server/internal/modules/overview`. Host info and live metrics come from the `system`
module (`docs/api/system.md`); the dashboard layout is the preference key `dashboard`
(see "Preferences" below).

## `overview.alerts` (user) → Alert[]

Things that need attention, most severe first (`err`, `warn`, then `ok`). A check that cannot run
(no package manager, journal not readable, no `sshd`) is left out, never reported as an error.

```json
[{"id":"unit:foo.service","severity":"err","titleKey":"failedUnit","title":"foo.service failed",
  "vars":{"unit":"foo.service"},"detail":"Failed Wed 2026-10-01 14:35:02 CEST (exit-code)",
  "action":{"section":"services","params":{"unit":"foo.service"}}},
 {"id":"updates","severity":"warn","titleKey":"updates","title":"23 updates ready","vars":{"count":23},
  "detail":"linux 7.2.7.arch1-1, firefox 140.0.1-1","action":{"section":"software"}},
 {"id":"ssh","severity":"ok","titleKey":"noSSH","title":"No failed SSH logins in the last 24 hours","detail":"Checked at 15:54"}]
```

| id | severity | source |
|---|---|---|
| `unit:<name>` (max 6, then `units-more`) | err | `systemctl list-units --state=failed --output=json`; detail from `systemctl show` |
| `updates` | warn when pending, ok when none | `pacman -Qu` / `apt list --upgradable` / `dnf check-update --cacheonly`, cached 10 min. Never refreshes the package databases (`checkupdates` is not used because it downloads). |
| `ssh` | warn when failures, ok when none | `journalctl -t sshd -t sshd-session --since "24 hours ago"`, only if `sshd` is installed and the journal is readable; cached 5 min. Counts `Failed password`, `Invalid user`, `Failed publickey`, `authentication failure`. |
| `disk:<mount>` | warn above 85 %, err above 95 % | statfs of every real filesystem in `/proc/mounts`, one per device |
| `swap` | warn above 50 % | `/proc/meminfo` |

- `titleKey`: key under `alerts.` in the `overview` i18n namespace, interpolated with `vars`.
  `title` and `detail` are English fallbacks.
- `action`: the web app navigates to `/<section>?<params>`; may be absent.

## `overview.actionRun` (user; pass `admin:true` to run as root) → ActionResult

Runs a user-defined action. Actions are stored in prefs (`dashboard` widgets) as argv arrays and
sent here as-is; nothing goes through a shell (use `["sh","-c","…"]` explicitly).

Params `{"argv":["systemctl","restart","nginx"],"timeout":30}`; `timeout` in seconds (default 30,
max 600). Validation: `argv` must be a non-empty array of strings, first element non-empty, at
most 128 elements, each at most 8192 bytes, no NUL.

```json
{"ok":false,"exitCode":3,"output":"out\nerr\n","truncated":false,"timedOut":false,"durationMs":12}
```
- `output` is stdout and stderr interleaved, at most 256 KiB (`truncated`).
- A non-zero exit is a normal result (`ok:false`), not an RPC error. A timeout kills the process
  group: `timedOut:true`, `exitCode:-1`.
- Errors: `invalid` (bad argv), `unavailable` (command not installed or not executable),
  `needs_admin` is raised by the daemon when `admin:true` is used without unlocking.
- The level is `user` on purpose, so one method serves both modes: the client sets `admin:true`
  for actions with "Run as administrator".

## `overview.processes` (user) → Process[]

Params `{"sort":"cpu"|"mem","limit":5}` (default cpu, 5; limit at most 100). Takes two samples
400 ms apart.

```json
[{"pid":1234,"name":"firefox","command":"/usr/lib/firefox/firefox -contentproc …","user":"fonlogen",
  "state":"S","cpu":12.5,"memory":512000000,"memPercent":3.1}]
```
`cpu` is percent of one core (250 = 2.5 cores busy), `memory` resident bytes. Errors: `invalid`.

## `overview.unitStatus` (user) → UnitStatus[]

Params `{"units":["nginx","sshd.service"]}` (1–30; a name without a suffix gets `.service`).
```json
[{"unit":"nginx.service","description":"A web server","active":"active","sub":"running","enabled":"enabled"}]
```
`active` is `active|inactive|failed|activating|deactivating|not-found`. Errors: `invalid` for a bad name.

## `overview.logTail` (user) → `{"lines":[…]}`

Params `{"unit":"nginx","file":"","lines":12}` (`lines` default 12, max 200). With `file` (absolute path)
the last lines of that file (reads at most the last 256 KiB); else the journal, filtered by `unit` when
given. Errors: `invalid`, `not_found`, `needs_admin` (file or journal not readable by the user; retry
with `admin:true`).

## Preferences used by the section

`dashboard` (via `prefs.set`):

```json
{"version":1,"widgets":[
  {"id":"w1","type":"stat","cols":3,"settings":{"metric":"cpu"}},
  {"id":"w5","type":"activity","cols":7},
  {"id":"w6","type":"actions","cols":5,"settings":{"actions":[{"id":"a1","label":"Restart nginx","icon":"refresh","hue":"svc","argv":["systemctl","restart","nginx"],"admin":true}]}},
  {"id":"w7","type":"alerts","cols":7},
  {"id":"w8","type":"machine","cols":5}]}
```
Widget types: `stat` (metric cpu|memory|disk|network), `activity`, `chart` (metric, range),
`actions`, `action` (one button), `alerts`, `machine`, `service` (units[]), `log` (unit, lines),
`output` (argv, every, admin), `folder` (path, label), `plugin` (plugin, widget).
`cols` is 3, 4, 5, 6, 7 or 12. Widget order is the array order.

## Windows notes

`overview.alerts` on Windows reports stopped Automatic services with a non-zero exit code as failed units (read-only service control manager query), fixed-drive usage, and page-file usage as swap. Update and SSH checks only run where their tools exist. `overview.processes` on Windows lists processes from a toolhelp snapshot: `memory` is the working set, `command` the full image path (`[name]` when it cannot be opened), `user` is `DOMAIN\name` (empty when unreadable), and `state` is empty. CPU comes from the delta against the previous call when it is 0.2 s to 2 min old, otherwise from two samples 400 ms apart; processes that cannot be opened report 0 CPU and memory. Not tested on a real Windows host.
