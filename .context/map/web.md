# Web application
Purpose: React 18/TypeScript browser console for ervisiod, built with Vite and repository-owned UI components.

## Entry points and public contracts
- `web/src/main.tsx`: mounts BrowserRouter, SessionProvider, PrefsProvider, ThemeProvider, I18nProvider and PluginsProvider.
- `web/src/App.tsx`: public `/login`; RequireAuth wraps AppShell and section/plugin routes; `/__kit` exists only in development.
- `web/src/sections/index.ts`: SECTIONS owns lazy pages and navigation metadata; SectionDef is the section contract.
- `web/src/sections/registry.ts`: PaletteAction/RailBadge and registration hooks; discovers section `badge.ts`, `palette.ts`, `background.ts` with build-time globs.
- `web/src/api/index.ts`: shared API exports; `types.ts` defines ApiError, CallOptions, Session, PublicHost, StreamHandlers and StreamHandle.
- `web/src/plugins/types.ts`: PluginManifest, PluginSDK, PluginViewDef and PluginModule; `plugins/protocol.ts` defines frame messages.

## Flow
- Page -> `api.call(method, params, options)` -> `client.ts` -> `http.ts` -> `/api/rpc` -> daemon/bridge.
- `needs_admin` opens the shared unlock dialog and retries a call once; `noUnlock` suppresses the dialog.
- `api.stream` multiplexes channels over `/api/ws`; JSON payloads are parsed, base64 payloads become Uint8Array.
- Session/preferences feed shared providers; preferences save optimistically through `prefs.set`.
- Plugin pages/widgets use sandboxed opaque-origin iframes and a postMessage capability broker, rather than executing inside the app.

## Invariants
- Build API URLs through `web/src/api/base.ts`; same origin is default, `VITE_API_BASE` supports external daemon origins.
- Section ownership is limited to its folder, matching English/Italian dictionaries and backend module; foundation owns shared API/UI/shell/registry.
- Visible text, aria labels, toasts, badges and palette text use i18n; English and Italian keys must match.
- Reuse `web/src/ui` and design tokens; preserve focus rings, reduced-motion behavior and the shell's 860px mobile breakpoint.
- Close streams during effect cleanup; surface failed interactive calls as actionable inline errors or toasts.
- Background hooks run on every page: keep them light, avoid administrator prompts and stay quiet on errors.

## Gotchas
- Disconnected streams fail with `unavailable` unless `reopen: true` resends the open frame after reconnect.
- Cross-section links use query parameters; see the target/parameter table in CONTRIBUTING-SECTIONS.md before constructing links.
- Format dates through `web/src/lib/format.ts` to honor language/region and clock preferences.
- `VITE_MOCK=1` covers only selected host/metrics/preferences/config/plugin/auth methods; it is not full backend verification.
- Vite proxies `/api`, `/plugins/`, `/plugin-frame/` to `ERVISIO_API` (legacy `LINUXADMIN_API`), default `http://127.0.0.1:9090`.
- Build keeps assets external (`assetsInlineLimit: 0`) for daemon CSP; dev/build first build the separate plugin runtime.

## Commands and references
- From `web/`: `npm run dev` alongside `ervisiod --dev`; mock mode in PowerShell: `$env:VITE_MOCK='1'; npm run dev`.
- Checks: `npm run typecheck`, `npm run lint`, `npm run i18n:check`, `npm test`; production build: `npm run build` (outputs `web/dist`).
- Sources: `docs/ARCHITECTURE.md` sections Web app, Preferences, Plugins; `web/CONTRIBUTING-SECTIONS.md` for section contracts and ownership.
- Related: `docs/DESIGN-RULES.md`, `web/PLUGIN-SDK.md`, `docs/api/plugins.md`, `web/package.json`, `web/vite.config.ts`.

Sweep helpers (2026-11-05): errorMessage (api/types.ts), lib/clipboard.ts, lib/useStore.ts, i18n/flatten.ts, adminGate (settings/useAdminLoad.tsx), b64encode, useIsMobile, measure (Menu.tsx). Format: npx prettier@3.3.3 (web/.prettierrc.json).
