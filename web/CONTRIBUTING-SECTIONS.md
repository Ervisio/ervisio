# Guide for section agents

You own **only** your section: `web/src/sections/<id>/` (any files you like inside), the dictionaries
`web/src/i18n/en/<id>.json` and `web/src/i18n/it/<id>.json`, and (backend side) your module. Everything
else (`api`, `ui`, `shell`, `theme`, `i18n`, `styles`, `sections/index.ts`) belongs to the foundation. If
you need something there, put a new file in your own folder or report it.

Section ids: `overview terminal files logs services software users plugins` (`settings` is done).

## Files

```
web/src/sections/<id>/index.tsx   default export = the page component (lazy loaded, replace the placeholder)
web/src/sections/<id>/*.tsx|css   your components and CSS (prefix CSS classes with your id, e.g. .svc-row)
web/src/sections/<id>/badge.ts    optional: default-exported hook returning the rail badge (see below)
web/src/sections/<id>/palette.ts  optional: default-exported hook returning command palette actions
web/src/i18n/en/<id>.json         English strings (nested keys allowed)
web/src/i18n/it/<id>.json         Italian strings, same keys. Run `npm run i18n:check`
```

Your page renders inside the shell's rounded surface panel. The route is `<path>/*`, so you may use
nested routes (`<Routes>` with relative paths) or query params. Use `<Page title subtitle actions hue>`
from `ui` for the standard 26/800 title row and padding, or `<Page flush>` when your section lays out
its own columns (Files, Terminal, Logs). The panel scrolls on desktop; make long lists scroll inside
your own container when you want sticky toolbars.

Run: `npm run dev` (Vite on :5173, proxies `/api` incl. WebSocket and `/plugins` to 127.0.0.1:9090) next to
`linuxadmind --dev`, or `VITE_MOCK=1 npm run dev` without a daemon (mock answers only `system.host`,
`system.metrics`, `prefs.*`, `config.*`, `plugins.list` and the auth routes; sign in with any password
except `wrong`). `npm run typecheck`, `npm run lint`, `npm run build` must pass.

## Calling the backend

```ts
import { call, stream, ApiError, downloadUrl, uploadUrl } from '../../api';

const units = await call<Unit[]>('services.list');                    // user level
await call('services.restart', { name: 'nginx' });                    // admin method: unlock dialog appears by itself, then retried once
await call('files.list', { path: '/root' }, { admin: true });         // force the root bridge
try { await call(...) } catch (e) { if (e instanceof ApiError) e.code /* 'not_found' | 'conflict' | ... */, e.message }

const s = stream<LogLine>('logs.follow', { unit: 'nginx' }, {
  onData: (line) => ..., onEnd: () => ..., onError: (e) => ..., admin: false, reopen: true,
});
s.send({ ... });   // input frame, forwarded as-is to the module (use toBase64() for bytes)
s.close();         // always close in a useEffect cleanup
```

* `stream` data frames flagged `b64` arrive as `Uint8Array`; JSON frames arrive parsed.
* Streams share one WebSocket. If it drops, streams end with `onError({code:'unavailable'})`, unless you
  pass `reopen: true` (the open frame is re-sent after reconnect).
* Sign-in state: `const { session, host, isUnlocked, unlockLeft } = useSession()` (`session.user`, `isAdmin`,
  `canSudo`...). Per-user preferences: `const { prefs, set } = usePrefs(); set('files.bookmarks', [...])`
  (instant optimistic save, persisted with `prefs.set`).
* Errors: every failed call should end in a toast or an inline message that says what went wrong and how to
  fix it. Do not swallow errors.

## Components (`import { ... } from '../../ui'`)

