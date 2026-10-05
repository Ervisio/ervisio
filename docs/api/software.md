# software.* (Software)

Package `server/internal/modules/software`. Sizes are bytes, times are unix **milliseconds** unless
noted (`installDate` is unix seconds). A `Backend` abstracts one package manager
(`Name, Kind, Available, ListInstalled, ListUpdates, Search, Info, Install, Remove, Upgrade, Refresh`);
install/remove/upgrade return a *plan* (argv steps plus a progress parser) that the transaction runner
executes in the root bridge.

## Backends and detection

| Backend | Detected by | Notes |
|---|---|---|
| `pacman` | `pacman` in PATH | system packages (`kind: "repo"`, `source` = core, extra, ...) |
| AUR helper | `yay`, else `paru` (user bridge only, never root) | `kind: "aur"`: listing, search, info. **Installing and upgrading AUR packages is not supported** (see below). Removing an AUR package is plain `pacman -Rs`. |
| `apt` | `apt-get` + `dpkg-query` | Debian/Ubuntu. Upgrade all = `apt-get update` + `apt-get dist-upgrade`. |
| `dnf` | `dnf` + `rpm` | Fedora (dnf4 and dnf5 output). |
| `zypper` | `zypper` + `rpm` | openSUSE. |
| `winget` | `winget` in PATH (Windows) | system manager on Windows (`kind: "repo"`, `name` = winget Id such as `Git.Git`, `title` = display name, `source` = winget/msstore). Listing and updates parse `winget list` / `winget upgrade` tables (positional columns, spinner and progress characters stripped, wide characters measured in cells); search is `winget search --query`. Install, uninstall and upgrade run `winget ... --id X -e --silent --accept-package-agreements --accept-source-agreements --disable-interactivity` (one step per package; upgrade-all is `winget upgrade --all`). `software.history` is always empty, scheduled updates answer `unavailable` (systemd only), and AUR/Flatpak do not apply. Parser: `winget_parse.go`. Not exercised on a real Windows host yet. Pending Windows Update (OS) updates are not reported (a possible later addition: PowerShell `Microsoft.Update.Session` `Search("IsInstalled=0")`). |
| `flatpak` | `flatpak` | `kind: "flatpak"`, system and user installations (`scope`). |

One system manager is used (first found in the order pacman, apt, dnf, zypper); AUR and Flatpak are added
when present. `kind` is `repo | aur | flatpak`; the web client routes by it.

AUR limitation: AUR builds must run as the unprivileged user and need an interactive `sudo` for the final
install step. The root bridge cannot provide either, so `software.transaction` answers `unavailable`
(with the terminal command to run) for AUR install/upgrade. The web UI shows AUR updates unselected with a
PKGBUILD warning and an "Open in terminal" button.

Caching: updates, installed, apps and suggestions are cached 10 minutes per bridge process; a transaction
or `software.check` / `force: true` refreshes them.

## Updates without root

On Arch, `software.check` copies the sync databases to a private directory
(`/var/cache/ervisio/checkdb` for the root bridge, `$XDG_CACHE_HOME/ervisio/checkdb` or
`~/.cache/ervisio/checkdb` for a user; never a shared folder such as `/tmp`). The folder must be a real 0700
directory of the bridge's user in a parent only that user or root can write; otherwise it is not used (the check
fails, and `pacman -Qu` reads the system databases). Copies are created with `O_EXCL|O_NOFOLLOW`. It syncs them there with `fakeroot pacman -Sy --disable-sandbox`
(plain `pacman -Sy` when the bridge is root) like `checkupdates`, and reads `pacman -Qu` / `pacman -Sup`
against it. The system databases are never partially synced. Without `fakeroot` it reports the state of the
last real sync and a warning. The private copy is deleted after a transaction.

## Methods

### `software.summary` (user) → Summary
```json
{"manager":"pacman","sources":["repo","aur","flatpak"],"aurHelper":"yay","aurSupported":false,
 "updates":62,"counts":{"repo":58,"aur":4,"flatpak":0},"downloadSize":546000000,
 "rebootNeeded":true,"rebootPending":false,"security":0,"lastCheck":1790880047662,
 "schedule":{"at":"03:00"},"busy":false,"warnings":[]}
```
`rebootNeeded`: a pending update is a kernel, systemd or glibc (`linux*`, `kernel*`, `linux-image-*`,
`systemd`, `libc6`, `glibc`). `rebootPending`: the running kernel's modules are gone, or
`/var/run/reboot-required` exists. `security`: updates with the `security` note. `lastCheck` 0 = never.
`schedule` is `null` when no timer is set. `downloadSize` excludes AUR. `warnings` carries partial
failures (for example Flatpak offline).

