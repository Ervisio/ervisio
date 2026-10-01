# Design rules

Approved screens: `docs/design/screens/*.html` (open `docs/design/index.html`). Match them.
Where an early screen still shows grey divider lines or pill-shaped controls, these rules win.

## Language
- English by default, Italian selectable in Settings. Every string through i18n.
- Plain, active copy. Buttons say what happens ("Restart", "Delete user"); toasts echo it
  ("nginx restarted"). Errors say what went wrong and how to fix it.

## Theme tokens (CSS variables)
Default theme **OLED** (lifted panels):

| token | value | use |
|---|---|---|
| `--bg` | `#000000` | page |
| `--surface` | `#0E0E10` | rail, panels, top bar controls |
| `--sunk` | `#18181B` | cards, inputs, rows |
| `--line` | `#26262B` | switch track, rare separators (avoid) |
| `--ink` / `--ink2` / `--ink3` | `#F4F4F6` / `#A3A3AD` / `#6C6C76` | text levels |

Section colours `--h-<id>` and soft fills `--h-<id>-s = color-mix(in srgb, hue 16%, bg)`:
ov `#8B93FF` (Overview), term `#3DDC97` (Terminal), file `#5AB0FF` (Files), log `#FFB547` (Logs),
svc `#FF6B81` (Services), sw `#B98CFF` (Software), usr `#3FD8DE` (Users), plg `#FF7AC6` (Plugins).
`--distro` is the distro colour (Arch `#1793D1`).

Status tokens, separate from section colours: `--ok #3DDC97`, `--warn #FFB547`, `--err #FF6B81`,
`--info #5AB0FF`, each with `-s` soft fill. A failed thing is always `--err`.

Themes: OLED (default), OLED Mono, Midnight, Graphite, Fjord, Forest, High contrast, Daylight,
Linen (values in `.variant-studio/gen/r22_themes.py`). Colour mode: Per section (default),
Distro colour (all section hues = distro colour), Monochrome (greys). Themes flagged
"own colours" (OLED Mono, Fjord, Forest, High contrast) ignore the colour mode. Users can create
themes (editor, import/export JSON). Follow-the-device option pairs a dark and a light theme.

## Type
Figtree (UI, weights 400–800) and JetBrains Mono (paths, code, terminal, config keys).
Title 26/800, section heading 15–17/800, body 14/500, small 12.5. Tabular numbers in tables.

## Shape (components "rounded rectangles")
Radii: card 18, control and input 12, checkbox 6, badge 8, toast 14, menu 14, dialog 20,
icon tile 14, tooltip 8, sheet 26 top corners. Control height 36, input 40.
Focus: 2px accent ring. Primary button = accent fill with black text.

## Structure
- **No grey divider lines anywhere.** Separate with spacing, tinted rows (zebra in tables),
  tinted panels and hover highlights.
- Shell: narrow icon rail (88px) with labels under icons; each item always shows its section
  colour, active item filled. Distro logo on top (from /etc/os-release, Tux fallback). Settings
  gear above the avatar. Installed plugin pages appear under Plugins. Top bar: search, host
  pill, theme toggle, notifications. Content sits in rounded surface panels on the black page.
- Details open in a right side panel with tabs; on phones the panel is a bottom sheet.
- Phones: bottom dock (Overview, Terminal, Files, Services, More); More is a sheet with host
  switcher and every section, plugin page and Settings.
- Dangerous actions: type-to-confirm dialog. Admin actions without unlock: "Administrator rights
  needed" dialog asking the user's password (sudo), unlocked for 5 minutes, chip in the UI.
- Motion: subtle, only on user action; respect prefers-reduced-motion. Edit mode wiggle is the
  one playful exception.

## Section decisions (see design reference for the screens)
Overview: customizable widget grid (stat cards tinted by section, activity chart, action tiles,
alerts, machine info), Personalize mode with handles, resize, drop zone, widget library
(system / personal / from plugins).
Sign in: oversized distro logo with glow, hostname, IP (config `login.show_ip`), distro name;
"Who's signing in?" row of square account tiles (recent in this browser) + Other; password,
stay signed in, root-disabled and wrong-password messages.
Files: places sidebar (Home, Root, Recent, Starred, Trash, bookmarks, SFTP remotes, disk usage),
browser tabs always visible, split view (two panes, each grid/list), Details/Preview panel,
bulk action bar, uploads with transfer queue.
Terminal: sessions sidebar (incl. root, SSH, detached persistent sessions, saved hosts),
toolbar (sudo chip, Split, Palette, Focus), split panes, command palette Ctrl+Shift+P,
hideable snippet chips; mobile extra keys row.
Logs: sources sidebar (All, System, Services, Files, Watchers + watch a file), level chips with
counts, time range, Live, stacked histogram, stream, details with surrounding lines.
Services: state tabs, purpose chips (Web/Containers/System), failed banner with plain hint,
Table/Cards toggle, wide panel (Info, Logs, Unit file via override.conf, Dependencies).
Users: People / System accounts / Groups tabs, square people tiles + table toggle, panel
(reset password, lock, delete; Info with admin switch, Groups switches, SSH keys, Sessions).
Software: tabs Updates (default: count, reboot/security notes, Update all, schedule; grouped
repos / AUR unselected / Flatpak), Find software (store), Installed (table + live transaction
panel), History. Real app icons, letter fallback.
Plugins: tabs Installed (cards + panel), Updates, Browse (consent dialog), Security
(risk summary + capability grid), Developer (load from folder).
Settings: gear in rail; section list with scroll-spy; one searchable page; instant save + Undo
toast; server settings show their config key; admin-only sections marked.
