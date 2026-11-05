# Shell, UI kit, theme, i18n and lib
Purpose: the app frame (rail, top bar, dock, palette, notices), shared UI components and tokens, theming, translations and formatting helpers.

ErvisiOS adds `web/src/desktop/`: virtual desktop preferences (`shell.desktop`), real section/plugin windows, native Terminal and host connectivity. AppShell selects `prefs['shell.mode'] ?? host.defaultShell ?? 'classic'` after a successful per-user preference load; load failures expose Retry. Classic remains the existing shell. See `docs/ERVISIOS.md`.

## Entry points and public contracts
- `web/src/shell/AppShell.tsx`: `AppShell` = `Rail` + `TopBar` + `<Outlet>` in Suspense + `Dock` + `CommandPalette` + `UpdateNotifier`; Ctrl/Cmd+K toggles the palette; sets `document.title` from the section or plugin page; mounts `useSectionBackgroundHooks`.
- `shell/useNav.ts`: `useNav()` -> `{main, settings, plugins}` `NavEntry[]`; the single source for Rail, Dock, MoreSheet and the palette (SECTIONS filtered by `rail` and `platforms`, plus plugin `railPages`, badges from `registry` + `badge.ts` hooks).
- `shell/Rail.tsx` (`RailItem`, avatar menu with lock/unlock, Settings, sign out), `TopBar.tsx` (search pill, host pill, theme toggle, sudo chip, bell), `Dock.tsx` (`DOCK_IDS` overview/terminal/files/services + `MoreSheet`), `Popover.tsx`, `DistroLogo.tsx`, `NavIcon.tsx`.
- `shell/CommandPalette.tsx`: rows "go" (nav entries) + "act" (`useAllPaletteActions` from section `palette.ts`); AND-match of words over `hay`.
- `shell/notifications.ts`: `notify(NoticeInput)` (max 50, same `key` replaces, in-memory per tab), `useNotices`, `markAllRead`, `clearNotices`. It does not toast.
- `shell/focus.ts`: `useFocusMode(id, on)` hides rail/top bar/dock; one owner at a time.
- `shell/UnlockHost.tsx`: registers the unlock requester (`api/unlock.ts`); ssh-key sessions first try `unlock('')` (NOPASSWD sudo).
- `web/src/ui/index.ts` is the kit barrel: Icon/registerIcon, Button/IconButton, Form (Field, Input, Textarea, Select, Switch, Checkbox, Radio, Segmented), Display (Badge, Chip, Kbd, Page, Progress, Skeleton, EmptyState, Card, StatCard, hueClass), Tabs, Overlay (Dialog, ConfirmDialog, UnlockDialog, Sheet, Panel), Toast (`toast.ok/err/info/undo/update/dismiss`, `ToastViewport`), Menu (Menu, DropdownMenu, useContextMenu, Tooltip), Table, Charts, `useIsMobile/useMediaQuery/useFocusTrap`. Dev showcase: `pages/Kit` at `/__kit`.
- `web/src/theme`: `ThemeProvider`/`useTheme` (theme, colourMode, follow, density, reduceMotion, preview, custom themes import/export, snapshot/restore), `themes.ts` (`BUILTIN_THEMES`, `HUE_KEYS`), `engine.ts` (`themeVars`, `applyVars`, `parseTheme`).
- `web/src/i18n/index.tsx`: `I18nProvider`, `useI18n`, `useT(ns)`, `LANGUAGES`, `NAMESPACES`; dictionaries `i18n/{en,it}/<ns>.json`.
- `web/src/lib`: `format.ts` (`formatTime/Date/DateTime/Bytes/Duration/Clock/Percent`, `relativeTime`, `getRegion`), `refresh.ts` (`useRefreshInterval`, `usePolling`, `refreshNow`), `RefreshSelect.tsx`, `storageMigration.ts` (`migrateStorage`).

## Flow
- Prefs -> `ThemeProvider` -> `themeVars` -> CSS variables on `<html>` (`applyVars`), also cached in `localStorage` `ervisio.themeVars`; `public/theme-boot.js` repaints from that cache before React boots (strict CSP, so external file).
- Follow-device: `prefers-color-scheme` picks `follow.dark/light`; `preview()` overrides for hover previews in Settings.
- Language: `prefs.language` else `localStorage ervisio.lang`; provider loads base namespaces (common, shell, auth, ui) for lang and en before rendering children; other namespaces load lazily in `useT(ns)`.
- Lookup: `{lang}/{ns}` then `en`; plural keys `key_one` / `key_other` chosen when `vars.count` is a number; `{name}` interpolation; missing key returns the key (or '' while en not loaded).
- `I18nProvider` also calls `setRegion` (lang, 12/24 h, week start) during render, so `lib/format` follows prefs; `regionKey` forces consumers to re-render.
- `usePolling` ticks only while the tab is visible, on visibility return, and on `refreshNow()`.

## Invariants
- Breakpoint 860px: `useIsMobile` is `max-width: 859px`, same as `shell.css`; below it the rail is replaced by the dock and Panel becomes a bottom sheet.
- Add a section/nav item through `SECTIONS`; never hardcode in Rail/Dock. Dock ids are fixed, everything else sits in MoreSheet.
- Every visible string uses `useT`; en and it files must have identical key sets per namespace (`npm run i18n:check`). Add new namespaces to `NAMESPACES` and both folders.
- Colours come from tokens only (defaults in `web/src/styles/tokens.css`): `--bg --surface --sunk --line --ink/2/3`, `--h-<ov|term|file|log|svc|sw|usr|plg>` (+`-s`), `--acc --on-acc`, `--ok/warn/err/info` (+`-s`), `--distro`; no hardcoded hex in components.
- Overlays use `useFocusTrap` (`data-autofocus` picks the first focus, Escape closes, focus restored) and `useScrollLock`.
- `ConfirmDialog` takes `confirmText` for type-to-confirm (design requires it for dangerous actions); toasts: 4 s, errors 8 s, undo 7 s, max 5 visible.
- `migrateStorage` runs before anything reads storage (`la.*` -> `ervisio.*`, once, never throws); wrap new storage access in try/catch.

## Gotchas
- DESIGN-RULES: no grey divider lines (use spacing, tints, zebra), radii card 18/control 12/dialog 20/toast 14, control height 36, input 40, focus ring 2px accent, primary button black text on accent, rail 88px, Figtree + JetBrains Mono, motion only on user action and reduced-motion respected (`data-reduce-motion`).
- Themes with `own: true` (OLED Mono, Fjord, Forest, High contrast) ignore colour mode; `mono`/`distro` mode collapses all hues, and `--on-acc` is computed from a hex, so distro `color-mix` values are approximated.
- Custom theme ids use `Date.now()`; `parseTheme` needs all 7 base colours and 8 hues as `#rrggbb`, else returns null.
- A failed namespace load is cached as an empty dictionary for the page lifetime (falls back to en, then key); reload to retry.
- Section ownership: touching `shell/`, `ui/`, `theme/`, `i18n/index.tsx`, `lib/` affects every section; keep API stable.
- Notices are not persisted and not shared across tabs; use `toast` for immediate feedback and `notify` for the bell.

## Checks and references
- `cd web && npm run typecheck && npm run lint && npm run i18n:check && npm test`; visual check of kit at `/__kit` in dev. `i18n:check` compares key sets only, not placeholders or unused keys.
- `docs/DESIGN-RULES.md` (tokens, themes, shape, structure), `docs/design/index.html` (approved screens), `web/CONTRIBUTING-SECTIONS.md`.
Related: map/web.md, map/settings.md, map/server.md
