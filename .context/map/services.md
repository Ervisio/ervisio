# Services
Purpose: list, inspect, control and watch systemd units (service/timer/socket) on Linux or Windows services via the Service Control Manager.

## Entry points and public contracts
- `web/src/sections/services/index.tsx`: page (tabs all/failed/running/stopped/timers/sockets, purpose groups web/containers/system on Linux, FailedBanner); `UnitPanel.tsx`: side panel tabs info/logs/unit/deps; `useActions.tsx`: `run(name, action)` with confirmations and toasts.
- `store.ts`: shared state for always-on hooks; `badge.ts` = failed-count rail badge plus toast/notification for newly failed units; `palette.ts` = restart action; `helpers.ts`: `needsConfirm`, `isCritical`, `bootMode`.
- `server/internal/modules/services/services.go` `Register`: `services.list` User (`type` service|timer|socket, `fresh`), `services.summary` User, `services.get` User, `services.action` Admin, `services.unitFile` User, `services.saveOverride` Admin, `services.logs` User (`lines` default 100, max 1000), stream `services.watch` User.
- Actions: Linux start, stop, restart, reload, enable, disable, mask, unmask. Windows: start, stop, restart, pause, continue, enable, disable, mask, unmask (no reload).
- Shapes in `services.go` (`Unit`, `FailedUnit`, `LogLine`); `units.go` `ValidUnitName`, `PurposeOf` (glob table, first match); `hints.go` failure hints (`hintId` translated by the web app).
- Docs: `docs/api/services.md`.

## Flow
- Page loads `services.summary` + `services.list`, polls both at `max(refresh interval, 2 s)`, and merges pushes from `services.watch` (`{units, removed}`), then reloads the summary after 300 ms and bumps the open panel.
- Action -> `services.action` (admin dialog + retry handled by the shared client) -> toast -> `after` refreshes lists with `fresh`.
- Unit file: `unitFile` returns unit content, drop-ins and the override at `/etc/systemd/system/<unit>.d/override.conf`; `saveOverride` writes atomically then runs `daemon-reload`; empty content removes the override.
- Always-on: `useBackgroundPoll` calls `services.summary` every 60 s (badge) when the page is not mounted; deep links `/services?unit=&tab=&state=` (CONTRIBUTING-SECTIONS.md).

## Invariants
- Linux names must pass `ValidUnitName` (known suffix, no `/`, no leading `-` or `.`, 3..255); systemctl receives `--` before names. All commands are argv through `sys`, never a shell.
- `action` has a 90 s timeout and reports `unavailable` with a hint to check the log.
- Override max 256 KiB, no NUL; a save that succeeded but failed to reload returns an error saying the file was saved.
- Unit file states from `list-unit-files` are cached 15 s per type; `fresh:true` bypasses it (needed after enable/disable). Descriptions of not-loaded units are cached in `descCache`.
- Critical units (`CRITICAL`/`WIN_CRITICAL` regex) need type-to-confirm for stop/disable/mask; mask elsewhere needs a plain confirm.
- `removed` units in watch are shown as stopped/not-loaded, not deleted from the list.

## Gotchas
- Platform split: no build tags on `services.go`; `services_unix.go` (+`_unix_test.go`) and `services_windows.go` define the handlers; `winmap.go` maps SCM state/start types with raw constants so it is testable everywhere.
- Windows mapping: enable = auto start, disable/unmask = demand start, mask = disabled start type. Enable/disable also clears delayed auto start. `unitFile`, `saveOverride`, `logs` return `unavailable`; UI hides logs/unit tabs, timers/sockets tabs, purpose groups and the log/disable buttons.
- Watch diff differs by OS: Linux compares load/active/sub/description every 2 s (an enable/disable alone is not pushed, hence `fresh` reloads); Windows ticks every 3 s and also compares `Enabled` and `PID`. A client `input` frame forces an immediate check.
- `services.watch` ignores transient list errors and keeps looping; it only fails on the initial snapshot.
- `services.logs` maps journal failures to `unavailable`, not `needs_admin`; `/logs?unit=` is the richer reader.
- Unit param without suffix gets `.service` appended on Linux only.

## Checks and references
- `cd server && go test ./internal/modules/services/...`; `GOOS=windows go vet ./internal/modules/services/`.
- `cd web && npm run typecheck && npm run i18n:check` (namespace `services`).
- Related: map/server.md, map/web.md, map/overview.md (failed-unit alerts and `unitStatus` widget), map/logs.md.

Sweep helpers (2026-11-05): bindUnitShow (services_unix.go).
