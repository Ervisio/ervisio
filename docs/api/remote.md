# Remote file connections

The `remote.*` methods run in the signed-in user's bridge on Linux and Windows.
Connections support SFTP, FTP(S), SMB and WebDAV; Files exposes SFTP and SMB connection
creation and can browse existing connections of every supported protocol.
Local administrator rights never elevate permissions on another server.

`remote.list` returns `{connections: [...]}`. Each connection has `id`, `name`,
`kind`, address fields, `path`, and redacted credential flags (`hasPassword`,
`hasKey`, `hasPassphrase`). Secrets are never returned by any method.

## Save and verify SFTP

`remote.test` accepts unsaved settings, or `id` plus edited settings:

```json
{"name":"Staging","kind":"sftp","host":"server.example","port":22,
 "user":"deploy","auth":"password","password":"...","path":"/srv"}
```

For key authentication, use `auth:"key"`, `key` (OpenSSH/PEM content), and an
optional `passphrase`. Credential fields omitted while editing preserve the
saved value; an explicit empty string clears it.

Before authentication, an unknown host key returns `{ok:false, needsTrust:true,
hostKey, keyType, fingerprint, changed:false}`. Compare the fingerprint with the
server, then repeat with the explicitly accepted `hostKey`. A changed saved key
sets `changed:true` and requires a new explicit trust decision. No password or
authentication signature is sent to an unverified server.

A successful test returns `{ok:true,path}`. `remote.save` takes the same fields,
requires a pinned SFTP key and returns the redacted connection. Credentials are
AES-GCM sealed in the account's `remotes.json`; the adjacent `remotes.key` and
store are owner-only. This protects against disclosure of the JSON file alone;
the account and root can read the account's own credentials.

`remote.start {conn}` returns the start folder. `remote.forget {id}` removes the
saved connection and credentials, closes pooled connections, and leaves remote
files intact. Calls on a saved connection with a new server key return `conflict`
with `data.reason:"hostKey"`, `fingerprint`, and `changed`.

## File operations

SMB network folders use `kind:"smb"`, `host`, `port` (445 by default), `share`,
`user`, `password`, optional `domain` and `path` (absolute inside the share).
Files parses `\\\\server\\share\\subfolder` and `smb://server/share/subfolder`
into these fields. Credentials belong in separate masked fields, never the URL.
Test/save/start and the per-user credential store are shared with SFTP. A saved
network folder reconnects when opened; it does not mount a drive in Ubuntu or
Windows. SMB2/3 authentication is performed by the backend's SMB client.

Pass a saved connection id as `conn` and an absolute POSIX `path`:

- `remote.ls {conn,path,showHidden}` returns `{path,parent,entries,truncated}`.
- `remote.stat {conn,path}` returns the same entry shape as `files.stat`.
- `remote.mkdir {conn,path,parents}`, `remote.create {conn,path}`.
- `remote.rename {conn,from,to}` refuses an existing destination.
- `remote.remove {conn,paths}` permanently deletes selected files or trees;
  returns `{deleted,failed}` and has no local Trash. The UI confirms first.
- `remote.readText {conn,path,maxBytes}` returns text, encoding, mtime and
  truncation; `remote.writeText {conn,path,content,encoding,expectedMtime}` refuses
  stale edits with `conflict` unless `expectedMtime` is zero.

`remote.readStream` and `remote.writeStream` use the existing file transfer stream
protocol. Authenticated HTTP endpoints `/api/files/download` and
`/api/files/upload` accept `remote=<id>` alongside `path`; they always use the user
bridge. Uploads stream bytes rather than buffering a whole file in RPC.

`remote.copy` streams copy/move progress with these endpoints:

```json
{"from":{"conn":"saved-id","paths":["/srv/app"]},
 "to":{"conn":"","path":"/home/deploy/backup"},"move":false}
```

An empty `conn` names this machine under the user's permissions. At least one
endpoint must be remote. Local-to-local transfers use `files.copy`.

The frontend stores remote navigation as `remote:<id>:/absolute/path`, containing
only an id and path. Its adapter converts this to the API params above and never
passes remote paths to local operations or automatically requests local root.
Remote chmod/chown and local-terminal actions are unavailable because the remote
file protocol does not grant those operations.

## Capacity and project search

`remote.statfs {conn,path}` returns `{supported,path,total?,used?,free?,avail?}` with
bytes reported by SFTP `statvfs@openssh.com` or SMB2/3 filesystem information.
`avail` is available to the remote account; `free` may include reserved blocks.
It is filesystem capacity reported by the server, rather than a fabricated share
quota. Unsupported servers return `supported:false` without made-up zeros.
Files displays saved connections as network drives in This PC in both shells.

`remote.searchText` is a read-only stream accepting `{conn,root,query,
caseSensitive?,includeGenerated?,maxResults?}`. Its batches and resource limits
match `files.searchText` in `files.md`; paths are POSIX paths inside the saved
connection. Search never follows directory symlinks or requests local root.
FTP project-text search is explicitly unavailable because its legacy adapter
cannot guarantee interruption of a stalled login/data read. Existing FTP file
browsing and transfers are unchanged.

Capacity checks and project search use dedicated read-only connections, bounded
to four at once per user bridge and a 15-second deadline including connection
setup. Cancellation closes that owned connection to interrupt stalled reads;
it never closes another app's pooled file transfer connection.