### `software.check` (user) → Summary
Refreshes the package metadata as far as the caller's rights allow (see above), recomputes the updates
(bypassing the cache) and returns the summary. Failures to refresh become `warnings`.

### `software.updates` {force?: bool} (user) → Update[]
```json
[{"name":"linux","from":"7.2.6.arch2-1","to":"7.2.8.arch1-1","source":"core","kind":"repo","size":161200000,
  "notes":["reboot"]},
 {"name":"docker","from":"1:29.8.1-1","to":"1:29.8.2-1","source":"extra","kind":"repo","size":29900000,
  "notes":["restartService:docker.service"]},
 {"name":"com.spotify.Client","title":"Spotify","from":"1.2.95","to":"1.2.96","source":"flatpak","kind":"flatpak",
  "size":182000000,"notes":[],"scope":"system"}]
```
`notes`: `reboot`, `security` (arch-audit when installed, `-security` origins on apt, `dnf check-update --security`),
`restartService:<unit>` (an active systemd service shipped by the package, pacman only). `from` is empty
for new dependencies pulled in by the upgrade. Sorted repo, aur, flatpak, then by name.

### `software.installed` {filter?, force?} (user) → Package[]
`filter`: `all` (default) `explicit` `deps` `orphans` `aur` `flatpak`; anything else is `invalid`.
```json
{"name":"docker","version":"1:29.8.1-1","source":"extra","kind":"repo","size":108000000,
 "reason":"explicit","installDate":1790252092,"description":"…","orphan":false}
```
pacman reads `/var/lib/pacman/local/*/desc` and `pacman -Sl` (repo of each package; packages in no sync
repo are `aur`) and `pacman -Qdtq` (orphans). Flatpak entries are apps only and carry `title` and
`scope` (`system`|`user`).

### `software.info` {name, source} (user) → Detail
`source` is a repo name, `aur` or `flatpak` (anything else selects the system manager).
```json
{"name":"docker","source":"extra","kind":"repo","installed":true,"version":"1:29.8.1-1",
 "description":"…","fields":[{"key":"URL","value":"https://www.docker.com/"},…]}
```
`fields` are the manager's own key/value lines, in order. Errors: `invalid` (bad name), `not_found`,
`unavailable` (no such backend).

### `software.search` {query} (user) → SearchResult[]
Searches the system repos (`pacman -Ss` with each word literal), the AUR (`yay -Ss --aur`) and Flatpak
remotes in parallel. Queries shorter than 2 characters return `[]`. Max 40 repo, 20 AUR, 24 Flatpak
results; exact and prefix matches first, translation/help/doc packages last.
```json
{"name":"gimp","version":"3.2.6-2","source":"extra","kind":"repo","description":"…","installed":false}
{"name":"org.gimp.GIMP","title":"GIMP","version":"3.0.4","source":"flatpak","kind":"flatpak","remote":"flathub",…}
```

### `software.suggest` {category?} (user) → SearchResult[]
Curated well-known software resolved against this system's repos and Flatpak remotes (missing names are
skipped). `category`: empty (popular), `development`, `internet`, `graphics`, `media`, `system`, `servers`.

### `software.apps` (user) → App[]
Desktop apps from `.desktop` files (`/usr/share/applications`, `/usr/local/share/applications`, Flatpak
exports, user dirs); hidden/`NoDisplay` entries skipped.
```json
{"id":"firefox","name":"Firefox","comment":"…","icon":"firefox","exec":"firefox %u","categories":["Network"],
 "package":"firefox","source":"extra","kind":"repo","version":"156.0.1-1","explicit":true}
```
`package` is resolved with `pacman -Qo` / `dpkg -S` / `rpm -qf` (Flatpak: the app id). `explicit` is true when
installed by the user (not as a dependency).

### `software.icon` {name, size?} (user) → {mime, data}
`name` is the `Icon=` value of a desktop file: a theme icon name or an absolute path (only under
`/usr/share`, `/usr/local/share`, `/opt`, `/var/lib/flatpak`, `/usr/lib`, `~/.local/share`, `~/.icons`).
Looks in hicolor, Adwaita, breeze (and others) at the size nearest to `size` (default 64), PNG or SVG,
Flatpak exports, then `/usr/share/pixmaps`. `data` is base64, `mime` is `image/png` or `image/svg+xml`.
Files over 1.5 MiB are refused. Errors: `not_found` (the client shows a letter tile), `invalid`.

