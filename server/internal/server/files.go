package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Transfer chunk size for uploads (raw bytes per input message).
const uploadChunk = 64 << 10

// safeInline lists types that may be shown inline (previews). Anything else
// is always served as an attachment.
var safeInline = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true,
	"image/bmp": true, "application/pdf": true, "text/plain": true,
	"video/mp4": true, "video/webm": true, "audio/mpeg": true, "audio/ogg": true, "audio/wav": true,
}

// fileTarget is what a download or upload URL names: a local path (on the
// user or root bridge), or with remote=<id> a path on a saved remote
// connection (always on the user bridge, docs/api/remote.md).
type fileTarget struct {
	path   string
	admin  bool
	remote string
}

func (t fileTarget) method(local, remote string) string {
	if t.remote != "" {
		return remote
	}
	return local
}

func (t fileTarget) params() map[string]any {
	m := map[string]any{"path": t.path}
	if t.remote != "" {
		m["conn"] = t.remote
	}
	return m
}

func validRemoteID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// validTransferPath reports whether p is an acceptable absolute path, in the
// flavour of the target: POSIX for a remote server, the local one otherwise.
func validTransferPath(p string, remote bool) bool {
	if p == "" || len(p) > 4096 || strings.ContainsRune(p, 0) {
		return false
	}
	if remote {
		return path.IsAbs(p)
	}
	return filepath.IsAbs(p)
}

func transferParams(r *http.Request) (fileTarget, *rpc.Error) {
	q := r.URL.Query()
	t := fileTarget{path: q.Get("path"), remote: q.Get("remote")}
	if t.remote != "" && !validRemoteID(t.remote) {
		return t, rpc.Errorf(rpc.Invalid, "remote must be a connection id")
	}
	if !validTransferPath(t.path, t.remote != "") {
		return t, rpc.Errorf(rpc.Invalid, "path must be an absolute path")
	}
	switch q.Get("admin") {
	case "", "0", "false":
	case "1", "true":
		// Administrator rights here do not reach another server.
		t.admin = t.remote == ""
	default:
		return t, rpc.Errorf(rpc.Invalid, "admin must be 0 or 1")
	}
	return t, nil
}

// fileMeta is the first event of files.readStream.
type fileMeta struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Mime string `json:"mime"`
}

// readDownloadMeta waits for the first event of a read stream and decodes it.
func readDownloadMeta(st *rpc.ClientStream, method string) (fileMeta, *rpc.Error) {
	var meta fileMeta
	ev, ok := <-st.Events()
	if !ok {
		if err := st.Err(); err != nil {
			return meta, rpc.ToError(err, false)
		}
		return meta, rpc.Errorf(rpc.Internal, "%s ended without data", method)
	}
	if ev.B64 || json.Unmarshal(ev.Data, &meta) != nil {
		return meta, rpc.Errorf(rpc.Internal, "%s: first event must be metadata", method)
	}
	return meta, nil
}

// inlineHeaders picks the content type and disposition of a download: only
// the safe types are shown inline, everything else is a plain attachment.
func inlineHeaders(meta fileMeta, p string, inline bool) (name, ctype, disposition string) {
	name = meta.Name
	if name == "" {
		name = path.Base(p)
	}
	ctype = meta.Mime
	if ctype == "" {
		ctype = mime.TypeByExtension(filepath.Ext(name))
	}
	disposition = "attachment"
	if inline && safeInline[strings.TrimSpace(strings.Split(ctype, ";")[0])] {
		disposition = "inline"
	} else {
		ctype = "application/octet-stream"
	}
	return name, ctype, disposition
}

