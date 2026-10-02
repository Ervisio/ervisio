# LinuxAdmin security review: remediation checklist

Date: 2026-10-01. Scope: `server/` (daemon, bridge, rpc, pam, sys, all modules) and `web/src` (API client,
plugin loader, HTML sinks). I traced every item from the HTTP/WebSocket entry point to the code that acts on
the system. Items marked **needs check** depend on distribution or sudo defaults that I could not confirm from
the code alone.

Threat model: an unauthenticated client; a signed-in user who has not unlocked admin; another local user on
the same machine; a third-party plugin. After an admin unlocks, that browser user is root by design, so
input coming from that admin is not counted as a weakness.

## Findings (sorted by severity)

| # | Sev | Location | Weakness | Fix |
|---|---|---|---|---|
| H1 | High | `server/internal/modules/files/ops.go:428-441` (recursive `hChmod`), `:521-531` (recursive `hChown`), `:59-140` (`copyTree`/`copyFile`) | In the root bridge, recursive chmod/chown/copy/move walk a tree and then act on full path strings. Only the last path component is protected from symlinks, so a non-root owner of the tree can change parent directories while the walk runs and redirect root's operation outside the tree. | **Fixed:** `files/safefs.go`: every changing method (chmod/chown/copy/move/delete/Trash/mkdir/create/writeText/writeStream) works on directory fds with `*at()` calls and `O_NOFOLLOW`; recursive walks open each folder `O_NOFOLLOW\|O_DIRECTORY` and act on the fd; as root, paths are resolved component by component and only root-owned links are followed (`forbidden` otherwise). Race tests in `files/safefs_test.go`. |
| H2 | High | `web/src/plugins/PluginsProvider.tsx:81-113`; `server/internal/server/static.go:90-125`; `server/internal/config/config.go:95`; `server/internal/modules/plugins/sign.go:20-23` | Plugin frontend modules run in the app's own origin with the full `call`/`stream` API (including `admin:true`). Manifest capabilities restrict only `plugins.exec`: `capabilities.files` and `capabilities.sockets` are never enforced. Unsigned plugins are allowed by default, and the only trusted key is a placeholder. | **Fixed**: plugin pages/widgets run in `<iframe sandbox="allow-scripts">` (`/plugin-frame/<id>`, opaque origin, strict CSP, no cookies/API) behind a manifest-enforcing `postMessage` broker (`web/src/plugins/broker.ts`); files via new server-checked `plugins.readFile/writeFile/listDir`; user commands refused on the root bridge; `allow_unsigned` defaults to false; real team key embedded (`docs/PLUGIN-SIGNING.md`). |
| H3 | High (dev only) | `server/internal/server/auth.go:99-140` (`sessionFor`/`noAuthSession`), `:51-81` (`originAllowed`); `server.go:151-162` | `--dev-insecure-noauth` authenticates every request that reaches the loopback listener. There is no `Host` allowlist, which leaves it exposed to DNS rebinding. Loopback is shared by all local users. There is no check for running as root, so a root daemon gives an unauthenticated root session. | **Fixed**: noauth refused as root and needs `--dev` + loopback listen; `--dev` answers only loopback `Host` names (421 otherwise); sign-in only through a one-time start-up URL (`/api/dev/noauth?token=…`, printed on the console) that sets the cookie. |
| M1 | Medium | `server/internal/server/auth.go:222-258`; `ratelimit.go:24-42` | Login rate limiting is check-then-record: `blocked()` runs before PAM and `fail()` only after PAM returns. Concurrent in-flight attempts from one IP are therefore not counted. The key is the full IPv6 address. | **Fixed**: `limiter.begin` reserves (counts) the attempt atomically before PAM/sudo and takes it back only on success; max 2 in flight per client, 8 PAM slots globally; IPv6 keyed by /64. |
| M2 | Medium | `server/internal/server/ws.go:16-20, 168, 205-224` | Per WebSocket connection the root daemon buffers up to 64 channels × 256 queued input frames × 2 MiB (`wsReadLimit`). There is no limit on WebSocket connections per session. A signed-in user can therefore make the root daemon allocate unbounded memory. | **Fixed**: frames ≤ 512 KiB (1009 close), per-channel queue = `rpc.Window`, 8 MiB queued-input budget per session (channel closed with `unavailable`), 8 WebSockets and 128 channels per session. |
| M3 | Medium | `server/internal/modules/software/pacman.go:136-181` (`tmpDir`, `Refresh`, `copyFile`), `:141-152` (`dbArgs`) | The private pacman database lives at a predictable path in shared `/tmp` (`linuxadmin-checkdb-<uid>`). The code reuses an existing directory without checking its owner or mode, follows existing entries and symlinks in it, and then trusts its contents for the update list. When this runs as root, it creates files through entries in that directory. | **Fixed:** the private copy lives in `/var/cache/linuxadmin/checkdb` (root) or `~/.cache/linuxadmin/checkdb` (user), must be a 0700 dir of the bridge user in a parent no one else can write, planted entries are replaced, copies use `O_EXCL\|O_NOFOLLOW` (`software/pacman.go`, `privfile.go`, tests in `privfile_test.go`). |
| M4 | Medium | `server/internal/pam/pam.go:375-397`; `server/internal/modules/terminal/terminal.go:304-315`; `server/internal/modules/overview/actions.go:58-80` | Sign-in runs only `pam_authenticate` + `pam_acct_mgmt`. It never runs `pam_open_session`, so limits.conf, pam_systemd and loginuid/audit are not applied. It also does not check the login shell. Accounts with `nologin`/`false` shells (SFTP-only, service accounts) can sign in, and the terminal falls back to `/bin/sh`. On Arch the packaged stack includes `pam_shells`; on Debian/Fedora stacks it does not (**needs check** per distro). | **Fixed**: shells `nologin`/`false`/restricted or not in `/etc/shells` are refused (shell from NSS, no `/bin/sh` fallback); user bridge runs in a PAM session (acct_mgmt + setcred + open/close_session) opened by the root helper `linuxadmind --pam-session-helper`; skipped in `--dev`. Terminal `/bin/sh` fallback lives in `modules/terminal` (not changed here). |
| M5 | Medium | `server/internal/server/session.go` (no absolute expiry); `route.go:440` (bridge restart reuses cached `sess.Account`) | Sessions have only an idle timeout and are never checked again. Group removal, account lock, password change or user deletion do not end live sessions. Bridge restarts reuse the group list from sign-in. | **Fixed**: absolute lifetime (24 h, or `session.timeout` if longer with remember); every 60 s, on unlock and before a bridge restart the account is re-checked (exists, same uid, allowed shell, no group lost, shadow lock/expiry/password fingerprint as root, PAM acct_mgmt) and the session is ended on failure. |
| L1 | Low | `server/internal/modules/software/transaction.go:62-75, 461-476` | When `XDG_RUNTIME_DIR` is unset, the user state file falls back to a predictable name in `/tmp`, and `readStatus` (also in the root bridge, as uid 0) trusts whatever file is there. The root state file in `/run/linuxadmin` is world-readable and contains package logs. | **Fixed:** user state file only in a private `$XDG_RUNTIME_DIR`; state written atomically (`O_EXCL` temp + rename) into owner-only dirs; root keeps the full state 0600 and publishes a log-free 0644 summary; readers use `O_NOFOLLOW` and require a regular file of the expected owner (`software/transaction.go`, `privfile.go`). |
| L2 | Low | `server/internal/server/static.go:90-125` (`handlePlugin`) | Plugin assets are served to any signed-in user, regardless of the manifest's `visibleTo` or the enabled state. | **Fixed**: `/plugins/<id>/…` and `/plugin-frame/<id>` are served only after `plugins.access` on the user bridge confirms enabled, signature policy and `visibleTo`; plugin files carry `CSP: sandbox`, nosniff, CORP same-origin. |
| L3 | Low | `server/internal/server/ws.go:53-55` | In dev mode the WebSocket accepts any `localhost:*` origin. Cookies are shared across ports, so any page on another local port can open the socket with the session. | **Fixed**: dev WebSocket and CSRF origins limited to the request's own host plus the `--vite` host. |
| L4 | Low | `server/internal/server/auth.go:243-249` | Eight global PAM slots can be held by unauthenticated clients (each PAM failure delays about 2 s), which blocks legitimate sign-ins. With `pam_faillock`, failures can lock accounts. | **Fixed**: per-client in-flight cap (2) before taking a PAM slot, 5 s wait limit on the slots (503 `busy`); faillock interaction documented in `docs/api/auth.md`. |
| L5 | Low (**needs check**) | `server/internal/bridge/bridge.go:81-87, 224-238` | The root bridge inherits the invoking user's working directory, and possibly `HOME` depending on sudoers. Root-run commands therefore start in the user's home, and `plugins.dev` scans `<cwd>/plugins` as root. | **Fixed**: `linuxadmin-bridge --admin` does `chdir("/")` and sets `HOME`/`USER`/`LOGNAME` from root's passwd entry (and drops `XDG_RUNTIME_DIR`); sudo is also started from `/`. |
| L6 | Low | `server/internal/server/auth.go:252-254, 272-274` | The "account cannot sign in" and "root disabled" responses are returned only after PAM succeeded, and they are not counted by the limiter. They confirm that a password is correct for those accounts. | **Fixed**: after PAM success, account-check refusals, uid-0 aliases and disallowed shells return the generic bad-credentials error and count as failures; the reason is logged only. |
| L7 | Info | `web/src/pages/Login.tsx:10-29` | The sign-in page keeps the last user names in `localStorage`, so they are visible on shared browsers. | **Fixed**: recent user names are stored only after opting in (Settings → This browser, off by default); turning it off, or not having opted in, clears the list. |

