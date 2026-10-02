# Docker plugin, source

TypeScript and React source of the Docker plugin (plugin SDK v3). The build writes the single file
`plugins/docker/index.js`; `plugins/docker/manifest.json` is edited by hand.

## Build

```
npm --prefix plugins-src/docker install     # once
npm --prefix plugins-src/docker run build   # typecheck, then bundle to plugins/docker/index.js
npm --prefix plugins-src/docker run dev     # rebuild on change (use with linuxadmind --dev, then "Reload" in Plugins)
npm --prefix plugins-src/docker run typecheck
```

After a build, re-sign `plugins/docker` with `plugin-sign` (only the release owner has the key).

## How it fits together

React is not bundled. `react` and the JSX runtime are aliased (vite.config.ts) to `src/react-shim.ts`, which forwards
to `sdk.react`. The SDK exists only inside `activate()` (`src/index.ts`), so:

* Never call a React API at module top level (`createContext`, `memo`, `forwardRef`, `lazy`). Hooks and JSX inside
  components are fine.
* Read the SDK with `getSdk()` (`src/sdk.ts`) inside functions, never at import time.
* Use the kit through `src/kit.ts` (`Button`, `Dialog`, `ConfirmDialog`, `toast`, ...), not `sdk.ui` directly.

```
src/index.ts          activate: sets SDK and React, strings, styles, icons; registers the page and the widget
src/sdk.ts            SDK v3 types and getSdk()
src/kit.ts            typed wrappers for the app's UI kit
src/router.ts         in-plugin router (Route type, navigate, back, useRoute, search box state)
src/api/              Docker Engine client and data layer
  engine.ts           docker.get/post/delete/stream, version negotiation, DockerError, classify()
  streams.ts          JsonLines, LogDemuxer, LogLines
  stats.ts            CPU/memory math and shared per-container stats streams
  events.ts           one shared /events stream
  store.ts            createResource(): polled + event-refreshed shared store
  resources.ts        containers, images, volumes, networks, info, diskUsage stores
  hooks.ts            useStats, useDockerEvents, useAsync
  actions.ts          start/stop/restart/remove, runBulk
  model.ts            stack grouping, health, ports, filters
  format.ts           bytes, percent, durations, relative time
src/settings.ts       JSON files in ~/.config/linuxadmin/plugins/docker (settings, registries, alerts, templates-sources)
src/i18n/areas/*.ts   strings, one file per area, merged automatically
src/styles/*.css      CSS, every file is injected automatically
src/ui/               shared pieces: PageHeader, DataTable, DiskBar, StatusDot, Charts, ErrorState, ComingSoon
src/shell/App.tsx     inner sidebar, search box, error gate
src/views/            one component per route, ViewHost in registry.tsx
```

## Add or fill a view

1. The route exists in `src/router.ts` (`Route`). For a new one, add it there, add its nav section in `sectionOf`,
   and render it in `src/views/registry.tsx`.
2. Edit the view file in `src/views/`. Its props are the route fields: `RouteProps<'container'>` is `{ id, tab? }`.
   Start with `<PageHeader icon title subtitle actions back />`.
3. Strings: create `src/i18n/areas/<area>.ts` exporting `{ en, it }` with keys `<area>.something`. Use `t('key')`.
4. Styles: create `src/styles/<area>.css`. Prefix classes with `dk-`, use theme variables only (`--surface`, `--sunk`,
   `--ink*`, `--acc`, `--ok`, `--warn`, `--err`, `--h`/`--s` inside a `hue-*` element, radii `--r-*`). No grey lines.
5. Data: `const { data, error, loading } = containers.use()` for shared lists; `docker.get/post/delete` for the rest;
   `docker.stream` for logs, stats and pulls. Tell the user about failures with `toast.err(title, errorText(e))`.
6. Go to another view with `navigate({ view: 'container', id })`; `navigate(route, { root: true })` starts a fresh history.
   `back()` goes back.

Every Engine path a view uses must be allowed by a rule in `plugins/docker/manifest.json` (`capabilities.http`).
