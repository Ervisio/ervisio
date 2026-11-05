# prefs.* (per-user preferences)

Package `server/internal/modules/prefs`. Stored by the **user bridge** in
`~/.config/ervisio/prefs.json` (dir 0700, file 0600, atomic writes). A flat JSON object;
values are any JSON. Keys match `^[A-Za-z][A-Za-z0-9_.-]{0,63}$`; a value is ≤ 256 KiB and the
whole file ≤ 1 MiB. All methods are user level.

| Method | Params | Result |
|---|---|---|
| `prefs.get` | `{}` | the whole object, e.g. `{"theme":"oled","language":"it"}` (`{}` when none) |
| `prefs.set` | `{"key":"theme","value":"oled"}` | the whole object after the change |
| `prefs.setAll` | `{"values":{"theme":"oled","density":"compact"},"replace":false}` | the whole object after the change |

- A `null` (or missing) value deletes the key.
- `replace:true` replaces the entire object with `values`.
- A corrupt file is moved to `prefs.json.corrupt` and treated as empty.

Suggested keys (owned by the web app): `theme`, `colourMode`, `language`, `density`,
`reduceMotion`, `terminal`, `dashboard`, `files.bookmarks`, `snippets`, `logs.watchers`.

ErvisiOS uses `shell.mode` (`classic` or `ervisios`) and `shell.desktop` (a versioned JSON
object containing desktop shortcuts, virtual folders, pinned apps and wallpaper). These
belong to the signed-in account and follow it between browsers. Virtual folders only
organize app shortcuts; saving them never creates directories on the operating system.
Without `shell.mode`, the console uses `ui.default_shell` from the daemon configuration
(also returned as `defaultShell` by `GET /api/public/host`). The browser preference cache
is scoped by API origin, account UID and account name; an unattributed old cache is ignored.
