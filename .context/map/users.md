# Users
Purpose: manage local accounts, groups, SSH authorized keys and login sessions of the host.

## Entry points and public contracts
- Backend `server/internal/modules/users/users.go:Register`; `registerPlatform` (`register_unix.go` returns false, `users_windows.go` replaces all handlers).
- User level: `users.list`, `users.groups`, `users.sessions`, `users.sshKeys`, `users.addSshKey`, `users.removeSshKey` (keys of another user need `c.Admin`, else `needs_admin`).
- Admin level: `users.create`, `modify`, `setPassword`, `lock`, `unlock`, `delete`, `setAdmin`, `terminateSession`; `groups.create`, `groups.delete`, `groups.addMember`, `groups.removeMember`.
- `users.list` returns `{people, system, uidMin, shadowReadable, lastLoginKnown, adminGroup, shells, self}`; `locked` / `passwordState` are null/unknown without shadow access.
- Frontend `web/src/sections/users/`: `index.tsx` (tabs people/system/groups, cards or table, pref `users.view`), `UserPanel.tsx` (details, groups, keys, sessions, password, delete), `CreateDialog.tsx`, `GroupsView.tsx`.

## Flow
- Read: parse `/etc/passwd`, `/etc/group`, `/etc/login.defs` (UID_MIN, default 1000), `/etc/shells`, optional shadow (`loadSystem`); last login from `lastlog`/`lastlog2` (`lastlog.go`).
- Write (Linux): shadow-utils via `runTool` argv (`useradd`, `usermod`, `userdel`, `gpasswd`, `groupadd`, `groupdel`, `chage`); passwords only via `chpasswd` stdin. Sessions: `loginctl` (`terminate-session`).
- `users.create`: useradd, then chpasswd; on password failure the user is rolled back with `userdel`.
- UI calls `users.list` with `admin: isUnlocked` so the root bridge can read shadow; mutating calls rely on the generic unlock-and-retry.
- SSH keys: authorized_keys edited atomically (0600, `.ssh` 0700) inside `asUser(uid,gid)` so root never follows user-planted links.

## Invariants
- "People" = UID 0 or `uidMin <= UID < 65534` (`isPerson`); system accounts cannot be modified (`person` returns Forbidden).
- Names match `^[a-z_][a-z0-9_-]{0,31}$`; full name excludes `: , = \` and control chars, max 128; password max 256, no line breaks.
- Mutations are serialised by `dbMu`.
- Cannot lock or delete root or the caller (`callerUID`: `SUDO_UID` in root bridge); cannot demote the last admin; cannot drop admin when the admin group is the user's primary group.
- Admin group is the first existing of `wheel`, `sudo`, `admin` (`adminGroups`); granting fails with Conflict if none exists.
- Session ids must match `^[A-Za-z0-9]{1,32}$` before reaching loginctl.

## Gotchas
- Windows (`users_windows.go`, `winaccounts.go`, `winnative_windows.go`): reads use netapi32 (`nativeSnapshot`), writes use PowerShell scripts (`ERR|id|type|msg` on stderr). `users.sessions`, SSH key methods and `terminateSession` return Unavailable; no UID/GID, accounts keyed by `sid`.
- `runTool` classifies "already exists" / "in use" as Conflict; everything else is Internal with the tool's last stderr lines.
- `users.sessions` is User level and lists any user's sessions (loginctl data is world-readable); `terminateSession` is Admin.
- `setAdmin` demotion loops over all admin groups and applies `memberChange` one at a time, so a failure part-way can leave partial membership (and the primary-group check is inside the loop).
- Last-login "never" depends on lastlog tooling being present (`lastLoginKnown`).
- Unix-only tests: `users_unix_test.go`; Windows-mapping tests (`winnative_test.go`, `winaccounts_test.go`) are platform-neutral.

## Checks and references
- `cd server && go test ./internal/modules/users/`.
- `cd web && npm run typecheck && npm run i18n:check` (`web/src/i18n/{en,it}/users.json`).
- Docs: `docs/api/users.md`.

Related: map/server.md, map/web.md

Sweep helpers (2026-11-05): bindGroup, editKeyFile, lockSystem (users.go); bindWinGroup, winLockTarget, winLockSnapshot (users_windows.go); findByName[T] (winaccounts.go).
