# Environments (envs.*)

An **environment** is a remote Docker host an administrator added in Settings > Environments. A plugin whose manifest
opts in (`remote: "docker"`, see [plugins.md](plugins.md)) can send its HTTP calls and its declared commands to one
instead of the local Docker socket. The first user is the Docker plugin.

## Who does what

```
browser ── /api/rpc, /api/ws ──▶ ervisiod (root)
                                   │ checks the access list, then plugins.envCheck on the user's bridge
                                   ├─ tcp-tls, ssh, portainer-agent:  tunnel socket  ──▶  the host
                                   │      user's bridge ── unix socket 0600 ──▶ ervisiod ── TLS / SSH / signed HTTPS ──▶ Docker
                                   └─ ervisio: remote bridge  ── pinned TLS, upgraded connection ──▶ the other ervisiod ──▶ its user bridge
```

* The **daemon** owns the configuration, the secrets and every connection. The methods below are answered by the daemon
  itself (not by a bridge), because the secrets must never be readable by a user's process. They need administrator
  rights (`needs_admin`, so the web client opens the unlock dialog); `plugins.envs.list` is for every user. A
  `--dev-insecure-noauth` daemon (loopback only, never root) counts every session as an administrator, as it has no
  password to unlock with.
* For **tcp-tls, ssh and portainer-agent** the daemon creates, per user and environment, a unix socket
  `<tunnel dir>/<uid>/<env id>.sock` (`/run/ervisio/tunnels` by default, `--tunnel-dir`; at most 100 characters). The
  socket is `0600` and owned by the user, so only that user's processes (and root) can connect. The folders are root's,
  `0711`: the user reaches its socket but cannot list, add or replace anything there, so it can never slip a symlink
  under the daemon (the socket is bound in a root-only staging folder, given its owner and mode there, renamed into
  place and checked with `lstat`). A folder left by an older version (the user's own) is removed and made again.
  The daemon puts the path in the `envSocket` param of the call it forwards to the user's bridge (it removes any
  `envSocket` the browser sent, in any spelling: `EnvSocket` and `ENV` count, as JSON field names match without regard
  to case; `via` too), and the bridge uses it instead of the capability's socket, or puts `unix://<path>` in
  the command's `{env}` argv item. Whatever connects speaks the Docker HTTP API (including connection upgrades, so
  `docker exec`, `attach` and `compose` work); the daemon carries the bytes. A tunnel is closed after 10 minutes without
  connections, when the environment changes or is removed. The Docker CLI sees errors as a `502` with a JSON message.
* Calls run **as the user, never as root**, on the user's bridge. A call that names an environment skips the
  `admin`/`adminUnlessGroup` check of the capability (those protect the local socket). What a user may do on the remote
  host is limited by the capability's rules (methods, paths, headers) and by what the remote account may do.
* Commands run **on this machine** against the remote engine: compose files, build contexts and bind mounts are
  resolved here, the containers run there. For an Ervisio environment the call runs **on the other server**.

## Kinds

| Kind | The daemon reaches the host through | Needs on the host |
|---|---|---|
| `tcp-tls` | TCP and TLS to `host:port`, client certificate | `dockerd --tlsverify -H tcp://…:2376` |
| `ssh` | `golang.org/x/crypto/ssh`, channel `direct-streamlocal@openssh.com` to the Docker socket | sshd with a key for a user who may use Docker; **no Docker CLI** |
| `portainer-agent` | HTTPS to the agent (port 9001), requests signed | the Portainer agent |
| `ervisio` | another Ervisio server, paired with a token | Ervisio, the plugin installed |

Every kind has: a name, an access list, a health check every 45 seconds (`GET /version`: engine and API version,
latency, error text) and an on-demand check (`envs.test`). Connections have a 10 s connect timeout; SSH keeps one
pooled connection per environment (keepalive every 30 s, redial when it breaks) and opens one channel per call; TLS
connections use session resumption.

### `tcp-tls`

* Fields: `address` (`host:port`), `ca` (PEM, optional), `clientCert` and `clientKey` (PEM, both or neither),
  `fingerprint` (pin), `skipVerify`, `insecure`.
