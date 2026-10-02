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

func transferParams(r *http.Request) (string, bool, *rpc.Error) {
	q := r.URL.Query()
	p := q.Get("path")
	if p == "" || len(p) > 4096 || !filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
		return "", false, rpc.Errorf(rpc.Invalid, "path must be an absolute path")
	}
	admin := false
	switch q.Get("admin") {
	case "", "0", "false":
	case "1", "true":
		admin = true
	default:
		return "", false, rpc.Errorf(rpc.Invalid, "admin must be 0 or 1")
	}
	return p, admin, nil
}

// fileMeta is the first event of files.readStream.
type fileMeta struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Mime string `json:"mime"`
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request, sess *Session) {
	p, admin, e := transferParams(r)
	if e != nil {
		writeError(w, e)
		return
	}
	inline := r.URL.Query().Get("inline") == "1"
	const method = "files.readStream"
	b, isAdmin, e := s.route(r.Context(), sess, method, admin)
	if e != nil {
		writeError(w, e)
		return
	}
	defer sess.hold(b, isAdmin)()
	st, err := b.Stream(r.Context(), method, map[string]string{"path": p})
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
			writeError(w, rpc.Errorf(rpc.Internal, "files.readStream ended without data"))
		}
		return
	}
	var meta fileMeta
	if ev.B64 || json.Unmarshal(ev.Data, &meta) != nil {
		writeError(w, rpc.Errorf(rpc.Internal, "files.readStream: first event must be metadata"))
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
	p, admin, e := transferParams(r)
	if e != nil {
		writeError(w, e)
		return
	}
	overwrite := r.URL.Query().Get("overwrite") == "1"
	const method = "files.writeStream"
	b, isAdmin, e := s.route(r.Context(), sess, method, admin)
	if e != nil {
		writeError(w, e)
		return
	}
	defer sess.hold(b, isAdmin)()
	params := map[string]any{"path": p, "size": r.ContentLength, "overwrite": overwrite}
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
// {"eof":true}.
func (s *Server) pumpUpload(ctx context.Context, st *rpc.ClientStream, body io.Reader, onChunk func()) error {
	buf := make([]byte, uploadChunk)
	for {
		n, rerr := io.ReadFull(body, buf)
		if n > 0 {
			msg, _ := json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(buf[:n])})
			if err := st.Send(ctx, msg); err != nil {
				return s.streamSendErr(st, err)
			}
			onChunk()
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return rpc.Errorf(rpc.Invalid, "read upload: %v", rerr)
		}
	}
	if err := st.Send(ctx, json.RawMessage(`{"eof":true}`)); err != nil {
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
