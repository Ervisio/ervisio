# Configuration reset

Settings exposes two distinct operations in Classic and ErvisiOS.

## Personal preferences

`prefs.setAll {values: {}, replace: true}` clears the signed-in account's Ervisio preferences. The UI first waits for pending preference writes, then clears its allowlisted browser caches if selected. Server files, connections and plugin data are retained by this personal reset.

## Complete Ervisio configuration and private data

Full reset requires an installed, privileged Linux daemon and an unlocked administrator session. Windows and development instances return `unavailable` without modifying data. Custom configuration, state, notification, environment, tunnel and command-line listen locations are refused rather than silently performing a partial reset.

1. `maintenance.reset.preview {preserveAccess: true|false}` inventories the managed data and returns `{token, expiresAt, paths, preserved, files, bytes, backupRoot, nextURL}`. The single-use preview capability expires after five minutes and belongs to the current session. Configuration contents and credentials are never returned.
2. `maintenance.reset.apply {token, confirm: "RESET ERVISIO"}` starts a detached systemd helper. It returns `{id, state, phase, statusToken, expectedBackupPath, nextURL}`. The helper refuses concurrent self-updates, resets and active package transactions.
3. `maintenance.reset.status {id}` returns privileged progress and diagnostics. The read-only `GET /api/maintenance/reset/status?id=…&token=…` capability also works after the intentional session-invalidating restart, but excludes backup paths and diagnostic errors. The browser keeps this token in its tab's session storage and URL fragment, on a public progress page outside the sign-in guard.

Before stopping Ervisio, the helper creates a complete private archive under `/var/backups/ervisio/factory-resets` (0700 directory, 0600 archive). Configuration, connection credentials, preferences, plugin settings, AI history and Ervisio-owned private state are included. Data is rechecked against the preview before replacement. Symlinks are archived without following them; mount points and unrelated system paths are excluded. Changed, unsupported or unsafe layouts fail closed.

The helper stages the old data, writes defaults, restarts Ervisio and waits for its actual HTTP(S) health endpoint before deleting staged originals. Failure attempts to stop the new daemon, restore the staged data and restart the original configuration. The archive is retained even after success. Administrators can recover it manually; there is no unauthenticated restore endpoint.

Linux/Windows accounts, ordinary server files, services, containers, active swap, mounted data, installed plugin code, update metadata, audit history and existing backups are preserved. A preview lists the exact included and preserved paths. Installed plugins remain installed with their private settings cleared.

`preserveAccess: true` retains listen/TLS, authentication access settings and the selected default interface, including required certificate files. With `false`, Ervisio restores its secure defaults (HTTPS on port 9090, root sign-in disabled); the UI displays the resulting address before confirmation. A non-secret marker prevents automatic import of old LinuxAdmin settings after reset.

Reset actions are audited without recording preview/status tokens, configuration contents or credentials. Existing browser sessions are invalidated by the restart and users must sign in again.
