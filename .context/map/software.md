# Software
Purpose: list, search, install, remove and update system packages and Flatpak apps through the host's package managers, with progress, history and scheduled updates.

## Entry points and public contracts
- Backend `server/internal/modules/software/software.go:Register`. User level: `software.summary`, `check`, `updates`, `installed`, `info`, `search`, `suggest`, `apps`, `icon`, `history`, `status`.
- Admin level: `software.schedule`; stream `software.transaction`.
- User-level stream `software.transactionUser` accepts Flatpak scope `user`, or `source:"cli"` for one managed account-scoped CLI. Root/admin CLI-prefix transactions are rejected.
- `software.cli` lists managed Node.js LTS, Claude Code and Codex plus curated distro-backed tools. `cli*.go` owns bounded official Node archive/checksum downloads, staged npm-prefix installs, per-account locks/history, safe launchers and rollback. Existing unmanaged executables are detected without taking ownership.
- `TxParams{op upgrade|install|remove, packages, source repo|flatpak|aur|all|cli, scope system|user}`; events `start`, `step`, `progress`, `log`, `done{ok,message,hint,rebootNeeded}`.
- `backend.go:Backend` interface (Name, Kind, Available, ListInstalled/Updates, Search, Info, Install/Remove/Upgrade returning `Plan` of argv `Step`s, Refresh); `scoped` adds `WithScope` (Flatpak).
- Backends: `pacman.go` (+AUR helper yay/paru), `apt.go`, `rpm.go` (dnf, zypper), `winget.go` (+`winget_parse.go`), `flatpak.go`.
- Frontend `web/src/sections/software/`: `index.tsx` tabs (Store, Installed, Updates, History); `store.ts` shared cache + 5-min background summary; `tx.ts` job runner and attach; `badge.ts` rail count; `palette.ts`; `ScheduleDialog.tsx`; `SidePanel.tsx` details.

## Flow
- `getManager` picks one primary backend in order pacman, apt, dnf, zypper, winget; AUR helper only with pacman; Flatpak is detected independently (`newManager`).
- Reads fan out to all backends in parallel and are cached 10 minutes (`cacheTTL`, `force` bypasses); `invalidate` after a change.
- Transaction: `buildPlans` -> `txMu` single-run guard -> `lockedByOther` (pacman `db.lck`) -> run each Step as argv, parse lines into progress -> state file mirrored for `software.status`.
- The command keeps running if the client disconnects; UI `attachIfBusy` polls `software.status` every 1.5 s and mirrors the state (page reload, other session).
- UI `tx.ts` runs a `Job` as sequential transactions; Flatpak user scope and managed CLI tools go to the User stream with `admin:false`, distro packages use the root bridge (unlock dialog).
- Managed CLIs live under `~/.local/share/ervisio/cli` without changing the system runtime or shell profiles. Only account-owned launchers are created in `~/.local/bin`; collisions remain visible and untouched. Terminal receives existing managed directories only for that bridge account; Open terminal prefills an absolute command for review and, on Windows, selects PowerShell through shell metadata.
- History parses `/var/log/pacman.log`, `apt/history.log`, `zypp/history`, `dnf.rpm.log` (last 12 MiB, newest first).
- Schedule: Linux systemd timer pair in `/etc/systemd/system` (`schedule.go`); Windows Task Scheduler task `\Ervisio\SoftwareUpdate`, SYSTEM, daily (`schedule_task.go`, `schedule_windows.go`).

## Invariants
- Package names pass `validNames` before any argv is built; no shell involved.
- `source:"all"` only for upgrade-all and covers primary plus system Flatpak, never AUR.
- AUR: remove goes through pacman; install/upgrade return `errAURUnsupported`; AUR sizes are excluded from the update total.
- Flatpak install/remove needs packages; admin cannot use scope `user` (Invalid).
- State files and dirs are private and owner-checked (`privfile.go`: `ensurePrivateDir`, `writeFileAtomic`, `readOwnedFile`); non-root state lives in `$XDG_RUNTIME_DIR`, root state has a world-readable summary without log.
- Update notes: `reboot`, `restartService:<unit>`, `security`; UI and `summary` derive reboot and security counts from them.

## Gotchas
- `txRunning` is per process: the user and root bridges do not see each other, so only the pacman `db.lck` check and package-manager locks guard cross-bridge overlap.
- Per-distro: apt needs `apt-get` + `dpkg-query`; dnf needs `dnf` + `rpm` (security via `check-update --security`, exit code 100 means updates); zypper shares `rpm.go`; winget output is parsed tables with `--disable-interactivity`.
- Windows: winget is the only primary (no systemd; Flatpak only if its binary exists); `schedule_other.go` (`!windows`) stubs OS hooks and `osUpdatesCount` returns -1 for unknown; privfile has `privfile_windows.go`.
- Apps tab/icons scan `.desktop` files and icon dirs under `/usr/share` and Flatpak exports (`apps.go`); not meaningful on Windows.
- `software.summary` uses cached data; `software.check` refreshes the primary backend metadata (needs rights: apt/dnf `Refresh` check `isRoot`), then forces a new summary.
- Frontend `badge.ts` runs on every page; keep it quiet and cheap.

## Checks and references
- `cd server && go test ./internal/modules/software/` (`software_test.go`, `winget_test.go`, `privfile*_test.go`, `schedule_task_test.go`).
- `cd web && npm run typecheck && npm run i18n:check` (`web/src/i18n/{en,it}/software.json`).
- Docs: `docs/api/software.md`, `docs/SECURITY-REVIEW.md` (M3, L1).

Related: map/server.md, map/web.md

Sweep helpers (2026-11-05): changePkgs (rpm.go), withManager/unixMillis (software.go), queryBackends[T] (manager.go), regularIconIn (apps.go), aptPackagePlan (apt.go).
