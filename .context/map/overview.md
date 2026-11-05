# Overview
Purpose: customizable dashboard (widgets, alerts, user actions) fed by host info, live metrics and a few overview-only bridge methods.

## Entry points and public contracts
- `web/src/sections/overview/index.tsx`: page; edit mode, Library, SettingsDialog; layout saved with `set('dashboard', {version:1, widgets})`. `model.ts`: `Layout`, `Widget`, `CATALOG`, `normalizeLayout`, `SIZES` (3,4,5,6,7,12 cols), `splitArgv`/`joinArgv`.
- `data.ts`: `useMetrics` (shared `system.metricsStream` + 1 h in-memory history), `useHost` (cached `system.host`), `useAlerts` (`overview.alerts`).
- `actions.ts`: `runAction`, `requestConfirm`, `showOutput`; `palette.ts` exposes Personalize (`/?personalize=1`) plus every custom action.
- `server/internal/modules/system/system.go` `Register`: `system.host` User, `system.metrics` User, `system.metricsStream` stream User (`interval` ms, default 2000, clamped 250..600000), `system.power` Admin (`reboot`|`poweroff`).
- `server/internal/modules/overview/overview.go` `Register`, all User: `overview.alerts`, `overview.actionRun` (`{argv, timeout}`), `overview.processes` (`{sort cpu|mem, limit<=100}`), `overview.unitStatus` (1..30 units), `overview.logTail` (`{unit|file, lines<=200}`).
- Types: `system/host.go` `Host`, `system/metrics.go` `Metrics`/`Disk`/`Net`; `overview/alerts.go` `Alert`; `actions.go` `ActionResult`.
- Docs: `docs/api/overview.md`, `docs/api/system.md`.

## Flow
- Metrics: widgets subscribe to one module-level stream (`reopen: true`); last subscriber leaving closes it after 5 s; refresh-interval change restarts it; "Refresh now" reopens it and reloads alerts.
- Alerts: `alerter.collect` = failed units + cached updates (10 min) + cached SSH failures (5 min) + disks + swap, sorted err, warn, ok. A check that cannot run is omitted. UI translates `titleKey` in `overview` ns `alerts.*`; `action{section,params}` becomes a link (see CONTRIBUTING-SECTIONS.md table).
- Widget polling (`usePoll` in `widgets.tsx`) runs as user with `noUnlock`; shows an Unlock button on `needs_admin` instead of prompting.
- Custom actions: user confirms, then `overview.actionRun` with `{admin}` option; result in `OutputDialog`.

## Invariants
- `overview.actionRun` is registered User on purpose: admin actions are routed to the root bridge by the client's `admin:true`, not by method level.
- Actions run as argv via `sys.Cmd` (no shell), cwd = user home, timeout default 30 s, max 600 s, output capped at 256 KiB (`Truncated`), exit -1 on kill.
- Unit names must match `unitRe`, no leading `-`, `.service` is appended when no dot; systemctl gets `--` before the name.
- `logTail` file paths must be absolute; reads happen as the calling bridge, so permission errors map to rpc codes.
- `system.metrics` shares one `sampler` across calls; each stream gets its own sampler. First sample waits 250 ms for a baseline (also if previous is older than 30 s).
- Disk list: unix parses `/proc/self/mountinfo`, keeps only `realFS` types, dedupes by device id (shortest mount wins), `statfs` guarded by a 1 s timeout for hung NFS.

## Gotchas
- Platform split: `system/{disks,host,metrics}_unix.go` (`//go:build unix`, /proc, statfs) vs `_windows.go` (`sys.FixedDrives`/`DiskSpace`, no load average: `Load` stays zero). `overview/{alerts,disk}_{unix,windows}.go`; processes: `/proc` sampling over 400 ms (USER_HZ 100) vs `procs_windows.go` via `windowsProcesses`; `procs_other.go` stub returns nil.
- `*_fmt.go` / `procs_win_calc.go` hold pure Windows helpers that compile everywhere so tests run on Linux.
- No platform branch in `system.power`, `overview.unitStatus`, `overview.logTail` unit mode: they call `systemctl`/`journalctl`, so they fail on Windows. Windows `log` widget uses `logs.query` with `evt:*` instead (`widgets.tsx` evt branch); `service` widget still calls `unitStatus`.
- Default actions/widgets differ by OS: `setWindowsDefaults` swaps Linux (`ss`, `df`, `journalctl --vacuum`, `systemctl daemon-reload`) for PowerShell ones; it must run before `defaultLayout()`.
- `overview.processes` has no caller in `web/src` (grep); `system.power` neither.
- `Metrics.Disks`/`Net` are always arrays (never null); `Time` is unix ms, `Interval` seconds.

## Checks and references
- `cd server && go test ./internal/modules/overview/... ./internal/modules/system/...`; `GOOS=windows go vet ./internal/modules/...` for the Windows files.
- `cd web && npm run typecheck && npm run i18n:check` (namespace `overview`).
- Related: map/server.md, map/web.md, map/services.md (alerts link there), map/logs.md (log widget).
