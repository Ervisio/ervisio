# Logs
Purpose: search, histogram and live-follow of the systemd journal, log files and Windows Event Log channels, with watched files and error notifications.

## Entry points and public contracts
- `web/src/sections/logs/index.tsx`: page (`PAGE` 400 rows per query, `MAX_ROWS` 20000 kept in live mode). Parts: `FilterBar`, `Histogram`, `Stream` (virtualized), `Details` (+context), `Sources`.
- `helpers.ts`: `parseFilter`/`writeFilter` keep the filter in URL params `src` (repeatable), `unit`, `file`, `level`, `q`, `range`, `from`, `to`, `live=1`; `normalizeWatchers` for pref `logs.watchers`.
- `background.ts` (always on): one `logs.follow` for watchers with `notify`, `levels:['err']`, toast max once per minute per file plus bell entry linking `/logs?file=..&level=err`.
- `server/internal/modules/logs/logs.go` `Register`, all User: `logs.sources`, `logs.query`, `logs.histogram`, `logs.context`, stream `logs.follow` (batches of entries every ~150 ms or 200 entries). `c.Admin` (client `admin:true`) selects root visibility.
- Params: `baseParams` in `query.go` (`sources`, `since`, `until` ms, `levels`, `text`, `watchers`); `queryParams` adds `limit` (default 300, max 2000) and `cursor`. Entry type in `types.go`.
- Docs: `docs/api/logs.md`.

## Flow
- Source ids: `all`, `journal`, `kernel`, `boot`, `unit:<name>`, `file:<abs path>`, `evt:<channel>` (`parseSourceID`). `all` expands to journal + up to 150 files from `/var/log` + watchers (Linux) or the fixed `eventChannels` + watchers (Windows).
- `logs.query`: journal groups, files and event channels fetched in parallel (semaphore 6), merged by `TsUs` desc. Cursor is `<tsUs>.<nSameTs>`; `want = limit+skip+1` yields `hasMore`/`next`.
- Page `lcall`: first as user with `noUnlock`; on `needs_admin` retries with `admin:true` and sticks to admin until the session locks. Live mode does the same inside `onError`.
- Details context: journal entries by cursor (`journalctl --cursor`, before/after), file entries by `file:<path>:<line>` cursor.
- Histogram: default 60 buckets (max 500), default window 24 h, journal capped at 300000 entries (`truncated`).

## Invariants
- A failing journal group fails the whole query; failing files/event channels are reported in `skipped` (unless only one source). UI shows an unlock banner for skipped `needs_admin`.
- Reading the journal as user first checks `journalReadable()`; unreadable returns `needs_admin` before spawning journalctl.
- File paths: absolute, clean (no `..`, `//`), no NUL, max 4096. Unit names validated by `unitRe`. Text filter max 300 chars, case-insensitive substring on message.
- Client `watchers` (even `[]`) overrides the prefs file; only `nil` falls back to `~/<UserDataDir>/<PrefsFile>` key `logs.watchers`.
- File scans read backwards at most 96 MiB per query; line numbers only for files up to 128 MiB (`maxLineCountSize`), larger files get no context.
- Streams: close on cleanup; `follow` with a single failing file ends with an error, with several it keeps going.

## Gotchas
- Platform split: `platform_other.go` / `platform_windows.go` define `onWindows`, `absLogPath`, `pathSeps`. `filescan_unix.go` uses inode for rotation detection; `filescan_windows.go` opens with all share flags (rotation by rename/delete) and uses volume serial + file index.
- Windows has no journal: `journal`, `kernel`, `boot`, `unit:` ids are rejected; `evt:` is rejected on Linux. Event Log goes through `wevtutil qe` (`eventlog.go` is platform-neutral so it is tested on Linux), polled every 2 s in follow. Channel `Security` needs admin (exit code 5 -> `needs_admin`).
- `/logs?unit=` on Windows becomes a text search `q=<unit>` (no unit sources).
- `Sources.tsx` rejects paths not starting with `/` (`watch.errAbsolute`), so watchers cannot be added from the UI on Windows; default watcher name also splits on `/` only.
- `Watcher.keepDays` is stored in prefs and shown in the UI, but no backend or client code enforces it (grep).
- Journal source counts come from a sampled 24 h journal (cap 100000, `approx`), refreshed every 60 s by the page.
- Live mode de-duplicates journal entries by cursor over the latest 600 rows; file entries are never deduped (`file` set).

## Checks and references
- `cd server && go test ./internal/modules/logs/...` (`logs_test.go`, `eventlog_test.go`); `GOOS=windows go vet ./internal/modules/logs/`.
- `cd web && npm run typecheck && npm run i18n:check` (namespace `logs`).
- Related: map/server.md, map/web.md, map/services.md (`services.logs` is a separate journal reader), map/overview.md (log widget calls `logs.query` on Windows).
