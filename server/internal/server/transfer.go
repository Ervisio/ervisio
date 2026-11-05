package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ervisio/ervisio/server/internal/audit"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Large transfers for plugins (plugins.download / plugins.upload in the SDK).
//
// The page asks POST /api/plugins/transfer for a transfer. The daemon routes
// it to the user's (or the root) bridge, which checks the request against the
// plugin's manifest exactly as plugins.http / plugins.execStream do and starts
// the transfer's stream (plugins.httpDownload, plugins.execDownload,
// plugins.httpUpload). The answer is a one-time URL, valid for transferTTL,
// bound to the session that asked. Fetching it (GET for a download; POST of
// the file for an upload) pipes the bytes between the browser and the bridge
// through the stream's flow control: nothing is buffered beyond its window,
// and the 16 MB protocol line and the 8 MB plugins.http limits do not apply.
const (
	transferTTL = 60 * time.Second
	// maxTransfersPerSession bounds the transfers a session has waiting or
	// running; maxTransferIssues bounds how many it may start per minute.
	maxTransfersPerSession = 8
	maxTransferIssues      = 30
	// maxTransferRequest bounds the JSON that asks for a transfer.
	maxTransferRequest = 64 << 10
	// errBodyMax is how much of an error response is returned to the plugin.
	errBodyMax = 2048
	// transferPrefix is the path of the one-time URLs.
	transferPrefix = "/api/plugins/transfer/"
)

type transfer struct {
	sess    *Session
	kind    string // "download" or "upload"
	st      *rpc.ClientStream
	cancel  context.CancelFunc
	release func()
	isAdmin bool
	rec     *auditRec
	// name is the download's file name.
	name string
	// size is the exact upload size, or the download's Content-Length (-1 if unknown).
	size int64
	// stream: the upload's response is streamed back (see transferRequest.Stream).
	stream bool

	status int
	timer  *time.Timer

	// store is the store that holds res.
	store *transferStore
	// rid keys res in the store (the hash of the token).
	rid string
	// res is what the page can ask for when the transfer ends.
	res *xferResult
}

// resultKeep is how long the outcome of a transfer stays readable.
const resultKeep = 5 * time.Minute

// xferResult is the outcome of one transfer, readable by the session that
// started it with GET /api/plugins/transfer/{token}/status. A download is
// fetched by the browser itself, so the page cannot see its end: it asks.
type xferResult struct {
	sess *Session
	done chan struct{}
	once sync.Once

	// Set before done is closed.
	ok    bool
	bytes int64
	err   string
}

// finishResult records the outcome once (the first call wins); later calls,
// such as the generic close after a normal end, change nothing.
func (ts *transferStore) finishResult(t *transfer, ok bool, bytes int64, msg string, id string) {
	r := t.res
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.ok, r.bytes, r.err = ok, bytes, msg
		close(r.done)
		time.AfterFunc(resultKeep, func() {
			ts.mu.Lock()
			delete(ts.results, id)
			ts.mu.Unlock()
		})
	})
}

// result returns the outcome record of token, for the session that owns it.
func (ts *transferStore) result(token string, sess *Session) *xferResult {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	r := ts.results[tokenID(token)]
	if r == nil || r.sess != sess {
		return nil
	}
	return r
}

// close ends the transfer's stream and gives its slot back.
func (t *transfer) close(ts *transferStore) {
	// A transfer that ends without having said so was cut short.
	ts.finishResult(t, false, 0, "The transfer was cancelled before it finished.", t.rid)
	t.st.Close()
	t.cancel()
	t.release()
	ts.done(t.sess)
}

// transferStore holds the transfers waiting for their one-time URL.
type transferStore struct {
	mu     sync.Mutex
	byID   map[string]*transfer
	live   map[*Session]int
	issued map[*Session][]time.Time
	// results are the outcomes of started transfers, by token hash.
	results map[string]*xferResult
	now     func() time.Time
	ttl     time.Duration
}

func newTransferStore() *transferStore {
	return &transferStore{byID: map[string]*transfer{}, live: map[*Session]int{}, issued: map[*Session][]time.Time{}, results: map[string]*xferResult{}, now: time.Now, ttl: transferTTL}
}