### `software.history` {limit?} (user) → HistoryEntry[]
Newest first (default 300, max 3000). Sources: `/var/log/pacman.log`, `/var/log/apt/history.log`,
`/var/log/zypp/history`, `/var/log/dnf.rpm.log` (dnf4). Flatpak history is not included.
```json
{"time":1790879399000,"action":"upgraded","name":"linux","from":"7.2.6.arch2-1","to":"7.2.7.arch1-1","tx":46}
```
`action`: `installed upgraded removed downgraded reinstalled`; entries of one transaction share `tx`.

### `software.status` (user) → {busy, transaction}
`busy` is true while a transaction runs (or the pacman database lock exists). `transaction` is the running or
last finished one (kept 1 hour):
`{running, pid, op, source, packages, startedAt, finishedAt?, done, total, current, step, steps, ok, message?,
hint?, rebootNeeded, log[last 300 lines]}`. The root bridge writes it to `/run/ervisio/software-transaction.json`
(0600, root only) and a summary with an empty `log` to `/run/ervisio/software-transaction.public.json` (0644);
user-scope Flatpak runs write `$XDG_RUNTIME_DIR/ervisio-software-<uid>.json` (no file when `XDG_RUNTIME_DIR` is
unset or not a private folder). Files are written to a fresh `O_EXCL` name and renamed into place. Readers open them
with `O_NOFOLLOW` and ignore anything that is not a regular file of the expected owner (root, or the user). The user
bridge therefore shows a root transaction's progress without its log; the root bridge returns the full log.
A page reload uses it to re-attach to a running transaction.

### `software.transaction` {op, packages, source, scope?} (admin, **stream**)
`op`: `upgrade | install | remove`. `packages`: names (validated: letters, digits and `@._+:~-`, never
starting with `-`). `source`: `repo` (system manager, default; a repo name works too), `flatpak`, `aur`
(remove only) or `all` (upgrade only: system manager + system Flatpak). `scope` (flatpak): `system` (default) or
`user`; user scope is refused here (use `software.transactionUser`). An empty `packages` with `upgrade`
upgrades everything (`pacman -Syu --noconfirm`, `apt-get dist-upgrade`, `dnf upgrade --refresh`,
`zypper update`, `flatpak update`). Flatpak install references may be `remote/app.id`.
Single-package updates on Arch run `pacman -S` (a partial upgrade); the web UI warns before doing that.

Events (each a `data` message):
```json
{"type":"start","op":"upgrade","source":"all","packages":[],"steps":2}
{"type":"step","index":1,"title":"upgrade","command":"pacman -Syu --noconfirm"}
{"type":"log","line":"(4/18) upgrading linux"}
{"type":"progress","done":3,"total":18,"current":"linux"}
{"type":"done","ok":true,"message":"","hint":"","rebootNeeded":true}
```
Progress is parsed from pacman (`Packages (N)`, `(n/N) upgrading x`), apt (`N upgraded…`, `Setting up`),
dnf4/dnf5, zypper and flatpak output; `done` counts finished packages. A failed step stops the
transaction: `done` has `ok:false`, `message` (the error lines) and `hint`: `locked | outdated | conflict |
signature | network | ""`. Output is stdout+stderr, ANSI stripped.

Only one transaction runs at a time per bridge (`conflict` otherwise) and `conflict` is also returned when
`/var/lib/pacman/db.lck` exists. The command is **not** killed when the client disconnects (killing a package
manager half way breaks the system); the stream just ends and the state file keeps the progress. The root
bridge must stay alive meanwhile (it is stopped after the admin idle timeout without admin calls).

Errors before the stream starts: `needs_admin`, `invalid` (op, names, empty selection), `unavailable` (no
manager, AUR install/upgrade), `conflict` (busy).

### `software.transactionUser` {op, packages, source:"flatpak", scope:"user"} (user, **stream**)
Same events, for Flatpak apps in the user's own installation (no administrator rights). Anything else answers
`needs_admin`.

### `software.schedule` {at: "03:00" | null} (admin) → {at}
Creates (or, with `null`, removes) the systemd timer pair
`/etc/systemd/system/ervisio-update.timer` (`OnCalendar=*-*-* HH:MM:00`, `Persistent=true`) and
`ervisio-update.service` (oneshot: the same commands as "Update all"; Flatpak steps are prefixed `-` so
their failure does not fail the run), then `daemon-reload` and `enable --now`. `at` is 24-hour server time
(`invalid` otherwise). The scheduled time shows up as `schedule.at` in `software.summary`. To inspect:
`systemctl list-timers ervisio-update.timer`, `journalctl -u ervisio-update.service`.
