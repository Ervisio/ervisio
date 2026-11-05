# Settings and daemon-owned admin state
Purpose: Settings page (preferences, server config, channels, plugin jobs, environments, activity log) and the backend that stores each piece.

## Entry points and public contracts
- `ResetControls.tsx`, `ResetProgressPage.tsx`: personal preference reset and admin complete Ervisio reset. Daemon `server/reset.go`, detached `maintenance/helper_linux.go`, safe staged engine `maintenance/reset_linux.go`; contract `docs/api/maintenance.md`. Full reset requires installed Linux/default managed paths; public progress uses a redacted read-only token across restart, outside RequireAuth.
- `web/src/sections/settings/index.tsx`: `SettingsPage`; `GroupDef[]` (part you/server/about, `admin`) of `RowDef` (`control` or full-width `block`, `cfgKey`, `restart`, `words` for search); `#hash` scrolls to a group.
- `save.ts`: `usePrefSave` (`prefs.set` + Undo toast), `useServerConfig(enabled)` (`config.get` noUnlock, `config.set` optimistic + Undo), `pick` (flat or nested key).
- `useAdminLoad.tsx`: `useAdminLoad(fn, pollMs)` -> `loading|ready|locked|failed`; `LockedPanel`. `NameList.tsx`: chip editor, `NAME_RE`.
- `jobsApi.ts`: `listChannels/saveChannel/deleteChannel/testChannel` (`notify.*`), `listJobs/setJobEnabled/approveJob/runJob/deleteJob/jobHistory`, webhook create/regenerate/revoke (`jobs.*`).
- `envs.ts` (`envs.*`), `ActivityLog.tsx` (`audit.list`, export link `GET /api/audit/export?format=csv|json`), `Appearance.tsx` (`ThemeGrid`, `ColourBlock`, theme editor/import/export via `useTheme`).
- Skipped here: Hosts/NetworkHosts/Pairing/Updates (see map/remote-hosts.md, map/updates.md); Pairing is rendered by the `envs` group.
- RPC (levels): `config.get` User, `config.set` Admin, `config.rawDiff` User (`server/internal/modules/config/config.go`); `prefs.get/set/setAll` User (`modules/prefs/prefs.go`).
- Daemon-local, all admin (`server/internal/server/jobsglue.go` `localMethods`): `notify.list/save/delete/test`, `jobs.list/setEnabled/approve/runNow/delete/history`, `jobs.webhooks.create/regenerate/revoke`; plugin-level `plugins.jobs.*`, `plugins.notify`.
- `server/internal/server/envs.go` `envsCall`: `envs.list/probe/create/update/delete/test/pairToken.create/pairings.revoke` admin; `plugins.envs.list` any user (Briefs).
- `server/internal/server/audit.go`: `audit.list`, `plugins.audit.list`, `/api/audit/export` (authed GET) for any user, own rows only unless admin.
- Packages: `internal/jobs` (`Manager`, `Env`, `Instance`, `Schedule`, `HandleHook`), `internal/notify` (`Service`, `Store`, `Spec`, `Event*`), `internal/audit` (`Log`, `Entry`, `Query`, `Record`), `internal/envs` (`Manager`, kinds tcp-tls/ssh/portainer-agent/ervisio, pairing).

## Flow
- Per-user prefs: page -> `prefs.set` -> user bridge -> `~/.config/ervisio/prefs.json` (`modules/prefs`); ThemeProvider/I18nProvider read the same prefs.
- Server config: `config.set` runs in the admin bridge, validates (`Set` then `Validate`), `cfg.Save` with .bak; daemon `configHolder` (`server.go`) re-reads on file change, so only `Restart` keys (listen, tls.*) need a restart.
- Wiring in `Server.New`: `initJobs` creates `notify.Service` (file `notify.json` beside the config) and `jobs.Manager` (state `<StateDir>/jobs`); audit log in `<StateDir>/audit`, `audit.SetDefault` lets background code call `audit.Record`.
- Job steps run via `bridgeExecutor` on the owner's bridge (pooled, idle-stopped); admin steps use a root bridge and need `jobs.approve` first.
- Notify: `Send(msg, event)` fans out to enabled channels subscribed to the event and at/above `minLevel`; events alerts/updates/plugins/jobs; alerts come from `alertwatch.go` (5 min scan while subscribed), updates from `NotifyCore`, jobs from the manager's `Alert`.
- Activity log: `auditBegin/callDone` wrap bridge RPC in `route.go`; core events via `auditCore`; rows are daily JSONL files pruned by `audit.retention_days`.
- `useAdminLoad` blocks refresh on lock/unlock; Environments polls `envs.list` every 30 s, jobs every 5 s (history 3 s).

## Invariants
- Secrets never leave the daemon: `config.get` drops `Secret` keys; channel secrets return only as `secrets{field:bool}`; an empty secret on `notify.save` keeps the stored one; `notify.json` is 0600 and written via tmp+rename; env credentials stay in the daemon.
- `jobs.*`, `notify.*`, `envs.*` are not `plugins.*`, so the plugin broker can never call them; keep approval-style methods out of `plugins.*`.
- `prefs` keys match `^[A-Za-z][A-Za-z0-9_.-]{0,63}$`, values <=256 KiB, file <=1 MiB; null deletes a key; corrupt file is moved to `.corrupt`.
- Plugin notifications are rate-limited per plugin and sender (10/min, 60/h); job limits: 200 instances, 50 per plugin, 20 kept runs, 4 concurrent, schedule >=60 s.
- Audit stores no secrets (`Redact*`); CSV cells are guarded by `csvSafe`; export sets sandbox CSP and no-store.
- Admin-only blocks use `useAdminLoad` (no automatic unlock dialog); direct config writes use `call` so the global unlock dialog opens once.

## Gotchas
- Local methods return `unavailable` if jobs/notifier failed to init, and `needs_admin` (not `not_found`) to non-admins; docs/api/jobs.md line ~192 says `not_found`, which is stale for `jobs.*`.
- `audit.list` and `plugins.audit.list` bypass the bridge (daemon is root); `canSeeAll` in the page is only a UI hint, the server filters by `IsAdmin`.
- Server settings default values are duplicated in `index.tsx` `server.get(key, fallback)`; keep them equal to `internal/config` defaults.
- `config.rawDiff` is User level and returns full current and proposed TOML; it relies on secrets not being part of the file text, so check `config.Encode` before adding secret keys.
- Daemon-started jobs with root steps fail under `--dev` (not root); `jobs.approve` needs a live unlock, group membership is not enough.
- Theme/colour/density are prefs, so Settings changes need `usePrefSave` or `useTheme` setters, not direct localStorage.

## Checks and references
- `cd server && go test ./internal/jobs ./internal/notify ./internal/audit ./internal/envs ./internal/modules/config ./internal/modules/prefs ./internal/server`; web: `npm run typecheck && npm run lint && npm run i18n:check` (namespaces `settings`, `envs`).
- Docs: `docs/api/config.md`, `jobs.md`, `notify.md`, `environments.md`, `prefs.md`; `docs/ARCHITECTURE.md` Preferences.
Related: map/server.md, map/web.md, map/remote-hosts.md, map/updates.md, map/shell-ui.md

Sweep helpers (2026-11-05): instanceLocked (jobs/api.go), intRange (config/keys.go), config.WriteAtomic, notify.WriteFileAtomic, AuditEntry.