// reserve takes a slot for a new transfer of sess, or says why not.
func (ts *transferStore) reserve(sess *Session) *rpc.Error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	now := ts.now()
	recent := ts.issued[sess][:0]
	for _, t := range ts.issued[sess] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= maxTransferIssues {
		ts.issued[sess] = recent
		return rpc.Errorf(rpc.Unavailable, "Too many transfers started in the last minute. Wait a moment and try again.")
	}
	if ts.live[sess] >= maxTransfersPerSession {
		ts.issued[sess] = recent
		return rpc.Errorf(rpc.Unavailable, "Too many transfers are running. Wait for one to finish.")
	}
	ts.issued[sess] = append(recent, now)
	ts.live[sess]++
	return nil
}

// done gives back a slot.
func (ts *transferStore) done(sess *Session) {
	ts.mu.Lock()
	if ts.live[sess]--; ts.live[sess] <= 0 {
		delete(ts.live, sess)
	}
	ts.mu.Unlock()
}

// gc forgets the rate records of sessions with nothing running.
func (ts *transferStore) gc() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	now := ts.now()
	for sess, l := range ts.issued {
		if ts.live[sess] > 0 {
			continue
		}
		if len(l) == 0 || now.Sub(l[len(l)-1]) >= time.Minute {
			delete(ts.issued, sess)
		}
	}
}

// forget drops the rate record of an ended session.
func (ts *transferStore) forget(sess *Session) {
	ts.mu.Lock()
	delete(ts.issued, sess)
	ts.mu.Unlock()
}

func tokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// put registers t under a new token and starts its expiry.
func (ts *transferStore) put(t *transfer) (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	id := tokenID(token)
	exp := ts.now().Add(ts.ttl)
	ts.mu.Lock()
	ts.byID[id] = t
	t.rid = id
	t.store = ts
	if t.kind == "download" {
		t.res = &xferResult{sess: t.sess, done: make(chan struct{})}
		ts.results[id] = t.res
	}
	// Under the lock: the callback reads t.timer through take.
	t.timer = time.AfterFunc(ts.ttl, func() {
		if ts.take(token, nil) == t {
			ts.finishResult(t, false, 0, "The download did not start in time.", id)
			t.close(ts)
		}
	})
	ts.mu.Unlock()
	return token, exp, nil
}

// take removes and returns the transfer of token: single use. A transfer
// of another session is left in place (nil is returned) when sess is given.
func (ts *transferStore) take(token string, sess *Session) *transfer {
	id := tokenID(token)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	t := ts.byID[id]
	if t == nil || (sess != nil && t.sess != sess) {
		return nil
	}
	delete(ts.byID, id)
	if t.timer != nil {
		t.timer.Stop()
	}
	return t
}

// closeSession ends the waiting transfers of a session that ended.
func (ts *transferStore) closeSession(sess *Session) {
	ts.mu.Lock()
	var mine []*transfer
	for id, t := range ts.byID {
		if t.sess == sess {
			mine = append(mine, t)
			delete(ts.byID, id)
		}
	}
	ts.mu.Unlock()
	for _, t := range mine {
		if t.timer != nil {
			t.timer.Stop()
		}
		ts.finishResult(t, false, 0, "The session ended.", t.rid)
		t.close(ts)
	}
	ts.forget(sess)
}

// transferRequest is the body of POST /api/plugins/transfer. The page has
// already checked it against the manifest; the bridge checks it again.
type transferRequest struct {
	Kind   string `json:"kind"` // "download" or "upload"
	Plugin string `json:"plugin"`
	// Name is the capabilities.http entry; Command (with Args) a declared
	// command, for downloads of its standard output.
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   string            `json:"query"`
	Headers map[string]string `json:"headers"`
	// Filename is the suggested name of a download.
	Filename string `json:"filename"`
	// Size is the size in bytes of an upload.
	Size int64 `json:"size"`
	// Env is the environment the request is for ("" = this machine). The
	// daemon resolves it (access list, tunnel or paired server) in
	// routeWithEnv, as for plugins.http; the browser never sets a socket.
	Env string `json:"env,omitempty"`
	// Stream (uploads) answers with the service's response as it arrives,
	// as application/x-ndjson lines: {"start":{status,headers}},
	// {"data":"<base64>"}..., {"done":true,"status"} or {"error":{code,message}}.
	Stream bool `json:"stream"`
	Admin  bool `json:"admin"`
}