`Button` (variants `primary secondary ghost danger danger-solid`, `icon`, `loading`, `size`), `IconButton`
(`icon`, `label`), `Input` (`label hint error icon kbd end mono`), `Textarea`, `Select` (`options value onChange`),
`Switch`, `Checkbox`, `Radio`, `Segmented`, `Badge` (`tone ok|warn|err|info|neutral`), `Chip`, `Tabs`
(`variant pill|underline`), `Card`, `StatCard` (`hue`), `Table` (zebra, sticky header, `sortable`, `selectable`),
`Panel` (right side panel, becomes a bottom sheet on phones), `Sheet`, `Dialog`, `ConfirmDialog`
(`confirmText` for type-to-confirm), `UnlockDialog`, `toast` (`toast.ok/err/info/undo/show/update`), `Menu`,
`DropdownMenu`, `useContextMenu`, `Tooltip`, `EmptyState`, `Progress`, `Skeleton`, `Sparkline`, `AreaChart`, `Icon`,
`Page`, hooks `useIsMobile`, `useMediaQuery`. Open `/__kit` in dev to see them all.

Side panel pattern:

```tsx
<Panel open={!!sel} onClose={() => setSel(null)} title={sel?.name} subtitle="Running for 6 min" icon="services" hue="svc"
       tabs={<Tabs value={tab} onChange={setTab} items={[...]} />} footer={<Button>Restart</Button>} wide>
  ...content for the active tab...
</Panel>
```

`Panel` floats over the page on the right (`inline` renders it in the layout flow instead). Details always go
in a Panel, never in a new page. Dangerous actions use `ConfirmDialog` with `confirmText`.

Hue scopes: wrap your root in `className="hue-svc"` (or pass `hue`) and use `var(--h)` / `var(--s)` for the
section colour and its soft fill. Status uses `--ok --warn --err --info` (a failed thing is always `--err`).
File-type tiles reuse section hues (`--h-file`, `--h-plg`...).

Helpers: `src/lib/format.ts` has `formatBytes`, `formatDuration`, `formatClock`, `formatPercent`, `relativeTime`.

## i18n

```tsx
const t = useT('services');          // namespace = your section id
t('title');  t('restarted', { name });  t('count', { count: 3 })   // plural keys: "count_one" / "count_other"
```

Every visible string goes through `t()`, including `aria-label`s and toasts. Use `useT('common')` for generic
words (`cancel`, `delete`, `search`...), `useT('ui')`/`useT('shell')` belong to the foundation. English is the
default and Italian must be complete: same keys in both files (`npm run i18n:check`). Copy style: plain, active;
buttons say what happens ("Restart", "Delete user"); toasts echo it ("nginx restarted"); errors say what went wrong
and how to fix it. The command palette and rail badge text must be translated by you too.

## Command palette actions and rail badges

```ts
import { usePaletteActions, useRailBadge } from '../../sections';

// while the page is mounted:
usePaletteActions('services', [
  { id: 'services.restart-all', title: t('palette.restartAll'), hint: t('title'), icon: 'refresh', hue: 'svc', keywords: ['reload'], run: () => ... },
]);
useRailBadge('services', failedCount);           // 0/undefined hides it; third arg 'info' for an accent badge
```

To show things even when your page is not open (the failed-services count, the updates count), add
`sections/<id>/badge.ts`:

```ts
export default function useBadge(): number | undefined { /* a cheap hook: one poll every 60 s at most */ }
```

and/or `sections/<id>/palette.ts` with `export default function usePalette(): PaletteAction[]`. They are
picked up automatically (build-time glob) and called by the shell on every page, so keep them light.

## Design rules (see `docs/DESIGN-RULES.md`)

* **No grey divider lines.** Separate with spacing, tinted rows (zebra), tinted panels, hover highlights.
* Rounded rectangles: cards 18, controls and inputs 12, badges 8, dialogs 20, toasts/menus 14. Control height 36,
  input 40. No pills for buttons or inputs. Focus ring is the global 2px accent outline: do not remove it.
* Primary action = accent fill with black text (`Button variant="primary"`), one per view.
* Motion is subtle and only on user action; honour `prefers-reduced-motion` (global CSS already shortens
  animations, do not add your own long ones).
* Mobile first-class: the shell switches at 860px (`useIsMobile()`); lists become cards, panels become sheets.
* Do not hard-code colours: use the CSS variables so all nine themes and the colour modes work.
* Keep chunks small: heavy libraries (xterm, charts) are imported inside your section only.
