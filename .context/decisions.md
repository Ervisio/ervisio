# Decisions (date · decision · why)
2026-11-06 · Keep ErvisiOS window routes in memory while the outer browser stays at the base URL with title Ervisio · User wants one desktop identity; Classic keeps normal deep links and initial desktop deep links are consumed once.
2026-11-06 · Store real-directory desktop shortcuts as per-user links, separate from virtual app folders · Opening follows the existing authenticated file manager; organizing or removing a link never mutates the real directory.
2026-11-06 · Stream Photos & Videos through authenticated, bounded byte windows with native browser decoding · Enable efficient local/SFTP/SMB video seeking without whole-file buffers or server transcoding; remote media owns its cancellable connection.
2026-11-06 · Install managed Node/AI CLIs only in the signed-in account's private prefix · Preserve system runtimes and unrelated launchers; use official archives/checksums and npm packages, with normal package managers for backend utilities.
2026-11-06 · Implement optional Backup UI as a signed plugin and reuse daemon jobs with an account-owned worker · Keep secrets out of job arguments and plugin output, and constrain remote retention to authenticated plan-owned artifacts after a successful backup.
2026-11-06 · Full factory reset is a session-bound preview, explicit typed confirmation and detached Linux helper with private backup and health-checked rollback · User authorized complete Ervisio configuration, connections, keys and plugin data reset while host accounts/files stay outside its scope; custom unmanaged layouts fail closed.
2026-11-06 · Format only re-inspected local data volumes with typed device confirmation · Keep system/swap/managed volumes protected; Linux quick/full paths are verified using an owned disposable loop image rather than user disks.
2026-11-05 · Share Notepad, lazy CodeMirror and remote file connections between Classic and ErvisiOS · User explicitly requires both interfaces to retain feature parity; desktop-only chrome/grid remain shell features.
2026-11-05 · SFTP/SMB connections use the existing per-account remote bridge store, not host OS mounts · User wants simple saved network locations; keep remote credentials sealed server-side and preserve the account's permissions.
2026-11-05 · Editor drafts stay in signed-in session memory across navigation/shell changes · Prevent accidental loss without persisting file contents or secrets in browser storage or user preferences.
2026-11-05 · Add ErvisiOS as an optional shell, keep classic and the same backend boundaries · User requested concept.html desktop, real section windows, automatic plugin apps and a per-user switch.
2026-11-05 · Virtual desktop folders live only in shell.desktop user preferences · They organize app shortcuts; real files remain in the existing file manager and transfer APIs.
2026-11-05 · Plugin contract v4 adds optional native page metadata and an AI-only local intent bridge · Legacy page layouts remain usable inside windows; native views share the same capabilities and opaque-origin sandbox.
2026-11-04 · Record existing architecture, not new product decisions · Initialization indexes current contracts without changing application behavior.
2026-11-04 · Keep daemon and user/admin bridges separate · System operations belong behind the session and privilege boundary; see docs/ARCHITECTURE.md and map/server.md.
2026-11-04 · Preserve approved screens and shared UI/theme primitives · docs/DESIGN-RULES.md governs visual work rather than independent section styling.
2026-11-04 · Route visible frontend text through i18n · English is default and Italian is selectable; section changes must preserve localization.
2026-11-04 · Treat plugins as separate repositories and local plugins/ as development input · The current architecture describes signed marketplace distribution; the older Docker plan is historical.
2026-11-04 · Keep shared context in git and personal checkpoints ignored; keeper remains off · Maps and decisions are durable project knowledge, while now.md is session state.
2026-11-05 · Go lint with golangci-lint (config in server/) · sweep: no linter beyond gofmt/vet; approved by user.
2026-11-05 · Web formatting with Prettier alongside ESLint, single quotes + semicolons · sweep: 79.9% single quotes, 73.3% semicolons already; approved by user.
2026-11-05 · Go exported PascalCase and React components PascalCase are not naming findings · sweep analyzer misfires on them.