* Server trust, one of: the **CA** (chain and host name checked; with no CA the machine's trust store), a **pinned
  fingerprint** (`sha256:<hex>` of the leaf certificate, shown by "Check connection" and confirmed by the admin; any
  other certificate is refused), or **skip verification** (explicit choice, off by default, flagged in the list).
* **Plain tcp** (`insecure`) needs its own checkbox and warning: the connection is readable and anyone on the path
  can run containers, which is root on the host. TLS settings are dropped when it is on.
* PEM is uploaded or pasted in Settings. The private key is stored sealed (below) and never shown again.

### `ssh`

* Fields: `address`, `user`, `sshKey` (PEM or OpenSSH private key), `passphrase`, `socketPath` (default
  `/var/run/docker.sock`), `hostKey`.
* **Host key pinning on first connect**: "Check connection" connects and shows the host's key fingerprint
  (`SHA256:…`) and key; the admin compares it with the host's own and confirms; the key is stored and every later
  connection must present exactly that key (a different one fails with both fingerprints and a hint). An environment
  cannot be saved without a pinned key.
* **Passphrase: stored, encrypted** (AES-256-GCM with the daemon's `master.key`), not asked at use. Reason: environments
  are used by background work (health checks, later jobs and webhooks) when nobody is there to type it, and a prompt
  would put the passphrase in a browser. An unencrypted key is accepted too. Either way a stolen `envs.json` alone
  reveals nothing; stealing `master.key` as well (same folder, root only) does, as it would for any daemon-held secret.
  Prefer a dedicated key with a restricted account.
* sshd must allow forwarding to a socket. Both `AllowStreamLocalForwarding yes` (or `local`) **and**
  `AllowTcpForwarding yes` (or `local`) are needed: with `AllowTcpForwarding no`, which many distributions ship,
  sshd refuses the channel with the unhelpful "open failed" (found testing against Alpine's sshd). A key restricted
  with `no-port-forwarding` is refused too. The remote account must be able to open the socket (the `docker` group).
  The error text names both causes.

### `portainer-agent`

The Docker API is proxied by the Portainer agent over HTTPS on port 9001. The agent's certificate is self-signed, so the
certificate is **pinned on first connect** exactly like an SSH host key.

**Signature scheme** (from `portainer/agent`, `http/security/notary.go` and `crypto/ecdsa.go`; verified against the
real agent):

```
X-PortainerAgent-PublicKey: hex( DER PKIX of an ECDSA P-256 public key )
X-PortainerAgent-Signature: base64-raw( r || s )      each 32 bytes, zero padded
                            ECDSA over md5(message)   message = "Portainer-App", or the agent's AGENT_SECRET
```

The daemon makes one P-256 key (stored sealed) and signs every request with it. An agent **without `AGENT_SECRET` binds
itself to the first public key it sees** and then refuses every other key (403): the first server to talk to it owns it.
With an `AGENT_SECRET`, the agent accepts any key whose signature covers the secret, so several servers can share one
agent. Hence the advice to always set `AGENT_SECRET` on the agent and give it to the environment (`agentSecret`,
stored sealed). A 403 explains this in the status.

Limit: the agent's proxy **drops upgraded connections** (the agent answers 101, then closes the stream), so attach, `exec`
and a foreground `docker run` do not work through it. The tunnel answers those with a `501` at once instead of
hanging. Use SSH or TLS when interactive streams are needed; everything else (API calls, logs, `compose up -d`) works.

### `ervisio`: another Ervisio server

Calls from A execute on B, by B's own daemon, under B's rules. The plugin must be installed on B.

**Pairing**

1. An admin on B opens Settings > Environments > Pair another server, picks the **Linux user** the pairing will run as
   (default: themselves) and creates a **token**: `ept_…`, 160 bits, single use, valid 15 minutes, kept in memory
   only. The dialog shows B's TLS certificate fingerprint (plain-http servers have none).
2. An admin on A adds an environment of kind Ervisio: B's address, "Check connection" (A connects and shows the
   fingerprint it sees; the admin compares it with the one B showed and confirms), then pastes the token.
3. A POSTs `/api/pair/redeem {token, name}` to B over the **pinned** TLS connection. B returns a **credential**
   (`epc_…`, 192 bits) and its pairing id. B stores only `sha256(credential)` with the user, who created the token,
   and A's name; A stores the credential sealed.
4. Either side can **revoke**: B in its pairing list (live connections are cut at once), A by removing the environment
   (which also tells B, best effort). A revoked pairing fails with a clear message on the other side.

HTTP endpoints on B (no session; rate limited: 10 failures per address per 15 minutes):
`GET /api/pair/info` (is this Ervisio, name, version), `POST /api/pair/redeem`, `GET /api/pair/status` (Bearer: is my
credential still valid, no bridge started), `POST /api/pair/revoke` (Bearer), `GET /api/pair/bridge` (Bearer,
`Upgrade: ervisio-bridge/1`, below). http to a non-loopback address is refused unless the admin ticks "Allow plain
http" (the credential and calls would travel unencrypted).

**The channel.** After the 101 answer the connection carries the same line protocol as a bridge's stdio (see
`docs/ARCHITECTURE.md`): B starts the pairing user's own user bridge, relays its hello line to A, and relays A's lines
to it after a filter. A's daemon wraps the connection as a bridge process (`bridge.NewRemote`), so streams, flow
control, cancellation and binary chunks work exactly as for local bridges, for every method, with no special cases.
The filter lets through **only** `plugins.http`, `plugins.httpStream`, `plugins.exec`, `plugins.execStream`,
`plugins.pty`, the transfer methods `plugins.httpDownload`, `plugins.httpUpload` and `plugins.execDownload`, and the plugin files
methods `plugins.readFile`, `writeFile`, `listDir`, `mkdir` and `remove` (`pairAllowed` in `server/internal/server/envpair.go`); anything else
(`plugins.list`, `plugins.install`, the console's own files and terminal, config, ...) is answered `forbidden` without reaching the bridge.
A call is also checked on A first (`plugins.envCheck`: plugin usable, capability opted in) and then by B's bridge
against B's manifest, signature policy, visibility and the capability rules.

**Security model**

* The credential is **scoped to the Docker plugin capabilities and declared commands of the plugins installed on B**,
  as one Linux user chosen at pairing time. It cannot reach another method, and the user's bridge is never the root
  bridge: no administrator rights, whatever A's user can do there. B refuses to run a pairing as root unless
  `allow_root` is on, and as a user that `auth.allow_users`/`allow_groups`/`admins_only` would not let sign in
  (checked at token creation and at every connection).
* What a paired server can do is **what the mapped user can do through B's plugins**: with the Docker plugin that is
  whatever the Docker API allows that user, which on most machines is root-equivalent. Pair only servers you control,
  and map to a user with only what you want to share.
* The pairing is authenticated by the credential (TLS-pinned; Bearer on an HTTP upgrade). B's audit trail: every
  connection and every call is logged by the daemon as `pairing: via <A's name> by <A's user> -> <local user>
  <method> <plugin>/<capability or command>`, and the same text is added to the call's params as `via` for the
  activity log of the core (which reads it when it lands).
* Access on A: the environment's access list decides who on A may use the pairing; A's own plugin visibility applies.
* Revocation on either side takes effect on the next call (live connections are closed).

## Storage and secrets

`/var/lib/ervisio/envs/` (`--envs-dir`; dev default `~/.local/state/ervisio-dev/<port>/envs`), mode `0700`, root only:

* `envs.json` (`0600`, written atomically): environments (public fields), pairings (hash only), the sealed Portainer key.
* `master.key` (`0600`, 32 random bytes): seals every secret (client key, SSH key, passphrase, agent secret, pairing
  credential, the Portainer signing key) with AES-256-GCM, so a copy of `envs.json` alone reveals none. If the key is
  replaced the daemon refuses to open the store rather than serve garbage. Back both files up together or neither.

An `Env` never serializes a secret: they are kept outside its fields, API views carry only `hasSecrets: {clientKey:
true, …}`, and a secret left empty on update is kept. The web client sends secrets only when typed.

## Access list

`access: {mode: "all" | "restricted", users: [...], groups: [...]}`. `all`: everyone who can use the plugin (the plugin's
`visibleTo` still applies). `restricted`: the listed users, or members of the listed groups (primary or supplementary,
as in `auth.allow_users`/`auth.allow_groups`). A user without access does not see the environment in
`plugins.envs.list` and gets `forbidden` if they name its id.

## Methods

Answered by the daemon. Admin unless noted. Errors: `invalid` (message fit for the UI), `not_found`, `needs_admin`,
`unavailable` (the store could not be opened; see the daemon log).

| Method | Params | Result |
|---|---|---|
| `plugins.envs.list` (user) | `{}` | `[{id, name, kind, address, status}; `address` is the non-secret display address (see plugins.md)]` for the caller |
| `envs.list` | `{}` | `{envs:[View], pairings:[{id,name,user,createdBy,created,lastUsed,lastVia,lastAddr}], openTokens, server}` |
| `envs.probe` | `{kind, address, user?, insecure?}` | `{fingerprint, hostKey?, plain?, kind}`: connects and shows what to confirm |
| `envs.create` | `Input` (below) | the View with its first status. Saved even if the host is down (the status says why) |
| `envs.update` | `{id, ...Input}` | the View; empty secrets stay; closes tunnels |
| `envs.delete` | `{id}` | `{id}`; Ervisio kind: revokes on the other server |
| `envs.test` | `{id}` | the Status now (for Ervisio: connects through a bridge and asks Docker's version there) |
| `envs.pairToken.create` | `{user?}` | `{token, expires, user, fingerprint, server, ttlMinutes}` |
| `envs.pairings.revoke` | `{id}` | `{id}` |

`Input`: `{name, kind, address, user?, socketPath?, insecure?, skipVerify?, ca?, clientCert?, fingerprint?, hostKey?,
access?, clientKey?, sshKey?, passphrase?, agentSecret?, token?}`. `View`: the stored public fields plus `hasSecrets`
and `status: {reachable, engineVersion?, apiVersion?, latencyMs, error?, checked}`.

## Files on a paired server

The files capability of a plugin (`sdk.files.*`, `capabilities.files`) takes the `env` option too, for environments of kind
`ervisio` only (the Docker plugin uses it to create and edit stacks in the paired server's `/opt/stacks`). The call
is relayed like the others and **executed by B's user bridge under B's manifest** for the mapped user: B's folders,
limits and rules decide, A's manifest is only a first filter. Administrator folders follow the rule commands already
follow over a pairing: they work only if the mapped user is root or a member of the folder's `adminUnlessGroup`, else
`needs_admin`; a pairing never reaches the root bridge, so nothing is escalated. For `tcp-tls`, `ssh` and
`portainer-agent` the option is refused with a clear message (files stay local there). Writes, folder creations and
removals are in the activity log of both servers (A: `env`; B: `origin` = `via <server> by <user>`); reads are not
logged. The `env` and `via` keys are stripped from a browser's params in any case, as for the other methods.

## Using it from a plugin

See the SDK (`sdk.envs.list()`, the `env` option of `api.http`, `httpStream`, `exec`, `execStream`, `pty`). The web
broker checks `env` (an `env-xxxxxxxx` id, the capability's `remote`) before the daemon does. A call with an `env`
for a capability or command that does not declare `remote` fails with `forbidden`.

## Known limits

* Large transfers (`sdk.api.download`, `downloadCommand`, `upload`) take the same `env` option as `http` and `exec`.
  The daemon resolves it for the transfer request as for any other call (access list, plugin opt-in, tunnel socket or
  paired server) and the bytes go through the tunnel like `httpStream`. The capability or command must declare `remote`.
* Plugin jobs (`docs/api/jobs.md`) cannot target an environment yet: a job step has no `env` parameter and always runs on
  this machine.
* The paired server writes its own activity log entries for what it runs for another one (`origin` = `via <server> by
  <user>`); the first server records the same calls with `env`.
* Portainer agents cannot carry upgraded connections (above).
* **Streams through a Portainer agent arrive in 4 KiB steps.** Tested against `portainer/agent` 2.45 with `curl` straight
  to the agent (signed headers) and through the tunnel: the agent copies a Docker stream into its own HTTP response with
  a 4 KiB write buffer and never flushes (`http/proxy/local.go` in the agent), so `logs?follow=1`, `stats?stream=1` and
  `/events` deliver nothing, not even the response headers, until 4096 bytes are ready or the stream ends. A quiet
  container's log can sit in that buffer indefinitely. This is the agent, not the tunnel; use SSH or TLS when live
  output matters. (The tunnel itself had a related fault, fixed: Go's chunked reader waits to fill its whole buffer
  inside an HTTP chunk, and the agent sends a stream as one big chunk, so the 32 KiB copy buffer added up to 32 KiB of
  extra delay on top of the agent's. It now copies with a 1 KiB buffer; `TestAgentTunnelFlushesEachChunk`.)
* The remote bridge of an Ervisio environment is started on first use and closed after 5 minutes without calls.
* Docker Swarm node targeting (`X-PortainerAgent-Target`) is not supported.

## Windows notes

Tunnels are AF_UNIX sockets (Windows 10 1803+), under `<run dir>\tunnels` (`--tunnel-dir`; at most 100 characters),
`<tunnel dir>\<rid>\<env id>.sock` where `<rid>` is the account's RID. Access is by ACL instead of uid/mode: the tunnel
dir has a protected DACL (SYSTEM, the daemon's user and Administrators full; Authenticated Users traverse only); the
per-user folder has a protected DACL with SYSTEM, the daemon's user and the account's SID (read and traverse only, so
it cannot add or replace entries); the socket is staged in a SYSTEM-only folder, given a DACL (SYSTEM, daemon's user,
the account's SID read/write), renamed into place and verified. Every object is inspected on a handle opened without
following reparse points: owner SYSTEM, the daemon's user or Administrators, no ACE for any other SID (a folder or
socket that fails is removed and made again). Not verified on a real Windows host: that the account can connect to the
socket with exactly these rights, and that `SetSecurityInfo` on the AF_UNIX reparse file works, are untested.
