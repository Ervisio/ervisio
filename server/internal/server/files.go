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

func transferParams(r *http.Request) (fileTarget, *rpc.Error) {
	q := r.URL.Query()
	t := fileTarget{path: q.Get("path"), remote: q.Get("remote")}
	if t.remote != "" {
		if !validRemoteID(t.remote) {
			return t, rpc.Errorf(rpc.Invalid, "remote must be a connection id")
		}
		if t.path == "" || len(t.path) > 4096 || !path.IsAbs(t.path) || strings.ContainsRune(t.path, 0) {
			return t, rpc.Errorf(rpc.Invalid, "path must be an absolute path")
		}
	} else if t.path == "" || len(t.path) > 4096 || !filepath.IsAbs(t.path) || strings.ContainsRune(t.path, 0) {
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

	// First event: metadata.
	ev, ok := <-st.Events()
	if !ok {
		if err := st.Err(); err != nil {
			writeError(w, rpc.ToError(err, false))
		} else {
			writeError(w, rpc.Errorf(rpc.Internal, "%s ended without data", method))
		}
		return
	}
	var meta fileMeta
	if ev.B64 || json.Unmarshal(ev.Data, &meta) != nil {
		writeError(w, rpc.Errorf(rpc.Internal, "%s: first event must be metadata", method))
		return
	}
	name := meta.Name
	if name == "" {
		name = path.Base(p)
	}
	ctype := meta.Mime
	if ctype == "" {
		ctype = mime.TypeByExtension(filepath.Ext(name))
	}
	disposition := "attachment"
	if inline && safeInline[strings.TrimSpace(strings.Split(ctype, ";")[0])] {
		disposition = "inline"
	} else {
		ctype = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(name))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Cache-Control", "no-store")
	if meta.Size >= 0 {
		h.Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	}
	w.WriteHeader(http.StatusOK)

	var written int64
	done := false
	for ev := range st.Events() {
		if !ev.B64 {
			var ctl struct {
				Done bool `json:"done"`
			}
			if json.Unmarshal(ev.Data, &ctl) == nil && ctl.Done {
				done = true
			}
			continue
		}
		var b64 string
		if json.Unmarshal(ev.Data, &b64) != nil {
			break
		}
		chunk, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
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

	// Collect events concurrently; the final one is {"done":true,…}.
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
	var ctl struct {
		Done bool `json:"done"`
	}
	if last == nil || json.Unmarshal(last, &ctl) != nil || !ctl.Done {
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
			if err := st.Send(ctx, msg); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return s.streamSendErr(st, err)
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
	if err := st.Send(ctx, json.RawMessage(`{"eof":true}`)); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return s.streamSendErr(st, err)
	}
	return nil
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
