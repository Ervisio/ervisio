# prefs.* (per-user preferences)

Package `server/internal/modules/prefs`. Stored by the **user bridge** in
`~/.config/linuxadmin/prefs.json` (dir 0700, file 0600, atomic writes). A flat JSON object;
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
