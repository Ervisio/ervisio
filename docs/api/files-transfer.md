# File transfer streams (files.readStream / files.writeStream)

linuxadmind implements `GET /api/files/download` and `POST /api/files/upload` by streaming
to/from two **bridge stream methods** that the Files section implements in
`server/internal/modules/files`. Both are `rpc.User` level (the daemon sends them to the root
bridge when the request has `admin=1`). A reference implementation used by the daemon tests is
in `server/internal/server/testdata/fakebridge/main.go`.

Flow control is handled by the rpc layer (`Stream.Send` blocks while the window is full), so
handlers just loop.

## files.readStream

Params: `{"path":"/abs/file"}`

Events, in order:
1. metadata (JSON, not b64): `s.Send({"name":"file.txt","size":1234,"mime":"text/plain"})`
   — `size` is the exact byte count that will follow (`-1` if unknown, e.g. a pipe);
   `mime` may be `""`.
2. zero or more chunks: `s.SendBytes(chunk)` → `{"event":"data","data":"<base64>","b64":true}`;
   use ≤ 64 KiB raw per chunk.
3. completion: `s.Send({"done":true})`, then return nil (→ `end`).

Errors (return `rpc.Errorf(...)` / an `fs` error) before the first event become an HTTP error
response (`not_found` → 404, `needs_admin` → 403 …). After headers are sent, a missing
`{"done":true}`, an error or a byte count different from `size` aborts the HTTP connection so the
browser never keeps a truncated file.

HTTP side: `GET /api/files/download?path=/abs/file&admin=0|1[&inline=1]` →
`Content-Disposition: attachment; filename*=UTF-8''<name>`, `Content-Length` from `size`.
`inline=1` is honoured only for safe preview types (images, pdf, text/plain, audio/video); the
response always carries `Content-Security-Policy: sandbox`.

## files.writeStream

Params: `{"path":"/abs/target","size":1234,"overwrite":false}` — `size` is the request's
Content-Length, `-1` if unknown.

Inputs (read from `s.Input()`):
- `{"data":"<base64>"}` — next chunk (≤ 64 KiB raw)
- `{"eof":true}` — end of the body; finish the file now.

Events:
- `s.Send({"done":true,"size":<bytes written>,"path":"/abs/target"})` after `eof`, then return nil.
  Extra fields are passed through to the HTTP response.
- Fail early with `conflict` when the target exists and `overwrite` is false; with `not_found`,
  `needs_admin` (EACCES) etc. otherwise.

Implementations should write to a temporary file in the target directory and rename it on
`eof`, removing it if the stream is cancelled (ctx done / Input closed).

HTTP side: `POST /api/files/upload?path=/abs/target&admin=0|1&overwrite=0|1`, raw body,
header `X-Requested-With: linuxadmin` → 200 `{"result":{"done":true,"size":…,"path":…}}` or an
error (`conflict` → 409, …).