## Checked and found sound

Session tokens (256-bit `crypto/rand`, stored hashed); cookie flags (HttpOnly, Secure outside dev,
SameSite=Strict). CSRF header plus Origin check on every POST; WebSocket origin check in production.
Logout and idle expiry. sudo invocation (`-S -p '' -k --`, fixed `PATH`, no shell, verdict parsed from stderr,
hello `uid==0` check). User bridge spawned with `setgroups`/uid/gid, a clean environment and no inherited fds.
Root routing (`needs_admin` unless unlocked). Method levels: no system-changing method is registered as
User-level without running as the user. SSH key edits under `setfsuid`. Plugin tar extraction (no links,
traversal or size bombs, `O_EXCL`). Signature verification (prefix, canonical JSON, per-file hashes, no
unlisted files). `plugins.exec` slot validation (anchored patterns, leading-dash rule, fixed `argv[0]`). Unit,
package, user, host and journal arguments (validated, `--`/`--opt=value` forms). `chpasswd` input rejects
newlines. Download headers (attachment, sandbox CSP, nosniff, inline only for safe types). Static and
plugin file serving confined with `os.OpenRoot`. CSP on the app. No `dangerouslySetInnerHTML`/URL sinks
with server data in the web client.

## Plugin SDK v3 and Docker 2.0 review (2026-10-02)

