# Updates
Purpose: tag-driven signed releases and the console's self-update (check, download, verify, switch, rollback) for versioned installs.

## Entry points and public contracts
- `server/internal/modules/updates/updates.go`: `Register` -> `updates.check` (User), `updates.status` (User), `updates.apply` (Admin, stream), `updates.rollback` (Admin); `DaemonDev` blocks updates in `--dev`.
- `server/internal/update/updater.go`: `Updater.Supported`, `Apply`, `Rollback`, `SystemdRun`, `UnitName`.
- `server/internal/update/github.go`: `Checker.Latest(channel, force)`, `ArchiveName`, `MaxArchive` (200 MB).
- `server/internal/update/sign.go`: `ReleasePublicKey`, `TrustedKeys`, `SignSums`, `VerifySums`, `ParseSums`, `VerifyFile`.
- `server/internal/update/layout.go`: `Layout` (`ManagedBy`, `Kind`, `Migrate`, `InstallFrom`, `SetCurrent`, `SetPrevious`, `Prune`); `state.go`: `State` (`last.json`, `Lock`, `PackageTransactionRunning`).
- `server/internal/update/switch.go`: `Switcher.Switch`, `RunHelper`, `HelperFlag = "--apply-update"`, `HealthTimeout` 30 s; `auto.go`: `Auto.Run/Tick`.
- `server/internal/update/{download,extract}.go`: `Downloader` (GitHub hosts only), `ExtractTarGz` + `DefaultLimits`.
- `server/internal/signkey/signkey.go`: key file parse/load/generate shared with plugin signing; `server/tools/release-sign/main.go`: `-genkey`, `-key`, `-verify [-dir]`.
- Web `web/src/sections/settings/`: `updatesApi.ts` (`checkUpdates`, `updateStatus`, `startUpdate`, `startRollback`, `health`, `useRun`), `Updates.tsx` (`UpdatesBlock`, Settings > About), `UpdateNotifier.tsx` (admin bell), `ReleaseNotes.tsx` (`parseMarkdown`).
- Config `[updates]` in `server/internal/config/config.go`: `channel` stable|prerelease, `auto_check` (true), `auto_install` (false), `auto_install_at` "03:30".
- `GET /api/health` (no auth, `server.go`): `{status, version, startedAt}`. Contract detail: `docs/api/updates.md`.

## Flow
- Release: tag `vX.Y.Z[-pre]` (or manual dispatch on main, creates the tag) -> `.github/workflows/release.yml`: web build, per-arch build via `build-compat.sh`, archive, deb/rpm, Windows zips -> `SHA256SUMS` over 10 assets + `SHA256SUMS.sig` (`release-sign`, secret `RELEASE_SIGNING_KEY`, self-verified against embedded key) -> `gh release create` (pre-release unless plain semver) -> optional AUR job (needs `AUR_SSH_KEY`, stable only).
- Notes: `CHANGELOG.md` section for the version, else annotated tag message, else a stub; fixed install footer appended.
- Apply: reject if managed/Windows/not installed/running/package txn -> lock -> latest (must equal confirmed version) -> download SHA256SUMS+sig -> verify -> download archive -> sha256 -> extract to partial dir -> run both binaries `--version` -> migrate flat->versioned if needed -> `InstallFrom` -> `systemd-run` transient unit running new `ervisiod --apply-update V` -> flip `current`, restart, poll `/api/health` for V; on failure switch back (`rolled-back`). Keeps `current` + `previous` only.
- Rollback: same helper toward `previous`, started from the running version's binary.
- Auto (daemon): ticks every 20 s; checks every 6 h (first ~2 min after start), notifies `EventUpdates` once per version; installs once per day within 2 min of `auto_install_at`, 30 min timeout. Packaged installs only check/log.
- Web: after `restart` event, polls `health()` every 1.5 s until new version (done), old version with new `startedAt` (rolled-back) or timeout, then reloads. Notifier checks admins every 6 h when `autoCheck`.

## Invariants
- Only keys in `TrustedKeys` verify releases; signed message is `"linuxadmin-release-v1\n" + SHA256SUMS`. Archive must be listed in the signed sums.
- Downloads: https only, `github.com` / `*.githubusercontent.com`, port 443, max 5 redirects; sums file <= 64 KB, sig <= 4 KB.
- Extraction: single top-level `ervisio-V-linux-ARCH/`, regular files/dirs only, no `..`/absolute/duplicates; limits 20000 entries, 256 MB/file, 1 GB total.
- `updates.apply` takes the version the user confirmed; changed latest -> `conflict`. Managed marker -> `unavailable` for apply/rollback.
- `Newer` is semver; dev builds count as older than any release; prerelease ordering `1.2.0-rc.1 < 1.2.0`.
- Never re-publish different files under an existing version.
- Notes are Markdown rendered as text only: links lose URLs, HTML escaped, images become alt text.

## Gotchas
- `sigPrefix` still says linuxadmin on purpose (old consoles and `install.sh` verify with it).
- `install.sh` trusts one key (`RELEASE_KEY_PEM`), unlike the daemon's `TrustedKeys`; rotation must update PEM in the same commit (`TestInstallScriptReleaseKey`). See rotation steps in `docs/RELEASING.md`.
- `updates.check` cache (1 h, ETag) is per bridge process; `force` bypasses it.
- Helper must run in a transient unit: the daemon restart would kill a child; `systemd-run` is required (`Supported` checks).
- A `running` `last.json` whose helper died is reported as `failed/interrupted`.
- Windows: no self-update; `Supported` returns `ErrWindows`.
- Release workflow is the only publisher; `ci.yml` and `windows.yml` are `workflow_dispatch` only, so tags are not gated by CI (release job runs `go vet`/`go test` only).

## Commands and references
- `cd server && go test ./internal/update/... ./internal/modules/updates/...`; `go run ./tools/release-sign -verify -dir DIR DIR/SHA256SUMS`; `go run ./tools/release-sign -genkey FILE`.
- `make dist VERSION=X.Y.Z` builds the archives locally (unsigned).
- Docs: `docs/RELEASING.md` (setup, cutting, layout, rotation, rename transition), `docs/api/updates.md`.
Related: map/packaging.md, map/server.md, map/web.md
