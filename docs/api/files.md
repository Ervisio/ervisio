# files.* (file manager)

Package `server/internal/modules/files`. Every method is **user level** except `files.chown`.
Run a user method as root by calling it with `admin:true` (the web client does this after the
user unlocked administrator rights). A permission error on the user bridge answers `needs_admin`;
on the root bridge it answers `forbidden`.

Paths must be absolute (`invalid` otherwise); they are cleaned (`/a/b/../c//` becomes `/a/c`) and must not
contain NUL. Times are Unix **milliseconds**. Messages are written for people and shown as they are.

### Links and the root bridge
Every method that changes something (mkdir, create, rename/move, copy, delete, Trash, chmod, chown, writeText,
writeStream) opens the parent folder once and works on the last name with `*at()` calls that never follow a link
(`server/internal/modules/files/safefs.go`). Recursive chmod/chown, copy and delete walk the tree with directory
descriptors (`O_NOFOLLOW|O_DIRECTORY`), so a folder swapped for a link during the walk is never entered.
On the **root bridge** the parent folder itself is resolved one component at a time and only links owned by root
are followed: a path that goes through another user's link (for example `/home/bob/link/file`) answers
`forbidden` — open the folder the link points to instead. The user bridge resolves paths normally.

## Entry

Returned by `files.list`, `files.stat`, `files.search`, `files.places` (recent) and inside `files.trashList`.

```json
{"name":"notes.md","path":"/home/u/notes.md","type":"file","size":59,"mode":"0644","perm":"rw-r--r--",
 "owner":"u","group":"u","uid":1000,"gid":1000,"mtime":1790880203665,
 "target":"docs","targetType":"dir","mime":"text/markdown"}
```
- `type`: `file | dir | symlink | other`. `mode` is octal with a special-bits digit (`0755`, `4755`, `1777`);
  `perm` is the `ls` string without the type letter (`rwxr-xr-x`, `rwsr-xr-x`).
- `path` is present for `stat`, `search`, recent items and Trash items (in `list` it is omitted: join `path` + `name`).
- `target`/`targetType` only for symlinks (`targetType` is empty when the link is broken). `mime` is guessed from the
  extension and only set for files and links to files.
- `size` is the size of the object itself (not of the folder contents).

## Methods

| Method | Params | Result |
|---|---|---|
| `files.list` | `{path, showHidden?}` | `{path, entries:[Entry], parent, truncated}` (`parent` is `""` for `/`; folders first, then by name; at most 50 000 entries, then `truncated:true`). A symlink to a folder can be listed. A file path answers `invalid`. |
| `files.stat` | `{path}` | `Entry` (with `path`, not following a final symlink) |
| `files.mkdir` | `{path, parents?}` | `Entry`. `conflict` if it exists. |
| `files.create` | `{path}` | `Entry` of the new empty file (exclusive create, `conflict` if it exists) |
| `files.rename` / `files.move` | `{from, to}` | `Entry` at `to`. Never overwrites (`conflict`). Moving across file systems copies, then removes the source. A folder cannot be moved into itself. |
| `files.copy` (stream) | `{from:[path], to:dir, move?}` | see below |
| `files.delete` | `{paths:[path], trash}` | `{deleted:[path], failed:[{path,message,code}]}` |
| `files.trashList` | `{}` | `{items:[{id, originalPath, deletedAt, entry}]}` newest first |
| `files.restore` | `{ids:[id]}` | `{restored:[path], failed:[{path(=id),message,code}]}` (`conflict` per item if the original path exists; missing parent folders are created) |
| `files.trashDelete` | `{ids:[id]}` | `{deleted:n}` |
| `files.trashEmpty` (alias `files.empty`) | `{}` | `{deleted:n}` |
| `files.chmod` | `{path, mode:"755", recursive?}` | `{changed:n, warning?}`. `mode` is an octal string of up to 4 digits. Symlinks are refused (`invalid`); recursion never follows or changes links. |
| `files.chown` (**admin**) | `{path, owner?, group?, recursive?}` | `{changed:n}`. Names or numeric ids; an empty field leaves it unchanged. Uses `fchownat(AT_SYMLINK_NOFOLLOW)` (links themselves are changed, never their targets). |
| `files.readText` | `{path, maxBytes?}` | `{path, content, encoding, size, mtime, truncated}` |
| `files.writeText` | `{path, content, encoding?, expectedMtime?}` | `{path, size, mtime}` |
| `files.search` (stream) | `{root, query, maxResults?}` | see below |
| `files.places` | `{}` | `{home, recent:[Entry], disks:[Disk], trashCount}` |
| `files.thumbnail` | `{path, size?}` | `{mime:"image/png", data:<base64>, width, height, origWidth, origHeight, format}` |
| `files.readStream` / `files.writeStream` (streams) | | see `files-transfer.md` |

