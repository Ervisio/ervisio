# services.* (Services section)

Bridge module `server/internal/modules/services`. Talks to systemd with `systemctl` / `journalctl`
(argv, no shell, no D-Bus). Unit names are validated strictly: letters, digits and `: _ . @ \ -`
followed by a known suffix (`service socket timer target mount automount path slice scope swap device`),
no leading `-` or `.`, at most 255 characters. Anything else answers `invalid`.

Times are unix seconds (`since`, `next`, `last`) unless stated. `memory` is bytes and `null` when
systemd does not account for it; `cpuNs` is cumulative CPU time (compute % from two samples).

## Unit object (services.list rows, services.watch units)

```json
{ "name": "nginx.service", "description": "A high performance web server", "load": "loaded",
  "active": "active", "sub": "running", "state": "running", "enabled": "enabled",
  "memory": 14893056, "cpuNs": 228932000, "pid": 3192, "since": 1790871752,
  "purpose": "web" }
```
- `load`: systemd LoadState; installed unit files that are not loaded show `not-loaded`.
- `state`: simplified: `running | failed | stopped | finished` (`finished` = active but exited, e.g. oneshot).
- `enabled`: `enabled | enabled-runtime | disabled | static | masked | indirect | generated | alias | linked | ...`, `""` unknown.
- `purpose`: `web | containers | system` (heuristic, one table in `units.go`).
- Timers add `next`, `last`, `triggers`; sockets add `listen: ["/run/docker.sock (Stream)"]`, `triggers`.

## services.list (user)
Params `{type?: "service"|"timer"|"socket" (default service), fresh?: bool}`.
Result `{units: Unit[]}` sorted by name. Includes installed unit files that are not loaded. `fresh`
skips the 15 s cache of `systemctl list-unit-files` (use right after enable/disable).
Errors: `invalid`, `unavailable` (systemd not running).

## services.summary (user)
Params none. Result:
```json
{ "total": 235, "running": 46, "stopped": 185, "timers": 14, "sockets": 72,
  "failed": [ { "name": "systemd-networkd-wait-online.service", "description": "...", "since": 1790871686,
                "result": "exit-code", "exitCode": 1,
                "hintId": "networkd-wait-online", "hint": "You use NetworkManager, so you probably don't need this unit..." } ] }
```
`total/running/stopped` count services (`running` includes `finished`); `failed` covers services, timers and
sockets. `hint`/`hintId` (English text and a stable id the web app translates) only appear for known cases
(wait-online units, start-limit-hit, oom-kill, timeout, exit 203/217/127...).

## services.get (user)
Params `{name}`. Result: the Unit fields plus `type`, `path` (unit file), `dropIns[]`, `result`, `exitCode`,
`documentation[]`, `canStart/canStop/canReload`, `dependencies` and `properties` (all `systemctl show` properties as strings):
```json
"dependencies": { "requires": [], "wants": [], "after": [], "before": [], "requiredBy": [], "wantedBy": [],
                  "conflicts": [], "boundBy": [], "partOf": [], "triggers": [], "triggeredBy": [] }
```
Errors: `invalid`, `not_found`.

## services.action (admin)
Params `{name, action}` with action `start|stop|restart|reload|enable|disable|mask|unmask`. Result `{ok:true}`.
Errors: `invalid`, `not_found`, `conflict` (systemd refused: message is systemctl's first line), `unavailable` (timed out after 90 s).

## services.unitFile (user)
Params `{name}`. Result:
```json
{ "path": "/usr/lib/systemd/system/docker.service", "content": "[Unit]...",
  "overrides": [ { "path": "/etc/systemd/system/docker.service.d/override.conf", "content": "[Service]..." } ],
  "overridePath": "/etc/systemd/system/docker.service.d/override.conf", "override": "<content of that file or empty>" }
```
`overrides` lists every drop-in systemd loaded. Files the caller cannot read come back with empty content.

## services.saveOverride (admin)
Params `{name, content}`. Writes `/etc/systemd/system/<name>.d/override.conf` (atomically, 0644), then runs
`systemctl daemon-reload`. Empty or blank `content` removes the override file (and its directory when empty).
Content limit 256 KiB, no NUL. Result `{ok:true, path}`. The package's own unit file is never touched.
The service must be restarted for the change to take effect.

## services.logs (user)
Params `{name, lines?: 1..1000 (default 100)}`. Result `{lines: [{time (unix ms), priority (0-7), message}]}`, oldest first.
Runs `journalctl -u <name> -n <lines> -o json`; the journal may be empty for accounts outside `systemd-journal`/`adm`
(call with `admin:true` to read it as root). The Logs section's own query API may be used instead for filtering.

## services.watch (stream, user)
Params `{type?: "service"|"timer"|"socket"}` (default service). Every 2 s the bridge diffs `systemctl list-units`
and pushes only changes (nothing is sent at open): `{units: Unit[] (new or changed, with details), removed: string[]}`.
`removed` names left systemd's loaded set. Enable/disable changes are not pushed; refetch with `services.list {fresh:true}`.

## Windows backend

On Windows the same methods are served by the Service Control Manager (`golang.org/x/sys/windows/svc/mgr`,
`services_windows.go`); the SCM enforces rights and `ERROR_ACCESS_DENIED` answers `needs_admin`.
- Only `type: "service"` has rows; `timer` and `socket` return an empty list. `name` is the service short name (no `/` or `\`).
- `active/sub/state`: running -> `active/running/running`, pending states -> `activating|deactivating`, paused -> `active/paused/running`,
  stopped -> `inactive/dead/stopped`, stopped with a non-zero exit code -> `failed`. `memory`/`cpuNs` are `null`, `since` is 0.
- `enabled`: Automatic and Automatic (Delayed) -> `enabled`, Manual -> `disabled`, Disabled -> `masked`, boot/system drivers -> `static`.
- `services.action`: `start`, `stop`, `restart` (stop, wait, start), plus `pause` and `continue`; `enable` = Automatic, `disable` and `unmask` = Manual,
  `mask` = Disabled. `reload` answers `invalid`. Errors: `needs_admin`, `not_found`, `conflict` (already running, disabled...), `unavailable` (timeout).
- `services.get`: `path` is the binary path, `dependencies.requires` the service's dependencies, `dependencies.requiredBy` its dependent services;
  `properties` holds DisplayName, StartType (auto|delayed|manual|disabled|boot|system), ServiceStartName.
- `services.unitFile`, `services.saveOverride` and `services.logs` answer `unavailable`.
