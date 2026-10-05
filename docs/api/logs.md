# logs.* (Logs section)

Package `server/internal/modules/logs`. Every method is **user level**: the user bridge shows what the
account's groups allow; calls with `admin:true` run in the root bridge and see everything.

If the account cannot read the system journal (not in `systemd-journal`/`adm`/`wheel`), journal
sources answer `needs_admin` ("Reading the system journal needs administrator rights…"), so the client
can offer the unlock dialog and repeat the call with `admin:true`. Unreadable files answer `needs_admin`
(user bridge) or `forbidden` (root bridge). `logs.sources` never fails for this reason: it flags the
sources (`needsAdmin`, `journalReadable:false`).

## Source ids

| id | meaning |
|---|---|
| `all` | journal + every readable file in `/var/log` + the watchers (query/histogram/follow only) |
| `journal` | the whole systemd journal |
| `kernel` | kernel messages (`journalctl -k`) |
| `boot` | the journal of the current boot |
| `unit:<name>` | one unit, e.g. `unit:nginx.service` |
| `file:<abs path>` | a log file; the path must be absolute and clean (no `..`, `//`) |

`watchers` (optional array on every method, `[{path,name,format,notify,keepDays}]`) gives names and formats to
`file:` sources. When it is missing the bridge reads `logs.watchers` from `~/.config/ervisio/prefs.json`.
`format` is `plain` (no level detection), `auto` (default: JSON lines are recognised, other lines by keywords)
or `json`.

## Common filter params

`since`, `until`: milliseconds since the epoch (omit = open ended). `levels`: any of `err`, `warn`,
`info`, `debug` (omit or all four = no filter). `text`: case-insensitive substring of the message.

Level mapping: journal `PRIORITY` 0-3 → `err`, 4 → `warn`, 5-6 → `info`, 7 → `debug`. Files: JSON fields
`level`/`severity`/`lvl`/`levelname` (names or numbers, pino 10-60 or syslog 0-7); other lines by the words
error/err/fatal/crit/alert/emerg/panic, warn(ing), debug (else `info`). Lines without a timestamp (stack
traces) take the time and, if they have no level word, the level of the nearest earlier line that has one.
Recognised timestamps: ISO 8601 (`2026-10-01T20:29:58+0200`, with `T` or space, `,` or `.` fraction),
`2026/10/01 20:29:58`, syslog `Oct  1 20:29:58`, access logs `[01/Oct/2026:20:29:58 +0200]`, JSON fields
`time`/`ts`/`timestamp`/`@timestamp`. Lines with none get the file's modification time.

## logs.sources

Params `{watchers?}` → 
```json
{"groups":[
  {"id":"system","sources":[{"id":"journal","kind":"journal","group":"system","label":"journal","hint":"systemd journal","count24h":79000,"errors24h":1510},
                            {"id":"kernel","kind":"kernel",…},{"id":"boot","kind":"journal",…}]},
  {"id":"services","sources":[{"id":"unit:nginx.service","kind":"unit","label":"nginx","hint":"journal: nginx.service","unit":"nginx.service","count24h":204,"errors24h":3}]},
  {"id":"files","sources":[{"id":"file:/var/log/pacman.log","kind":"file","label":"pacman","path":"/var/log/pacman.log","size":122164,"count24h":16,"errors24h":0},
                          {"id":"file:/var/log/snapper.log","kind":"file","needsAdmin":true,…}]},
  {"id":"watchers","sources":[{"id":"file:/home/me/app.log","kind":"file","label":"app.log","format":"auto","notify":true,…}]}],
 "total24h":79000,"errors24h":1510,"approx":false,"journalReadable":true}
```
Services are the top 30 `.service` units by entries in the last 24 h. Counts come from the newest 100 000
journal entries of the last 24 h (`approx:true` when that cap was hit) and from the last 512 KiB of each file.

## logs.query

Params `{sources[], since?, until?, levels[]?, text?, limit?, cursor?, watchers?}`; `limit` default 300, max 2000;
`cursor` is the `next` of the previous page.