Scope: commits `0059255..3daa42b`, which cover `plugins.http`/`httpStream`/`pty`/`mkdir`/`remove`, admin folders,
the broker and frame changes, `allow-forms`, `openUrl`, `plugins/docker/manifest.json` and `plugins-src/docker`.
The question was whether a plugin frame can do more than its manifest declares, and whether a docker-group user can
do anything as root through LinuxAdmin that they could not already do with the Docker socket.

| # | Severity | Location | Defect | Status |
|---|---|---|---|---|
| P1 | Medium | `web/src/plugins/broker.ts` (`openUrl`) | `openUrl` accepted any http(s) URL, including the app's own origin. A plugin could open an app tab outside its manifest, for example `/terminal?cmd=…`, which opens a new root-capable terminal with a command already typed in. | **Fixed:** `BrokerUser.appOrigin` (set by `PluginFrame` to `window.location.origin`) is refused. Test in `web/tests/broker.test.ts`. |
| P2 | Medium | `plugins-src/docker/src/views/TemplatePage.tsx` | Stack templates from third-party Portainer lists were deployed in one click. The compose file came from the template repository's mutable `HEAD`, the user never saw it, and "Show compose" fetched it a second time, so the preview could differ from what was deployed. | **Fixed:** for stacks without a built-in compose file, the first Install fetches the file and shows it, and the second Install deploys exactly the reviewed text. A warning appears when the file uses `privileged`, the Docker socket, host namespaces, `cap_add`, `devices`, `security_opt` or a `/` bind. Container templates already went through the create wizard's review step, which shows the full `docker run`. |
| P3 | Low | `server/internal/modules/plugins/files.go` | `readFile`, `listDir` and `writeFile` (parent folder) opened paths in a blocking mode. A FIFO planted in a shared folder (`/opt/stacks` is `2775 root:docker`) made the call hang, including on the root bridge of an admin. | **Fixed:** `openNoBlock` (`O_NONBLOCK` through `os.Root`) followed by a type check. Test `TestFilesFIFOAndPrivateMode`. |
| P4 | Low | `server/internal/modules/plugins/http.go`, `files.go` | Text bodies are sent as JSON strings, and `encoding/json` writes `<`, `>`, `&` and control characters as 6 bytes each. An 11 MiB response (or a 4 MiB file) could therefore exceed `rpc.MaxLine`. The line was dropped and the call never answered, so content inside a container (logs, inspect output) could stall the plugin. | **Fixed:** `jsonTextLen` upper bound. Bodies over 12 MiB escaped are sent as base64, which the SDK decodes transparently. Tests `TestJSONTextLen` and `TestHTTPLargeEscapedTextGoesBase64`. **Open (pre-existing):** `plugins.exec` stdout and stderr (4 MiB each) have the same worst case. |
| P5 | Low | `server/internal/modules/plugins/files.go` (`writePluginFile`) | Files in a plugin's `create: true` folder under `~` (the Docker plugin stores `registries.json` there, with registry passwords) were written `0644`. They were private only when the folder itself was newly created `0700`. | **Fixed:** files in `create: true` folders under `~` are written owner-only (`mode &= 0700`, so new files are `0600` and rewrites tighten old ones). Passwords are still stored in plain text, as Docker's own `config.json` stores them (accepted). |

