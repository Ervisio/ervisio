# Remote hosts and multi-host
Purpose: reach other machines (SFTP/FTP/SMB/WebDAV file connections, Ervisio-to-Ervisio pairing, plugin network hosts) from one console.

## Entry points and public contracts
- `server/internal/modules/remote/remote.go:Register`: user-level `remote.list|save|forget|test|start|ls|stat|mkdir|create|rename|remove|readText|writeText`; streams `readStream|writeStream|copy`. All `rpc.User`, none admin.
- `remote/store.go`: `Conn`, `store` (`~/.config/ervisio/remotes.json` 0600 plus `remotes.key`), `maxConns=200`; `spec.go:build` validates input; `pool.go:pool.with/get` (max 24 open per session, idle close 5 min, FTP up to 4 slots, others 1 shared).
- Protocol files: `sftp.go` (pinned host key), `ftp.go`, `smb.go`, `webdav.go`; `fs.go:FS` interface; `copy.go` (cross-connection copy, `conn ""` = this machine); `local.go`.
- Pairing (server side): `server/internal/server/envpair.go` (`registerPair`: `/api/pair/info|redeem|status|revoke|bridge`, `pairAllowed`, `pairUser`) and `envs.go` (`envs.pairToken.create`, `envs.pairings.revoke`); logic in `server/internal/envs/pair.go` (`TokenTTL` 15 min, `CreatePairToken`, `Redeem`, `AuthPair`).
- Plugin network hosts: `server/internal/modules/plugins/userhosts.go` (`plugins.network.list` user, `plugins.network.revoke` admin), `netcap.go`; manifest `capabilities.network.userHosts`.
- Public host: `server/internal/server/public.go:publicHost` -> `GET /api/public/host`; web `api/types.ts:PublicHost` (`hostname`, `ip?`, `distro`, `sshKeys?`).
- Web: `sections/settings/Pairing.tsx` (`PairingBlock`, `TokenDialog`), `NetworkHosts.tsx` (`NetworkHostsBlock`), `Hosts.tsx` (`HostsBlock`, config key `hosts`), `settings/envs.ts` (`pairTokenCreate`, `pairingRevoke`, `hostsList`, `hostRevoke`); `sections/files/Places.tsx` (remote entry).
- Cross-origin: `web/src/api/base.ts` (`API_BASE` from `VITE_API_BASE`, `apiUrl`, `wsUrl`, `consoleOrigin`, `FETCH_CREDENTIALS`).

## Flow
- Remote files: browser `remote.save` -> secrets sealed (AES-GCM, `v1:`) -> `remote.start/ls/...` -> pool dials from the user bridge with the account's rights; secrets never return (`Conn.view` only reports whether set).
- SFTP trust-on-first-use: unpinned or changed key returns `conflict` with `reason: hostKey`, `fingerprint`, `changed`; UI re-saves with the pinned `hostKey`.
- Pairing: admin creates one-shot token (user chosen, default own) -> other server redeems at `/api/pair/redeem` (pins this server's TLS cert fingerprint) -> gets Bearer credential (stored as hash) -> opens `/api/pair/bridge` upgrade relaying user-bridge protocol filtered by `pairAllowed`.
- Revoke from either side closes live pair connections (`closePairConns`).
- Network hosts: plugin with `userHosts` asks for a host, an admin approves, `NetworkHostsBlock` lists and revokes (revoke needs unlock).

## Invariants
- Remote permission errors map to `forbidden`, never `needs_admin` (`remoteErr`): local admin rights do not help elsewhere.
- Remote credentials stay in the user bridge; the daemon and browser do not see them.
- A pairing runs as one local user under the same rules as sign-in (`allow_root`, `auth.allow_*`, dev user); it only reaches the user bridge, never root, and only `plugins.*` capability methods in `pairAllowed`.
- Pair endpoints use Bearer auth, not session or CSRF; failed credentials are rate limited per IP (`AuthBlocked`, `NoteAuthFail`). Pair calls are logged and tagged `via`; `via` is set only by the relay, never a browser.
- Pairing tokens: single use, 15 min, bounded open count; compare credentials in constant time.
- Build all API URLs through `api/base.ts`; cookies need `credentials: 'include'` when `API_BASE` is set.
- `Insecure` (skip TLS verify) exists only for FTP and WebDAV; `build` forces it false elsewhere.

## Gotchas
- Files now uses `remote.*` in both shells: `RemoteConnections.tsx` manages SFTP test/trust/save/forget and SMB network folders. SMB address parser `networkPath.ts` accepts UNC/smb://, rejects credential-bearing addresses and resolves host/share/subpath; no OS mount. Places opens saved connections. `remoteLocation.ts` + `fapi.ts` adapt encoded navigation paths, and `transfers.ts` supports remote uploads/downloads/copy/move. Secrets remain in the account's backend connection store, never prefs.
- `HostsBlock` edits the `hosts` config key, but `server/internal/config` has no `hosts` field; `supported={server.hasKey('hosts')}` hides the add button ("later").
- `remotes.key` sits beside `remotes.json` in the same user folder: sealing guards against casual leaks (backups, screenshots of the file), not against the account itself or root.
- Cross-origin `VITE_API_BASE`: session cookie is `SameSite=Strict` and the server sends no CORS headers, so it works only same-site (e.g. another port or a wrapper behind the same host) and the page origin must be in `web.allowed_origins`.
- Remote call can reach any host the user's account can (no private-range block); dial timeout only.
- Linux vs Windows: pairing and remote run in the user bridge on both; Windows details follow the bridge (see map/server.md). Tunnels are in `envs/tunnel_unix.go` vs `tunnel_windows.go`.
- Pairing fingerprint is empty when TLS is off (dev or `tls.mode=http`); the UI warns that pairing is then unpinned.
- `Hosts.tsx` and `NetworkHosts.tsx` are unrelated: SSH-style host list vs plugin-approved HTTP hosts.

## Commands and references
- `cd server && go test ./internal/modules/remote ./internal/envs ./internal/server` (`pair_test.go`, `envpair_test.go`, `security_test.go`).
- `docs/api/environments.md` (pairing), `docs/api/plugins.md` (network capability), `docs/api/auth.md` (`public/host`).
- `docs/api/remote.md` documents connection trust, storage, and file/transfer methods.
Related: map/auth.md, map/server.md, map/web.md

Sweep helpers (2026-11-05): checkoutPath, bindPathParams (remote.go); server/envpair.go relayPeerLine.