```json
{"entries":[{"ts":1790879867658,"tsUs":1790879867658194,"level":"warn","source":"plasmashell","srcId":"journal",
  "message":"Failed to find service…","unit":"user@1000.service","pid":1530,"cursor":"s=…;i=30f8c;…"},
 {"ts":1790879399000,"tsUs":1790879399000000,"level":"info","source":"pacman","srcId":"file:/var/log/pacman.log",
  "message":"[ALPM] transaction completed","raw":"[2026-10-01T20:29:59+0200] [ALPM] transaction completed",
  "file":"/var/log/pacman.log","line":1622,"cursor":"file:/var/log/pacman.log:1622"}],
 "next":"1790879399000000.2","hasMore":true,
 "skipped":[{"id":"file:/var/log/snapper.log","code":"needs_admin","message":"…"}]}
```
Entries are newest first, merged by time across sources. `raw` is present when `message` has the leading
timestamp removed. `skipped` lists sources that could not be read when several were requested (with a single
source the error is returned instead). `line` is 0 for files over 128 MiB. Search (`text`) matches the message
only. Errors: `invalid` (bad source id, level, path, cursor), `needs_admin`/`forbidden`, `not_found` (file
missing), `unavailable` (journalctl missing or timed out).

## logs.histogram

Params `{sources[], since?, until?, text?, buckets?, watchers?}` (`buckets` default 60, max 500; default range
the last 24 h; levels are always all) →
`{"since":ms,"until":ms,"step":ms,"buckets":[{"t":ms,"err":0,"warn":3,"info":40,"debug":0},…],"truncated":false}`.
Reads at most the newest 300 000 journal entries and 96 MiB of each file (`truncated:true` when the journal cap
was hit).

## logs.follow (stream)

Params `{sources[], levels[]?, text?, watchers?}`. Sends **arrays** of entries (same shape as `logs.query`,
oldest first within a batch) every ~150 ms. Journal via `journalctl -f -n 0 -o json`; files by polling every
400 ms from the current end, handling rotation (new inode) and truncation (reads the new file from the start).
File entries of a follow have `line:0`. Ends with an error if the journal is unreadable (`needs_admin`) or a single
followed file cannot be opened.

## logs.context

Params `{source?, cursor?, file?, line?, before?, after?, watchers?}` (before/after default 5, max 200).
Give a journal `cursor` (optionally with `source` = `unit:…`/`kernel` to restrict to it), or `file`+`line`, or the
`cursor` of a file entry (`file:<path>:<line>`).
→ `{"entries":[… oldest first …],"index":4}` where `index` is the requested entry. Files over 128 MiB answer
`unavailable`; a line beyond the end answers `not_found` (the file was probably rotated).

## Windows (Windows Event Log backend)

On Windows the journal, `kernel`, `boot` and `unit:` sources do not exist (they answer `invalid`); the
system group lists Windows Event Log channels instead, as sources `evt:<channel>` with `kind:"evt"`:
`evt:Application`, `evt:System`, `evt:Security`, `evt:Setup` and `evt:Microsoft-Windows-PowerShell/Operational`.
`all` expands to those channels plus the watchers. Log files (watchers) take absolute Windows paths.

Entries are read with `wevtutil qe <channel> /rd:true /c:<n> /f:RenderedXml /q:<XPath>`; time range and levels
are pushed down into the XPath, text is filtered server side. Level mapping: 1,2 → `err`, 3 → `warn`,
0,4 → `info`, 5 → `debug`. `source` and `unit` are the provider name, `pid` the execution process id, `cursor`
is `<channel>/<EventRecordID>`, `message` the rendered message (event data when none is available).
Reading `Security` without administrator rights gives `needs_admin` (source flag `needsAdmin`). `logs.follow`
polls every ~2 s for records with a higher `EventRecordID`. `logs.context` supports files only. Untested on a
real Windows host.
