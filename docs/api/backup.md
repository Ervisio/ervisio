# Backup worker and official plugin

The optional [Backup plugin](https://github.com/Ervisio/plugin-backup) requires
Ervisio **7.0.3 or later** and currently runs on Linux. Its Classic and ErvisiOS
pages configure the same account-owned plans. The existing plugin job manager
executes them after sign-out and across daemon restarts; no second daemon,
browser timer or cron service is installed.

## Plans and scheduling

A plan contains one or more folders, one or more databases, or both, and a saved
SFTP/SMB connection plus an absolute remote parent folder. Create and explicitly
trust connections in Files first. The plugin can browse their remote directories
without receiving the saved password or SSH private key.

Frequency accepts minutes, hours, days or calendar months. Minute/hour/day
intervals use the job manager's real second intervals. Monthly plans use its
daily wall-clock schedule and a persisted due date: the worker runs only when
due, retaining the original day and clamping shorter months. A January 31 anchor
therefore advances through February 28 and March 31, including after a delayed
March 1 catch-up. The server's clock is used, not the browser's time zone.

Each plan has a scheduled `backup` job (`tick`) and, after its first manual run,
one reusable unscheduled `backup-once` job (`run`). Manual execution bypasses the
due check without postponing a future scheduled backup. There are at most 20
plans per account, leaving room within the existing 50 job instances per plugin.
The worker serializes backups of one account; cancellation also cancels waiting
workers. Removing a plan removes only its own schedules and local configuration,
preserving all completed remote archives.

## Sources and database clients

- Folder paths are absolute and readable by the worker's account. There is no
  implicit administrator elevation. Exclusions accept relative patterns such as
  `node_modules/**`, `.git/**` or `*.log`. Symbolic links and special files are
  skipped and counted. Permission failures and files changing size/mtime abort
  the run. A live folder is not an application-consistent filesystem snapshot.
- PostgreSQL uses an installed `pg_dump` and its custom archive format. The
  password is supplied through a private `PGPASSFILE`, never a command argument.
- MySQL/MariaDB uses installed `mysqldump` or `mariadb-dump` with a private client
  option file, `--single-transaction` and streaming rows. The transaction guarantee
  applies to transactional tables; coordinate writers for nontransactional data.
- SQLite uses Python 3's standard-library online backup API and integrity check,
  including committed WAL data. A live SQLite file is never copied as an ordinary
  file and presented as a consistent database dump.

The page reports actual client availability. Install `postgresql-client`,
`mysql-client`/`mariadb-client` or `python3` in Software as needed. No packages are
silently installed. Docker databases can be reached through their published TCP
port and database credentials; direct `docker exec` dumps are not provided.

The private work directory is excluded even if a source is its parent or `/`.
Linux traversal opens every component through directory descriptors with
`O_NOFOLLOW`, checks the source root identity and never follows a replaced
ancestor into the private store. Defaults limit source data and spool to 2 GiB;
plans can select 1 MiB–100 GiB. The actual spool budget is further limited by
available space, keeping 64 MiB free. Dumps and the compressed archive share that
budget. One run lasts at most six hours and at most 200,000 entries are visited.

## Publication, ownership and retention

Files live below the selected parent in `ervisio-backup-<plan-id>/`. SFTP creates
that namespace with mode 0700 and files with 0600 before writing their data.
SMB follows the server's share ACLs; the helper does not claim Unix permissions
for SMB. Transport protection does not encrypt the archive's database contents
at rest.

The worker builds a gzip/tar archive with `folders/`, `databases/` and
`ERVISIO-BACKUP.json`. It calculates SHA-256 locally, uploads to a random exclusive
`.part` file, closes it, then renames without replacing an existing archive. A
sidecar `.manifest.json` records the run, size, hash and an HMAC authenticated with
this plan's private ownership key. Retention begins only after both files commit.

Cleanup considers only strict plan/run archive names with matching authenticated
manifests and regular-file sizes. It ignores unrelated names, forged metadata,
links and foreign plans, and always retains at least one successful backup as
well as the newly published archive. Keep-count and optional age use minutes,
hours, days or calendar months. Failed runs never trigger retention; cleanup
refusals are reported as warnings without discarding the new archive.

Before creating each remote partial file, a private authenticated intent is
persisted locally. A later run can recover only those exact recorded paths after
checking the same connection, destination, directory chain, marker, type and
bounded size. It never sweeps by prefix. Changed destinations and intents older
than 90 days are preserved for manual review. Recovery is bounded to ten seconds;
1,000 pending intents is a hard limit. Authenticated stale local spools can also
be removed after a killed worker, while the account-wide run lock is held.

Restoration is manual. Verify an archive against its sidecar SHA-256 before
extracting it into a chosen restore directory; use `pg_restore`, a MySQL client
or SQLite as appropriate. The plugin does not overwrite running application data.

## Private CLI contract

`ervisiod --backup <method> [id]` dispatches before server startup and returns one
JSON object: `{ "ok": true, "result": ... }` or
`{ "ok": false, "error": { "code": "backup_failed", "message": "..." } }`.
Exit code is 0 or 1. It handles SIGINT/SIGTERM with context cancellation and
never prints raw database-client stderr, passwords or private keys.

| Method | Argument | Result |
|---|---|---|
| `status` | none | platform/support, OS account, client availability and spool limits |
| `list` | none | `{plans}` with secret-presence booleans only |
| `remotes` | none | `{connections}` containing only safe saved SFTP/SMB views |
| `save` | 32-hex inbox token | validated, persisted public plan view |
| `browse` | 32-hex inbox token | up to 500 real child directories |
| `test` | 32-hex plan ID | real connection/folder check; `writeTested:false` |
| `run` / `tick` | 32-hex plan ID | completed receipt or a monthly `skipped` result |
| `history` | 32-hex plan ID | the latest 20 local receipts |
| `delete` | 32-hex plan ID | local removal; `remoteBackupsPreserved:true` |

Settings live under `~/.config/ervisio/plugins/backup/`, with 0700 directories and
0600 regular files. The only plugin file capability is `inbox/`: a random,
one-shot JSON request, bounded to 256 KiB, is read without following links and
removed before processing. No request JSON or password is placed in argv. Saved
database passwords and ownership keys are AES-GCM sealed using `backup.key`
outside that capability. Blank password edits preserve the old secret; an
explicit `clearPassword` removes it. The account and root can access their own
private state; sealing does not defend against the same account reading its key.

## Verification

`go test ./internal/backup ./internal/modules/remote` covers real ephemeral
loopback SSH/SFTP, mocked SMB filesystem operations, safe retention/upload
failures, encrypted settings, private request boundaries, quotas, calendar
anchors, symlink races and spool exclusion. Linux tests also exercise real SQLite
online snapshots and fake PostgreSQL/MySQL processes to check argv/error secrecy.

CI additionally opts into `TestDisposableDatabaseDumpAndRestore` with
`ERVISIO_BACKUP_TEST_DATABASES=1`: disposable PostgreSQL/MariaDB containers on
127.0.0.1 ports 15432/13306, fixture credentials and database
`ervisio_backup_fixture`. It creates uniquely named fixture tables, dumps,
restores and queries their data. It never uses saved database connections or a
production database. No live VPS backup/retention run is part of these tests.
