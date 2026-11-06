# Backup
Purpose: official optional Backup plugin and Linux account-owned worker; existing jobs scheduler, no second daemon.

Entry: `server/internal/backup/cli.go:RunCLI(args)`, called by `cmd/ervisiod/main.go --backup` before daemon startup. JSON ok/result/error, hex-only IDs/tokens. Safe methods status/list/remotes/save/browse/test/run/tick/history/delete. UI in sibling Ervisio/plugin-backup, SDK v4 Classic/native views, minCore7.0.3.

`types.go` validates bounded plans (20/account, 32 folders, 16 DBs), safe DB metadata, SFTP/SMB IDs and absolute paths. `store.go` private0700/0600 inbox/config, AES-GCM passwords/ownership, secret-free views. `schedule.go` monthly due-boundaries retain anchorDay and clamp months; UI dailySDKtick+manualSDKrun separate reusable instances.

`archive.go` tar/gzip snapshot, fixed budget/free reserve, 200k entries, changing files abort, no links/specials, private-store/spool autoexclude. `platform_linux.go` same-root inode checks and dirfd Openat O_NOFOLLOW for every component; serial account-wide flock with cancellation. `database.go` installed pg_dump/mysqldump/mariadb-dump via argv/privatepasswordfiles; SQLite Python stdlib onlinebackup, no shell. No directDockerexec; publishedTCP databases supported.

`retention.go` dedicated plan namespace, privateSFTPpermissions, exclusive temp upload/nonreplace rename, outerHMACmanifest+SHA256, keep>=1 and newarchive, never deletes unrelatedprefix/foreignmetadata. `journal.go` durableHMACexactintent before remoteCreate; bounded10s recovery onlymatching currentconnection/destination, realdirchain/marker/type/size; ignoresold/changedtargets. `spool.go` HMACownedlocalspool recovery afterworker killed, never generic directory cleanup.

`server/internal/modules/remote/backup.go` only minimal exported bridge: safeViews + dedicated cancellable transport, same saved per-user sealed remote credentials/pinnedSFTPkey. BackupFS creates SFTP0700dirs/0600files before data; SMBACLservercontrolled. No existing remote API or pool privilege changes.

Tests: backup unit+loopbackSFTP, fakeSMB, privateinbox/secretstore, source/races/quota/calendar, journal9+subcases; Linux SQLite/fakeDB. Opt-in disposable actual PostgreSQL/MariaDB dump+restore CI env ERVISIO_BACKUP_TEST_DATABASES=1 fixedloopback15432/13306, neverproduction. Docs `docs/api/backup.md`; plugin own npmtest/build/pack. Root owns signing/publishing/deploy; no backup of actual VPS data tested.