**Checked and found sound**

- **Rule matching (`cleanHTTPPath`, `match`):**
  - Paths are origin-form only, with no `?`, `#`, `\` or control characters. `%2f`, `%5c`, `%00`, `%0a` and `%0d` are refused.
  - The decoded path must already be clean: no `//`, `.`, `..` or trailing `/`.
  - Rules are RE2, anchored `^(?:…)$` on the decoded path, and Docker's mux matches on that same once-decoded path.
  - None of the Docker rules' character classes allow `%`, so double-encoding cannot reach another route.
  - The version prefix is required (`/v1\.[0-9]+/`) except for `/_ping` and `/version`. Matching is case-sensitive on both sides.
- **Docker routes:**
  - Docker's `{name:.*}` image routes cannot be steered to other handlers: GET must end in `/json` or `/history`, POST in `/tag`, and DELETE is the only DELETE route. Container, volume and network names exclude `/`.
  - The rules exclude `/plugins`, `/swarm`, `/services`, `/nodes`, `/tasks`, `/secrets`, `/configs`, `/session`, `/grpc`, `/build` (only `/build/prune`), `/containers/{id}/archive` (GET, HEAD and PUT), `/export`, `/attach`, `/wait`, `/commit`, `/images/load`, `/images/get`, `/images/{name}/push` and `/images/search`. They should stay excluded.
