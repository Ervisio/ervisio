# Files
Purpose: dual-pane file manager (browse, edit, copy/move, trash, upload/download, previews) over the `files.*` bridge methods.

## Entry points and public contracts
- Backend `server/internal/modules/files/files.go:Register`. User level: list stat mkdir create rename move delete chmod readText writeText places thumbnail trashList restore trashDelete empty/trashEmpty (aliases).
- Admin level: only `files.chown` (Windows: Unavailable, ACLs).
- Streams (User): `files.copy` (also used for move), `files.search`, `files.readStream`, `files.writeStream`.
- Shared with `modules/remote`: `export.go` (ModeString, TypeOf, MimeOf, DecodeText/EncodeText, CheckNotProtected, MaxReadText). Remote imports files, never the reverse.
- HTTP: `server/internal/server/files.go:handleDownload` (GET `/api/files/download`) and `handleUpload` (POST `/api/files/upload`, needs `X-Requested-With`, CSRF). They wrap `files.readStream` / `files.writeStream`, or `remote.*Stream` when `remote=<id>` is given (`fileTarget`).
- Frontend `web/src/sections/files/`: `index.tsx` page (tabs, optional split panes); `Pane.tsx`; `data.ts` loaders (list/trash/recent/starred/search); `fapi.ts` `fcall` + thumbnail/snippet loaders; `transfers.ts` queue; `dropfiles.ts` folder drops.
- Prefs keys: `files.tabs`, `files.bookmarks`, `files.starred`, `files.showHidden`, `files.remotes`.

## Flow
- Browse: `fcall('files.list')`; pseudo-locations `trash:`, `recent:`, `starred:` map to `trashList` / `places` / `stat`.
- Elevation: `fcall` tries the user bridge with `noUnlock`; on `needs_admin` it opens the unlock dialog and repeats once with `admin:true` (`onElevate` flips the pane to root).
- Upload: `enqueueUpload` -> XHR POST raw body to `uploadUrl` (max 2 concurrent, `MAX_UPLOADS`); 409 = conflict, offers replace (`overwrite=1`). Server re-chunks 64 KiB to `writeStream` as base64 `{data}`, then `{eof}`.
- Writes go to `.upload-*` temp in the target dir, fchmod, then rename (unix: `renameNoReplace` unless overwrite; windows: `os.CreateTemp` + rename). Existing file mode is preserved on overwrite.
- Download: browser navigates to `downloadUrl`; stream sends meta `{name,size,mime}`, then 64 KiB byte frames, then `{done}`. Folders are not downloadable (UI skips them).
- Copy/move: `enqueueCopy` opens `files.copy`; events `start` / `progress` / `done`; cancel closes the stream.
- Thumbs: `Thumb.tsx` lazy via IntersectionObserver -> `loadThumb` (`files.thumbnail`, size 320, PNG base64); text cards use `readText` maxBytes 1200. Loaders cap 3 concurrent, 400 cached.
- Trash: freedesktop layout (`trash.go`, files + info dirs, `trashDateLayout`).

## Invariants
- All paths must be absolute and NUL-free (`cleanPath`); batch ops limited to 10000 paths; names via `validName`.
- `checkNotProtected` blocks delete/move of `/`, `/etc`, `/home`, ... (windows: SystemRoot, ProgramFiles, ProgramData, PUBLIC, USERPROFILE).
- Unix mutations use dirfd `*at()` calls via `safefs_unix.go` (`openParent`, `walkAt`) and never follow links; root bridge (`strictLinks`, euid 0) only follows root-owned links in path components, else `errUntrustedLink`.
- Thumbnails: PNG/JPEG/GIF only, input <= 25 MiB, <= 50M pixels, size clamped 32..640. Text read/write limit 8 MiB (`maxReadText`); binary refused.
- Inline serving only for `safeInline` types (server/files.go); everything else is an attachment.

## Gotchas
- `files.move` is routed to `hRename`; cross-directory move of trees is done through `files.copy` with `move:true`.
- `files.delete` with `trash:true` moves to trash; permanent delete uses `removePath` (no symlink following).
- Windows: `safefs_windows.go` `errUnsupported` for dirfd primitives; copies never follow reparse points; no chown. Unix/windows variants split by build tags: `*_unix.go`, `*_windows.go`.
- Frontend `downloadUrl` / `uploadUrl` have no `remote` parameter and Places shows remote entries as "soon": remote is backend-ready but not wired in this UI.
- Thumb cache key is `admin:path:mtime:size`; user and root views cache separately.
- Upload `status 403` maps to `needs_admin` on the client when no JSON error body is present.
- Large bodies bypass the 16 MiB RPC line limit only because transfers are streams; do not use `writeText` for big files.

## Checks and references
- `cd server && go test ./internal/modules/files/` (`files_test.go`, `safefs_test.go` race hooks, `winlogic_test.go`); `server/internal/server/files_test.go` for HTTP upload.
- `cd web && npm run typecheck && npm run i18n:check` (dictionaries `web/src/i18n/{en,it}/files.json`).
- Docs: `docs/api/files.md`, `docs/api/files-transfer.md`, `docs/api/remote.md`, `docs/SECURITY-REVIEW.md` (H1).

Related: map/server.md, map/web.md

Sweep helpers (2026-11-05): bindPath, cachedName (files/entry.go); openParentStat (ops_unix.go).