### Deleting and the Trash
`trash:true` moves items to the freedesktop Trash of the account running the bridge
(`~/.local/share/Trash/{files,info}` with a `.trashinfo` file; names are made unique). Items on other
file systems are copied into it. `trash:false` removes for good (`unlinkat`/descriptor walk: **symlinks are removed, never followed**).
Protected folders (`/`, `/etc`, `/usr`, `/var`, `/home`, `/root`, `/boot`, ...) and the account's home folder are refused
(`forbidden`). If nothing could be deleted, the first error is returned as the call's error (so `needs_admin` reaches
the client); otherwise partial failures are listed in `failed`. The client uses permanent deletion (after a confirmation)
when browsing as administrator.

### files.copy (stream)
Params `{from:[...], to:"/folder", move:false}`. A target name that exists is never overwritten: the copy gets
`name (copy).ext`, `name (copy 2).ext`... Symlinks are copied as links, sockets/devices are skipped, mode and
modification time are kept. With `move:true` each source is renamed when possible, else copied and removed.
Events:
```
{"type":"start","total":<bytes>,"items":<n>}
{"type":"progress","done":<bytes>,"total":<bytes>,"current":"<name>"}   // about 10 per second
{"done":true,"paths":["/new/path", ...]}                                // then the stream ends
```
Cancel by closing the stream: the partial copy of the current item is removed.

### files.readText / files.writeText
`readText` refuses non-regular files and binary data (NUL bytes in the first 8 000 bytes, UTF-16 BOMs) with `invalid`.
`maxBytes` defaults to 1 MiB (cap 8 MiB); `truncated:true` means the file is longer (do not offer saving it).
`encoding` is `utf-8`, `utf-8-bom` or `latin1` (used when the bytes are not valid UTF-8); send it back to `writeText`
to keep it. `writeText` writes a temp file in the same folder (`O_EXCL`) and renames it (mode and, as root, owner are
kept; if the folder is not writable but the file is, it writes in place with `O_NOFOLLOW`). Saving a link saves the
file it points to; on the root bridge only links owned by root are followed (`forbidden` otherwise). `expectedMtime` (from `readText`) makes it answer
`conflict` with `data:{mtime}` when the file changed since; `0` or missing skips the check. A non-zero value for a file
that no longer exists also answers `conflict`.

### files.search (stream)
Case-insensitive substring match on the file name (or a glob when the query contains `*`, `?` or `[`). Symlinks are not
followed; `/proc /sys /dev /run` are skipped. Events:
```
{"entries":[Entry, ...]}                         // batches (path is set)
{"done":true,"count":n,"truncated":bool}         // truncated when maxResults (default 200, max 5000) was reached
```

### files.places
`home` is the account's home. `recent` comes from `~/.local/share/recently-used.xbel` (newest 40 that still exist, no
folders). `disks`: `[{mount, device, fstype, total, used, avail}]` in bytes for real block devices and network file
systems (loop devices and pseudo file systems are left out; one entry per device, shortest mount point).

### files.thumbnail
PNG, JPEG and GIF only (Go standard library), input up to 25 MiB and 50 megapixels, output fitted inside
`size` x `size` (32 to 640, default 256) without enlarging. Other formats answer `invalid`. WebP and SVG should be shown
by the browser through `GET /api/files/download?...&inline=1`.

## Errors
`not_found`, `conflict`, `invalid`, `forbidden` (protected folder, or permission denied as root), `needs_admin`
(permission denied as user), `unavailable` (cancelled).

## Windows notes

- Paths are Windows paths (`C:\Users\me`). The bridge runs as the signed-in user, so ACLs are enforced by the OS. Links (symlinks, junctions) are never followed at the final path component: creates use exclusive creation, saves and uploads write a temp file in the same folder and rename it over the target (`MoveFileEx` replace + write-through, which replaces a link rather than writing through it), deletes remove links without descending into them.
- `files.copy`: links inside a copied tree are skipped and listed in the final message's `skipped` array; a link given as a source is refused. Moving across drives copies then deletes, and is refused when anything would be skipped.
- `files.chmod`: only the read-only attribute exists. The owner write bit decides (`644` writable, `444` read-only); setuid/setgid/sticky and modes without owner read are `invalid`. A folder needs `recursive: true` (applies to the files inside).
- `files.chown`: `unavailable` (ownership uses ACLs).
- `files.places`: `home` is `%USERPROFILE%`; `disks` lists fixed drives (mount `C:\`, device = volume label); `trashCount` is 0. Trash methods and `delete` with `trash: true` return `unavailable` (no Recycle Bin integration); use permanent delete.
- Entry `owner` is `DOMAIN\name` (best effort); `uid`/`gid`/`group` are empty. Protected paths: drive roots and system folders (Windows, Program Files, ProgramData, Users, the profile folder).
- Not verified on a real Windows host: cross-compiled and vetted only.