// bridgeMethod and params of the request, and its audit target.
func (q *transferRequest) bridge() (method string, params map[string]any, ok bool) {
	switch {
	case q.Kind == "download" && q.Command != "":
		p := map[string]any{"plugin": q.Plugin, "command": q.Command, "args": q.Args}
		if q.Env != "" {
			p["env"] = q.Env
		}
		return "plugins.execDownload", p, true
	case q.Kind == "download" && q.Name != "":
		return "plugins.httpDownload", q.httpParams(), true
	case q.Kind == "upload" && q.Name != "":
		p := q.httpParams()
		p["size"] = q.Size
		if q.Stream {
			p["stream"] = true
		}
		return "plugins.httpUpload", p, true
	}
	return "", nil, false
}

func (q *transferRequest) httpParams() map[string]any {
	p := map[string]any{"plugin": q.Plugin, "name": q.Name, "method": q.Method, "path": q.Path}
	if q.Env != "" {
		p["env"] = q.Env
	}
	if q.Query != "" {
		p["query"] = q.Query
	}
	if len(q.Headers) > 0 {
		p["headers"] = q.Headers
	}
	return p
}

// sanitizeFilename makes a name safe for Content-Disposition and for the
// file the browser saves: no path, control characters, quotes or characters
// that Windows or shells treat specially, at most 200 bytes.
func sanitizeFilename(name string) string {
	name = strings.ToValidUTF8(name, "_")
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f || unicode.IsControl(r) || r == ' ' || r == ' ' || r == '‮':
			b.WriteByte('_')
		case strings.ContainsRune(`<>:"/\|?*;%`+"`$", r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimLeft(out, ".")
	out = strings.TrimRight(out, ". ")
	for len(out) > 200 {
		_, n := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-n]
	}
	if out == "" {
		return "download"
	}
	return out
}

// contentDisposition is an attachment header for name.
func contentDisposition(name string) string {
	ascii := make([]rune, 0, len(name))
	for _, r := range name {
		if r < 0x20 || r > 0x7e {
			r = '_'
		}
		ascii = append(ascii, r)
	}
	return `attachment; filename="` + string(ascii) + `"; filename*=UTF-8''` + url.PathEscape(name)
}

// handleTransferStart is POST /api/plugins/transfer.
func (s *Server) handleTransferStart(w http.ResponseWriter, r *http.Request, sess *Session) {
	var req transferRequest
	if e := decodeJSON(w, r, maxTransferRequest, &req); e != nil {
		writeError(w, e)
		return
	}
	method, params, e := req.checked()
	if e != nil {
		writeError(w, e)
		return
	}
	ip := s.realClientIP(r)
	rec := s.transferRecord(sess, ip, &req)
	fail := func(e *rpc.Error) {
		if rec != nil && e.Code != rpc.NeedsAdmin {
			rec.setErr(e)
			rec.write()
		}
		writeError(w, e)
	}
	if e := s.transfers.reserve(sess); e != nil {
		fail(e)
		return
	}
	reserved := true
	defer func() {
		if reserved {
			s.transfers.done(sess)
		}
	}()
	rawParams, _ := json.Marshal(params)
	b, isAdmin, rawParams, relEnv, e := s.routeWithEnv(r.Context(), sess, method, rawParams, req.Admin)
	if e != nil {
		fail(e)
		return
	}
	hold := sess.hold(b, isAdmin)
	release := func() { hold(); relEnv() }
	handed := false
	defer func() {
		if !handed {
			release()
		}
	}()
	sctx, cancel := context.WithCancel(s.baseCtx)
	defer func() {
		if !handed {
			cancel()
		}
	}()
	go s.endWithSession(sctx, sess, cancel)
	st, err := b.Stream(sctx, method, json.RawMessage(rawParams))
	if err != nil {
		fail(rpc.ToError(err, false))
		return
	}
	defer func() {
		if !handed {
			st.Close()
		}
	}()
	t := &transfer{sess: sess, kind: req.Kind, st: st, cancel: cancel, release: release, isAdmin: isAdmin, rec: rec, size: -1}

	start, e, ok := awaitTransferStart(r.Context(), st)
	if !ok {
		if e != nil {
			fail(e)
		}
		return
	}
	resp := map[string]any{}
	if req.Kind == "download" {
		if start.Status == 0 {
			fail(errBadStart())
			return
		}
		t.status = start.Status
		if start.Status < 200 || start.Status > 299 {
			writeServiceAnswer(w, st, rec, start.Status)
			return
		}
		describeDownload(t, start, req.Filename, resp)
	} else {
		if !start.Ready {
			fail(errBadStart())
			return
		}
		t.size = req.Size
		t.stream = req.Stream
	}
	token, exp, err := s.transfers.put(t)
	if err != nil {
		fail(rpc.Errorf(rpc.Internal, "Could not create the transfer."))
		return
	}
	handed = true
	reserved = false // the slot lasts until the transfer ends
	resp["url"] = transferPrefix + token
	resp["expires"] = exp.UnixMilli()
	writeJSON(w, http.StatusOK, struct {
		Result any `json:"result"`
	}{resp})
}

