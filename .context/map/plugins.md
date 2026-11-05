# Plugins
Purpose: signed, manifest-scoped third-party UI plugins that run in sandboxed frames and reach the host only through a capability broker and daemon-checked `plugins.*` methods.

## Entry points and public contracts
- `server/internal/modules/plugins/plugins.go:Register`: all bridge `plugins.*` methods (list/catalog/exec*/pty/http*/files/access/loadDev...); install/setEnabled/uninstall are `rpc.Admin`, the rest `rpc.User`.
- `.../plugins/manifest.go:ParseManifest|LoadManifest|validate`: strict manifest (unknown fields rejected), id `^[a-z][a-z0-9-]{1,39}$`, entry `.js|.mjs`, size/count caps (256 KiB, 64 commands, 16 http APIs).
- `.../plugins/store.go:scan|find|list|state`: locations `system` / `installed` / `dev`; scan order dev, system, installed (first id wins); enabled state in `plugins-state.json` (missing = enabled).
- `.../plugins/access.go:trust|authorize`: signature policy + enabled + core compat + `visibleTo`; every plugin-scoped method calls `authorize`. `plugins.access` feeds daemon frame/asset checks.
- `.../plugins/sign.go`: `TeamPublicKey`, `TrustedKeys`, `Canonical`, `CheckSignature`, `SignFolder`; `catalog.go`: `SignCatalog|VerifyCatalog`, `catalogSigPrefix`; `signkey/signkey.go`: private/public key file parsing.
- `.../plugins/cmd/plugin-sign`: genkey, sign folder, verify folder, sign/verify catalog.
- `.../plugins/install.go:install|ExtractTarGz`; `catalog.go` (remote catalog + `.sig`); `moved.go` (docker auto-migration); `corecompat.go` (`minCore` / `requires.ervisio`, `>=` only).
- Capabilities: `exec.go`, `pty.go`, `http.go`, `files.go`, `transfer.go`, `netcap.go` + `userhosts.go` (`capabilities.network`, user-approved hosts, `plugins-hosts.json`), `manifest_jobs.go` + `jobsapi.go:Resolve`, `envbridge.go:envCheck`, `platform.go` (linux/windows per-entry filtering), `dev.go|devasset.go`.
- Daemon: `server/internal/server/pluginframe.go` (`handlePluginFrame`, `pluginFrameCSP`, `setPluginAssetHeaders`, `pluginAccess`), `static.go:handlePlugin|serveDevPlugin`, `transfer.go` (one-time transfer URLs), `jobsglue.go` (`plugins.jobs.*`, `plugins.notify`, `bridgeExecutor`), `envs.go` (`plugins.envs.list`, `plugins.envCheck` call), `audit.go` (plugin action log; `plugins.audit.list`), routes in `server.go` (`/plugins/{id}/{file...}`, `/plugin-frame/{id}`, `/api/plugins/transfer*`, `POST /hooks/{plugin}/{token}`).
- Web host: `web/src/plugins/PluginsProvider.tsx` (plugins.list -> rail pages/widgets/snippets), `PluginFrame.tsx` (iframe + message loop), `PluginPage.tsx`, `broker.ts` + `brokerJobs.ts` (pure policy: `authorize*` -> `Plan`), `protocol.ts` (`la:'plugin'` messages, `FrameOp`), `types.ts` (`PluginManifest`, `PluginSDK`, contract `version: 3`), `networkApproval.tsx`.
- Frame runtime: `web/src/plugins/frame/{runtime.tsx,channel.ts,kit.ts,jobs.ts,i18n-shim.ts}`; built by `web/vite.runtime.config.ts` into `web/public/plugin-runtime.js` (single IIFE, `ErvisioPluginRuntime`).
- Plugins section UI: `web/src/sections/plugins/` (tabs installed/updates/browse/security/developer; `ConsentDialog`, `caps.ts` capability wording, `data.ts` list+catalog, `badge.ts`, `palette.ts`).
- Docs: `docs/api/plugins.md` (manifest, isolation, methods), `docs/PLUGIN-SIGNING.md`, `web/PLUGIN-SDK.md` (pointer; SDK lives in Ervisio/plugin-sdk), `plugins/README.md` (dev folder).

## Flow
- Install: Browse -> `plugins.catalog` (remote catalog.json + catalog.sig verified) -> ConsentDialog shows capabilities -> `plugins.install{source,sha256,consent}` (admin bridge) -> download (32 MiB cap) -> `ExtractTarGz` -> `LoadManifest` -> `CheckSignature` -> `CoreProblem` -> consent must equal declared caps -> atomic rename into `InstalledDir`, enabled.
- Load: `PluginsProvider` calls `plugins.list` (filters disabled/blocked) -> page `/p/<id>/<page>` or widget mounts `PluginFrame`.
- Frame: iframe `sandbox="allow-scripts allow-forms"` -> `GET /plugin-frame/<id>` (daemon asks session user bridge `plugins.access`, else 404; nonce CSP) -> runtime posts `ready` -> host fetches `/plugins/<id>/<entry>` with credentials and posts `init{code,theme,lang,appOrigin,platform}` -> runtime evals via `blob:` module.
- Broker: frame `req` / `stream-open` / `upload-open` -> `broker.authorize*` (manifest-only, first gate) -> `api.call/stream` of exactly one `plugins.*` method (`needs_admin` -> unlock dialog) -> daemon route -> bridge `authorize` (second gate) -> `sys` exec / unix-socket HTTP / file op as the user (or root bridge when entry is `admin`).
- Large transfers: `plugins.download|upload` -> `POST /api/plugins/transfer` -> bridge validates -> 60s one-time token URL bound to session; app (not the frame) fetches it.
- Jobs: `plugins.jobs.*` answered by daemon (`jobsglue.go`), broker fills `plugin`; steps run through `bridgeExecutor` -> `plugins.exec|http` with `admin` per manifest + owner; `Resolve` re-checks enabled/signature/visibility. Webhooks bypass session by token.
- Envs: call with `env` -> daemon checks access list, calls bridge `plugins.envCheck` (entry needs `remote`), passes tunnel path as `envSocket` (`envSocketOK`: clean abs socket owned by bridge uid); secrets stay in daemon.

