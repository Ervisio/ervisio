# Brief for section agents

You build ONE section of LinuxAdmin end to end (Go bridge module + React page), in
/home/fonlogen/Documenti/LinuxAdmin. Seven other agents build the other sections at the same
time in the same working tree, so stay strictly inside your files.

## Read first
- docs/ARCHITECTURE.md, docs/DESIGN-RULES.md, docs/api/*.md (existing contracts)
- web/CONTRIBUTING-SECTIONS.md (how to use api, ui kit, i18n, palette actions, rail badges)
- server/README.md (rpc framework, sys helpers, dev mode)
- Your approved mock-up: docs/design/screens/<screen>.html. Reproduce it faithfully, but with
  the final rules: no grey divider lines, rounded-rectangle components from web/src/ui.

## You own ONLY
- server/internal/modules/<id>/ (fill `Register`, add files, tests)
- web/src/sections/<id>/ (replace the placeholder; prefix every CSS class with `<id>-`)
- web/src/i18n/en/<id>.json and web/src/i18n/it/<id>.json (full English + Italian)
- docs/api/<id>.md (document every method: params, result JSON, level user/admin, errors)

Do NOT edit anything else: not go.mod/go.sum, package.json/lock, web/src/ui, web/src/api,
web/src/shell, web/src/theme, other sections, the i18n generator. Never run `go mod tidy`,
`npm install <pkg>` or the i18n generator script. If you truly need a shared change or a new
dependency, finish without it and list it in your final report.
Do not commit or push; the coordinator does.

## Backend rules
- Run commands only through server/internal/sys (argv, no shell). Validate every param.
- Pick the level per method: read-only things the user may do → rpc.User; changes to the
  system → rpc.Admin (the daemon routes them to the sudo bridge). Calls with admin:true
  can also run User methods as root (e.g. reading /root).
- Errors: rpc.Errorf with the defined codes; messages written for humans.
- Unit tests for parsing logic (parse real command output samples). `go vet` clean.
- Build your own test binaries into your scratch dir, never into server/bin
  (e.g. `go build -o $SCRATCH/bin/ ./cmd/...`).

## Testing end to end
- Your test ports: daemon 127.0.0.1:<port>, Vite 5<port-last-3> (given below).
- `server/bin`-free run: build both binaries into your scratch dir, then
  `<scratch>/bin/linuxadmind --dev --dev-insecure-noauth --listen 127.0.0.1:<port> --bridge <scratch>/bin/linuxadmin-bridge`
  (from the repo root). The daemon prints a one-time sign-in URL: open it in the browser, or run
  `go run ./server/tools/devclient login '<url>'` and pass the printed token with `-cookie` (or
  `LA_SESSION=`) to `go run ./server/tools/devclient`, or as `Cookie: la_session=<token>` with curl.
- You cannot sudo (no password): admin methods can be tested only for the needs_admin path;
  test their logic with unit tests and by reading command output.
- Web: `cd web && LINUXADMIN_API=http://127.0.0.1:<port> npx vite --port <vite-port>`; take
  screenshots with Playwright's Chromium (already in ~/.cache/ms-playwright; `npx playwright`
  is available through node_modules or install nothing — use `node` + `playwright-core` if
  present, else skip screenshots) at 1280×800 and 390×844, and compare with the mock-up.
- `cd web && npx tsc --noEmit -p .` — fix errors in YOUR files only; ignore errors from other
  sections that are still being written.
- Kill every process you started before finishing.

## Final report (short)
What works, what was verified and how, methods added, anything not done, shared changes or
dependencies you need from the coordinator.