// checked returns the bridge method and params of a valid request.
func (q *transferRequest) checked() (method string, params map[string]any, e *rpc.Error) {
	method, params, ok := q.bridge()
	if !ok {
		return "", nil, rpc.Errorf(rpc.Invalid, "Ask for a download of an HTTP API or command, or an upload to an HTTP API.")
	}
	if q.Kind == "upload" && (q.Size < 0 || q.Method != http.MethodPost && q.Method != http.MethodPut) {
		return "", nil, rpc.Errorf(rpc.Invalid, "An upload needs a size and the method POST or PUT.")
	}
	if q.Kind == "download" && q.Command == "" && q.Method != http.MethodGet {
		return "", nil, rpc.Errorf(rpc.Invalid, "A download uses GET.")
	}
	return method, params, nil
}

// endWithSession ends the session's transfers when it ends (sign-out,
// expiry, revalidation): a running one is cancelled, a waiting one-time URL
// dropped (security review L6).
func (s *Server) endWithSession(sctx context.Context, sess *Session, cancel context.CancelFunc) {
	select {
	case <-sess.Done():
		cancel()
		s.transfers.closeSession(sess)
	case <-sctx.Done():
	}
}

type transferStart struct {
	Ready   bool              `json:"ready"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
}

func errBadStart() *rpc.Error {
	return rpc.Errorf(rpc.Internal, "The transfer did not start properly.")
}

// awaitTransferStart reads the stream's first event, which says the request
// passed the rules: the response started (download) or the bridge is ready
// for the file (upload). ok is false when the transfer cannot go on; e is
// then nil if the client went away.
func awaitTransferStart(ctx context.Context, st *rpc.ClientStream) (start transferStart, e *rpc.Error, ok bool) {
	var first rpc.Event
	select {
	case ev, open := <-st.Events():
		if !open {
			if err := st.Err(); err != nil {
				return start, rpc.ToError(err, false), false
			}
			return start, rpc.Errorf(rpc.Internal, "The transfer ended before it started."), false
		}
		first = ev
	case <-ctx.Done():
		return start, nil, false
	}
	if first.B64 || json.Unmarshal(first.Data, &start) != nil {
		return start, errBadStart(), false
	}
	return start, nil, true
}

// writeServiceAnswer answers a download whose service did not send a file
// with the service's own answer, for the plugin.
func writeServiceAnswer(w http.ResponseWriter, st *rpc.ClientStream, rec *auditRec, status int) {
	msg := readErrorBody(st, errBodyMax)
	e := rpc.Errorf(rpc.Unavailable, "The service answered %d%s", status, msg).WithData(map[string]any{"status": status})
	if rec != nil {
		rec.setCode(status)
		rec.write()
	}
	writeError(w, e)
}

// describeDownload sets the download's size and file name on t and resp.
func describeDownload(t *transfer, start transferStart, filename string, resp map[string]any) {
	if n, err := strconv.ParseInt(start.Headers["Content-Length"], 10, 64); err == nil && n >= 0 {
		t.size = n
		resp["size"] = n
	}
	t.name = sanitizeFilename(filename)
	resp["filename"] = t.name
	resp["status"] = start.Status
}

// readErrorBody reads up to max bytes of a failed response from st, as
// ": <text>" (empty when there is none or it is not text), and closes it.
func readErrorBody(st *rpc.ClientStream, max int) string {
	defer st.Close()
	var buf []byte
	timeout := time.After(5 * time.Second)
	for len(buf) < max {
		select {
		case ev, open := <-st.Events():
			if !open {
				return textOf(buf)
			}
			if !ev.B64 {
				continue
			}
			var b64 string
			if json.Unmarshal(ev.Data, &b64) != nil {
				return textOf(buf)
			}
			b, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return textOf(buf)
			}
			buf = append(buf, b...)
		case <-timeout:
			return textOf(buf)
		}
	}
	return textOf(buf[:max])
}

// textOf formats an error body for a message: the "message" of a JSON
// object, else the text, trimmed.
func textOf(b []byte) string {
	if len(b) == 0 || !utf8.Valid(b) {
		return "."
	}
	var v struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &v) == nil && v.Message != "" {
		return ": " + strings.TrimSpace(v.Message)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "."
	}
	return ": " + s
}

// transferRecord is the audit record of a transfer: downloads are "read"
// entries, uploads change the service.
func (s *Server) transferRecord(sess *Session, ip string, q *transferRequest) *auditRec {
	return s.transferRecordFor(sess.Account.Name, ip, q)
}

func (s *Server) transferRecordFor(user, ip string, q *transferRequest) *auditRec {
	if !s.audit.Enabled() {
		return nil
	}
	rec := &auditRec{s: s, kind: q.Kind}
	rec.e = audit.Entry{Time: time.Now(), User: user, IP: ip, Source: audit.SourcePlugin, Plugin: auditName(q.Plugin), Action: q.Kind, Admin: q.Admin, Env: auditName(q.Env)}
	if q.Command != "" {
		rec.e.Target = audit.CommandTarget(q.Command, q.Args)
	} else {
		rec.e.Via = auditName(q.Name)
		rec.e.Target = audit.HTTPTarget(q.Method, q.Path, q.Query)
	}
	return rec
}

// handleTransfer is GET (download) and POST (upload) of a one-time URL.
func (s *Server) handleTransfer(w http.ResponseWriter, r *http.Request, sess *Session) {
	token := r.PathValue("token")
	t := s.transfers.take(token, sess)
	if t == nil {
		writeError(w, rpc.Errorf(rpc.NotFound, "This transfer link was already used or has expired."))
		return
	}
	defer t.close(s.transfers)
	switch {
	case t.kind == "download" && r.Method == http.MethodGet:
		s.serveDownload(w, r, t)
	case t.kind == "upload" && r.Method == http.MethodPost:
		s.serveUpload(w, r, t)
	default:
		t.rec = nil
		s.transfers.finishResult(t, false, 0, "The link was used with the wrong method.", t.rid)
		writeErrorStatus(w, http.StatusMethodNotAllowed, rpc.Errorf(rpc.Invalid, "This link is for a %s.", t.kind))
	}
}

// maxStatusWait bounds how long GET .../status holds a request open.
const maxStatusWait = 25 * time.Second

// statusWait reads the ?wait= seconds of a status request, capped.
func statusWait(r *http.Request) time.Duration {
	n, err := strconv.Atoi(r.URL.Query().Get("wait"))
	if err != nil || n <= 0 {
		return 0
	}
	return min(time.Duration(n)*time.Second, maxStatusWait)
}

// statusOutcome is the answer for a transfer: {"done":false} until it ended.
func statusOutcome(res *xferResult) map[string]any {
	select {
	case <-res.done:
		out := map[string]any{"done": true, "ok": res.ok, "bytes": res.bytes}
		if res.err != "" {
			out["error"] = res.err
		}
		return out
	default:
		return map[string]any{"done": false}
	}
}

// handleTransferStatus is GET /api/plugins/transfer/{token}/status?wait=<seconds>:
// the outcome of a download, for the session that started it. It waits up to
// `wait` seconds (at most 25) for the transfer to end and answers
// {"done":false} when it has not, so the page can ask again. The record is
// kept for five minutes after the end. Unknown, expired and other sessions'
// tokens are all "not found".
func (s *Server) handleTransferStatus(w http.ResponseWriter, r *http.Request, sess *Session) {
	res := s.transfers.result(r.PathValue("token"), sess)
	if res == nil {
		writeError(w, rpc.Errorf(rpc.NotFound, "There is no such download, or its result is no longer kept."))
		return
	}
	if wait := statusWait(r); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-res.done:
		case <-timer.C:
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Result any `json:"result"`
	}{statusOutcome(res)})
}

func (s *Server) serveDownload(w http.ResponseWriter, r *http.Request, t *transfer) {
	writeDownloadHead(w, "application/octet-stream", contentDisposition(t.name), t.size)
	var written int64
	for ev := range t.st.Events() {
		if !ev.B64 {
			continue
		}
		chunk, ok := decodeChunk(ev)
		if !ok {
			break
		}
		n, err := w.Write(chunk)
		written += int64(n)
		if err != nil {
			t.finish(&written, context.Canceled)
			return
		}
	}
	err := downloadEndErr(r, t, written)
	t.finish(&written, err)
	if err != nil {
		// Truncated: abort the connection so the browser does not keep a
		// partial file as complete.
		s.log.Printf("plugin download of %q for %q aborted after %d bytes: %v", t.name, t.sess.Account.Name, written, err)
		panic(http.ErrAbortHandler)
	}
}

// downloadEndErr is why a download stream ended short, or nil when complete.
func downloadEndErr(r *http.Request, t *transfer, written int64) error {
	err := t.st.Err()
	if err == nil && t.size >= 0 && written != t.size {
		err = errors.New("the response ended early")
	}
	if err == nil && r.Context().Err() != nil {
		err = r.Context().Err()
	}
	return err
}

// finish writes the audit entry of a download and records its outcome.
func (t *transfer) finish(bytes *int64, err error) {
	if t.store != nil {
		msg := ""
		if err != nil {
			msg = "The download did not finish: " + err.Error()
			if errors.Is(err, context.Canceled) {
				msg = "The download was cancelled before the end."
			}
		}
		t.store.finishResult(t, err == nil, *bytes, msg, t.rid)
	}
	if t.rec == nil {
		return
	}
	t.rec.e.Bytes = bytes
	if err != nil {
		t.rec.setErr(err)
		t.rec.e.Result = audit.Failed
		t.rec.e.Code = &t.status
	} else {
		t.rec.setCode(t.status)
	}
	t.rec.write()
}

// failUpload records a failed upload in the audit entry and answers the
// browser, unless it is already gone.
func failUpload(w http.ResponseWriter, r *http.Request, t *transfer, sent int64, e *rpc.Error) {
	cancelled := r.Context().Err() != nil
	if t.rec != nil {
		t.rec.e.Bytes = &sent
		if cancelled {
			// The browser went away or the plugin cancelled it.
			t.rec.e.Result, t.rec.e.Detail = audit.Failed, "cancelled before the end"
		} else {
			t.rec.setErr(e)
		}
		t.rec.write()
	}
	if !cancelled {
		writeError(w, e)
	}
}

func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request, t *transfer) {
	var sent int64
	fail := func(e *rpc.Error) { failUpload(w, r, t, sent, e) }
	if r.ContentLength >= 0 && r.ContentLength != t.size {
		fail(rpc.Errorf(rpc.Invalid, "The upload is %d bytes but %d were announced.", r.ContentLength, t.size))
		return
	}
	body := http.MaxBytesReader(w, r.Body, t.size)
	cr := &countReader{r: body}
	if t.stream {
		s.serveUploadStream(w, r, t, cr, fail)
		return
	}
	// The bridge sends one last event with the service's answer.
	result := collectLast(t.st)
	sendErr := s.pumpUpload(r.Context(), t.st, cr, func() {
		if t.isAdmin {
			t.sess.touchAdmin()
		}
	})
	sent = cr.n
	if sendErr != nil && !errors.Is(sendErr, errStreamEnded) {
		t.st.Close()
		<-result
		fail(rpc.ToError(sendErr, false))
		return
	}
	last := <-result
	if err := t.st.Err(); err != nil {
		fail(rpc.ToError(err, false))
		return
	}
	var res struct {
		Done   bool `json:"done"`
		Status int  `json:"status"`
	}
	if last == nil || json.Unmarshal(last, &res) != nil || !res.Done {
		fail(rpc.Errorf(rpc.Internal, "The upload did not complete."))
		return
	}
	if t.rec != nil {
		t.rec.e.Bytes = &sent
		t.rec.setCode(res.Status)
		t.rec.write()
	}
	writeJSON(w, http.StatusOK, struct {
		Result json.RawMessage `json:"result"`
	}{last})
}

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// serveUploadStream is serveUpload when the plugin wants the response as it
// arrives (a build answering with progress lines while the context uploads).
// The response starts with the service's own: nothing is written before it, so
// the browser keeps sending the file until the service has answered.
func (s *Server) serveUploadStream(w http.ResponseWriter, r *http.Request, t *transfer, cr *countReader, fail func(*rpc.Error)) {
	rc := http.NewResponseController(w)
	_ = rc.EnableFullDuplex() // the service may answer while the file is still being read
	out := &ndjsonWriter{w: w, rc: rc}
	var (
		status int
		done   bool
	)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		status, done = relayUploadEvents(t.st.Events(), out)
	}()
	sendErr := s.pumpUpload(r.Context(), t.st, cr, func() {
		if t.isAdmin {
			t.sess.touchAdmin()
		}
	})
	if sendErr != nil && !errors.Is(sendErr, errStreamEnded) {
		t.st.Close()
	}
	<-finished
	sent := cr.n
	err := uploadStreamErr(t.st.Err(), sendErr, done)
	if t.rec != nil {
		t.rec.e.Bytes = &sent
	}
	if err != nil {
		e := rpc.ToError(err, false)
		if !out.hasStarted() {
			fail(e)
			return
		}
		if t.rec != nil {
			t.rec.setErr(e)
			t.rec.write()
		}
		b, _ := json.Marshal(map[string]any{"error": map[string]any{"code": e.Code, "message": e.Message}})
		out.write(b)
		return
	}
	if t.rec != nil {
		t.rec.setCode(status)
		t.rec.write()
	}
}

// ndjsonWriter writes the lines of a streamed upload response, starting the
// response with the first one.
type ndjsonWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	rc      *http.ResponseController
	started bool
}

func (n *ndjsonWriter) write(line []byte) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.started {
		n.started = true
		h := n.w.Header()
		h.Set("Content-Type", "application/x-ndjson")
		h.Set("Cache-Control", "no-store")
		n.w.WriteHeader(http.StatusOK)
	}
	_, _ = n.w.Write(append(line, '\n'))
	_ = n.rc.Flush()
}

func (n *ndjsonWriter) hasStarted() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.started
}

// relayUploadEvents writes the bridge's events as lines until the stream
// ends and returns the service's status and whether the done event came.
func relayUploadEvents(events <-chan rpc.Event, out *ndjsonWriter) (status int, done bool) {
	for ev := range events {
		if ev.B64 {
			out.write(append(append([]byte(`{"data":`), ev.Data...), '}'))
			continue
		}
		var m struct {
			Status  int             `json:"status"`
			Headers json.RawMessage `json:"headers"`
			Done    bool            `json:"done"`
		}
		if json.Unmarshal(ev.Data, &m) != nil {
			continue
		}
		if m.Status != 0 {
			status = m.Status
		}
		if m.Done {
			done = true
			out.write(ev.Data)
		} else if m.Status != 0 {
			out.write(append(append([]byte(`{"start":`), ev.Data...), '}'))
		}
	}
	return status, done
}

// uploadStreamErr is why a streamed upload failed, or nil: the stream's own
// error first, then the send's, then a missing done event.
func uploadStreamErr(streamErr, sendErr error, done bool) error {
	if streamErr != nil {
		return streamErr
	}
	if sendErr != nil && !errors.Is(sendErr, errStreamEnded) {
		return sendErr
	}
	if !done {
		return errors.New("The upload did not complete.") //nolint:staticcheck // user-facing message, wording is part of the API
	}
	return nil
}