## Invariants
- Plugin code never runs in the app origin: opaque-origin sandbox, CSP `default-src 'none'`, `connect-src` only declared `capabilities.network` hosts (https+wss; approved `http://` hosts get http only), `form-action 'none'`, no cookies/DOM/storage access.
- Plugin files are served inert (`sandbox; default-src 'none'`, nosniff, CORP same-origin) via `os.OpenRoot`; names must pass `fs.ValidPath`, no backslash. Archive extraction rejects symlinks, hardlinks, `..`, absolute paths; caps 16 MiB/file, 64 MiB total, 2000 entries.
- Two gates always: web broker (UX + first filter) and bridge `authorize`/manifest checks; never loosen only one. Broker maps frame ops to a fixed allowlist (`FrameOp`); no arbitrary RPC.
- Commands are argv with per-arg `ArgSpec`; HTTP only to declared unix sockets, rules (method+path), header allowlist; files only inside declared folders; `admin` only when declared, skipped for root or `adminUnlessGroup` members.
- Trust: `plugins.allow_unsigned=false` default. Signature = ed25519 over `"linuxadmin-plugin-v1\n"+canonical(manifest)`; `manifest.files` hashes every file; unlisted/missing/changed file fails. Dev-location folders run unsigned only in dev mode (`devUnsigned` badge).
- Catalog signature uses a different prefix (`ervisio-catalog-v1\n`); `plugins.catalog_key` trusts a listing only, install still needs a team signature.
- Install is all-or-nothing: consent mismatch, bad signature (even with allow_unsigned), too-new core, or replacing a `system` plugin aborts before anything is moved.
- Network approval is per host and scheme, admin-only (`plugins.network.approve`), UI owned by the app, not the frame.
- Private signing key never enters the repo; CI secret `PLUGIN_SIGNING_KEY` lives in Ervisio/plugins.

## Gotchas
- `signaturePrefix` keeps the old `linuxadmin-plugin-v1` name on purpose; renaming invalidates every signed plugin and catalog.
- Changing broker/protocol/runtime in a plugin-visible way requires the SDK repo update and a contract-version bump (`types.ts` `version: 3`, `runtime.tsx`).
- `/api/rpc` bodies are capped (`MaxRPCBody`, 16 MiB protocol line): big bodies must use transfers, not `plugins.http`.
- `plugin-runtime.js` is generated (`npm run build:runtime`, run by `dev` and `build`); the daemon CSP bans chunks/fonts/CSS fetches, so everything is inlined. `/plugin-frame/` and `/plugins/` also proxied by Vite.
- `capabilities` is parsed twice: `declared` (all systems) vs `Capabilities` (this OS); consent compares `declared`.
- `capabilities.network` accepts a list or `{hosts,userHosts}` (custom `UnmarshalJSON`).
- Dev plugins: `--dev` scans repo `./plugins`; `plugins.loadDev` folders are per user (`~/.config/ervisio/plugins-dev.json`) and served through the user bridge `plugins.devAsset` (16 MiB cap).
- Docker plugin left the core: `MigrateMoved` runs at daemon start (root, non-dev), state in `plugins-moved.json`; "moved" card in the UI is dismissed via localStorage.
- Frame teardown on any navigation; frames are recreated per plugin/view/reload; host identifies frames by `event.source`, targetOrigin `'*'`.
- Audit/credentials: mutating plugin calls are logged by the daemon (`server/internal/server/audit.go`), not by the bridge.

## Commands and references
- Tests: `cd server && go test ./internal/modules/plugins/... ./internal/server/...` (isolation, pluginframe, catalog, sign); web broker: `node --test web/tests/broker.test.ts`.
- Sign/verify: `go build -o /tmp/plugin-sign ./internal/modules/plugins/cmd/plugin-sign` then `-key K dir`, `-verify dir`, `-verify -catalog catalog.json`.
- Runtime build: `cd web && npm run build:runtime`.
- Read `docs/api/plugins.md` (Isolation, manifest.json, Signing, Methods), `docs/api/jobs.md`, `docs/api/notify.md`, `docs/api/environments.md`, `docs/PLUGIN-SIGNING.md`.
Related: map/server.md, map/web.md

Sweep helpers (2026-11-05): openCapRoot, openCapFile (plugins/files.go); web DetailPanel.tsx local runWork.