// writeDownloadHead sends the status and headers of a file download.
func writeDownloadHead(w http.ResponseWriter, ctype, disposition string, size int64) {
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", disposition)
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Cache-Control", "no-store")
	if size >= 0 {
		h.Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.WriteHeader(http.StatusOK)
}

// decodeChunk decodes a base64 data event; ok is false when it is malformed.
func decodeChunk(ev rpc.Event) (chunk []byte, ok bool) {
	var b64 string
	if json.Unmarshal(ev.Data, &b64) != nil {
		return nil, false
	}
	chunk, err := base64.StdEncoding.DecodeString(b64)
	return chunk, err == nil
}

func isDoneEvent(data json.RawMessage) bool {
	var ctl struct {
		Done bool `json:"done"`
	}
	return json.Unmarshal(data, &ctl) == nil && ctl.Done
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request, sess *Session) {
	t, e := transferParams(r)
	if e != nil {
		writeError(w, e)
		return
	}
	p := t.path
	inline := r.URL.Query().Get("inline") == "1"
	method := t.method("files.readStream", "remote.readStream")
	b, isAdmin, e := s.route(r.Context(), sess, method, t.admin)
	if e != nil {
		writeError(w, e)
		return
	}
	defer sess.hold(b, isAdmin)()
	st, err := b.Stream(r.Context(), method, t.params())
	if err != nil {
		writeError(w, rpc.ToError(err, false))
		return
	}
	defer st.Close()

	meta, e := readDownloadMeta(st, method)
	if e != nil {
		writeError(w, e)
		return
	}
	name, ctype, disposition := inlineHeaders(meta, p, inline)
	writeDownloadHead(w, ctype, disposition+"; filename*=UTF-8''"+url.PathEscape(name), meta.Size)

	var written int64
	done := false
	for ev := range st.Events() {
		if !ev.B64 {
			done = done || isDoneEvent(ev.Data)
			continue
		}
		chunk, ok := decodeChunk(ev)
		if !ok {
			break
		}
		n, err := w.Write(chunk)
		written += int64(n)
		if err != nil {
			return
		}
	}
	if !done || st.Err() != nil || (meta.Size >= 0 && written != meta.Size) {
		// Truncated transfer: abort the connection so the browser does not
		// keep a partial file as complete.
		s.log.Printf("download %s for %q aborted after %d bytes: %v", p, sess.Account.Name, written, st.Err())
		panic(http.ErrAbortHandler)
	}
}

// collectLast drains a stream's events in the background and delivers the
// last non-data one, which carries the service's answer.
func collectLast(st *rpc.ClientStream) <-chan json.RawMessage {
	result := make(chan json.RawMessage, 1)
	go func() {
		var last json.RawMessage
		for ev := range st.Events() {
			if !ev.B64 {
				last = ev.Data
			}
		}
		result <- last
	}()
	return result
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request, sess *Session) {
	t, e := transferParams(r)
	if e != nil {
		writeError(w, e)
		return
	}
	overwrite := r.URL.Query().Get("overwrite") == "1"
	method := t.method("files.writeStream", "remote.writeStream")
	b, isAdmin, e := s.route(r.Context(), sess, method, t.admin)
	if e != nil {
		writeError(w, e)
		return
	}
	defer sess.hold(b, isAdmin)()
	params := t.params()
	params["size"], params["overwrite"] = r.ContentLength, overwrite
	st, err := b.Stream(r.Context(), method, params)
	if err != nil {
		writeError(w, rpc.ToError(err, false))
		return
	}
	defer st.Close()

	// The final event is {"done":true,…}.
	result := collectLast(st)

	sendErr := s.pumpUpload(r.Context(), st, r.Body, func() {
		if isAdmin {
			sess.touchAdmin()
		}
	})
	if sendErr != nil && !errors.Is(sendErr, errStreamEnded) {
		st.Close()
		<-result
		if r.Context().Err() != nil {
			return
		}
		writeError(w, rpc.ToError(sendErr, false))
		return
	}
	last := <-result
	if err := st.Err(); err != nil {
		writeError(w, rpc.ToError(err, false))
		return
	}
	if last == nil || !isDoneEvent(last) {
		writeError(w, rpc.Errorf(rpc.Internal, "upload did not complete"))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Result json.RawMessage `json:"result"`
	}{last})
}

var errStreamEnded = errors.New("stream ended")

// pumpUpload sends the body as {"data":"<b64>"} inputs followed by
// {"eof":true}. When the browser goes away (ctx, the request's context,
// ends) it returns ctx's error, never errStreamEnded, so the caller closes
// the stream: the bridge then cancels its request to the service instead of
// leaving it waiting for the rest of the body (Docker holds the container's
// lock while it waits).
func (s *Server) pumpUpload(ctx context.Context, st *rpc.ClientStream, body io.Reader, onChunk func()) error {
	buf := make([]byte, uploadChunk)
	for {
		n, rerr := io.ReadFull(body, buf)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if n > 0 {
			msg, _ := json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(buf[:n])})
			if err := s.sendOrCtx(ctx, st, msg); err != nil {
				return err
			}
			onChunk()
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			break
		}
		if rerr != nil {
			return rpc.Errorf(rpc.Invalid, "read upload: %v", rerr)
		}
	}
	return s.sendOrCtx(ctx, st, json.RawMessage(`{"eof":true}`))
}

// sendOrCtx sends one input; a failure caused by ctx ending is ctx's error.
func (s *Server) sendOrCtx(ctx context.Context, st *rpc.ClientStream, msg json.RawMessage) error {
	err := st.Send(ctx, msg)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.streamSendErr(st, err)
}

// streamSendErr distinguishes "the bridge ended the stream" (its error is
// reported by st.Err) from transport failures.
func (s *Server) streamSendErr(st *rpc.ClientStream, err error) error {
	var re *rpc.Error
	if errors.As(err, &re) || errors.Is(err, context.Canceled) {
		return errStreamEnded
	}
	return err
}