- **Request headers:**
  - Only `Content-Type` and `X-Registry-Auth` may be set.
  - Host, credentials, hop-by-hop, `Proxy-*`, `Sec-*` and `X-Forwarded-*` headers are refused at manifest validation. Values with CR, LF or NUL are refused.
  - `Host` is fixed to `localhost`.
- **Responses:** no redirects are followed and no proxy is used. Response headers are limited to 64 KiB, `Set-Cookie` is dropped, and bodies are capped at `min(maxBody, 11 MiB)`.
- **Streams:** `httpStream` ends the connection when the stream input closes. Frame streams are capped at 32 and closed on teardown or navigation.
- **Root bridge and `adminUnlessGroup`:**
  - User-level commands, HTTP APIs and folders are refused on the root bridge.
  - Admin entries run on the root bridge, or as the user when the user is root or in `adminUnlessGroup` (looked up through NSS).
  - A docker-group member's requests always run as that member, never as root.
  - `~` is not allowed in admin folders.
  - The root bridge environment is reset (`HOME=/root`, `cwd /`, fixed `PATH`), and the Docker CLI is pinned with `-H unix:///var/run/docker.sock`.
- **PTY:** `pty` commands run only through `plugins.pty`. The process runs as a new session; closing the stream sends SIGHUP and then SIGKILL to the process group.
- **Files:** every path is resolved through `os.Root`, so neither symlinks nor `..` can leave the declared folder. `mkdir` works one component at a time. `remove` deletes the link itself, never its target, and refuses a declared folder (`rel == "."`). Writes use an `O_EXCL` temporary file plus `renameat`, so a symlink or hardlink at the target is replaced, not written through.
- **Broker:** every new op (`http`, `httpStream`, `pty`, `mkdir`, `remove`, `openUrl`) is checked against the server-provided manifest, and the daemon checks again.
- **Frame isolation:**
  - `allow-forms` together with CSP `form-action 'none'` means submit events fire but no form is ever sent.
  - There is no `allow-popups`, `allow-top-navigation` or `allow-same-origin`, and `default-src 'none'` blocks nested frames.
  - `openUrl` takes http(s) only, without credentials, and opens with `noopener,noreferrer`.
- **Docker plugin UI:**
  - The only `dangerouslySetInnerHTML` is the compose and env highlighter (`stack/highlight.ts`). It escapes `& < >`, and its class names are fixed.
  - Portainer descriptions and notes are tag-stripped and rendered as text. Remote logos are not loaded.
  - Template `website` links come only from the signed built-in catalog.

**Accepted and noted**

- Docker group membership and Docker administration are root-equivalent. The consent screen discloses this, so `containers/create` (binds, `privileged`), `exec` and `networks/create` are allowed as Docker allows them.
- **Query strings are not constrained by rules.** Through `/images/create?fromSrc=URL` or `fromImage=host/…` and `/auth`, a plugin can make the Docker daemon contact hosts outside `capabilities.network`. This is inherent to pulling images.
- **`compose-config-project` runs `docker compose -f <any *.yml> config` as root for admins who are not in the docker group.** The path comes from a container's compose label, so a docker-group user can choose it. The output goes only to the admin's screen. This is accepted because the docker group is root-equivalent anyway.
- **Stacks created by an admin on the root bridge are not group-writable.** Their folders are `0755` and their files `0644`, owned by root, so docker-group members cannot edit them afterwards. This is a usability issue, not a security one.
- **A shell opened by `docker exec` may outlive its PTY.** When the pty stream closes, the Docker CLI is killed, but whether the process in the container ends is up to Docker.
- **A frame can still navigate itself.** The host then tears the frame down, but only after the request has left; this was already true before this change set. `allow-forms` adds no new way out.
