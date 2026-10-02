package server

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/ervisio/ervisio/server/internal/audit"
	"github.com/ervisio/ervisio/server/internal/config"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// The activity log (internal/audit). The daemon records what goes through
// it: every mutating plugin call (commands, pty starts, HTTP calls other than
// GET and HEAD, uploads, file writes, folders, removals), downloads as read
// entries, and the important core actions (sign-in, administrator unlock,
// plugin install and removal, settings). Request headers and bodies are
// never stored, and secrets in arguments or query strings are replaced (see
// audit.HTTPTarget and audit.CommandTarget).

// auditRec is one entry being recorded for a call or stream.
type auditRec struct {
	s    *Server
	e    audit.Entry
	kind string
	// params of the call (for sizes).
	size  int64
	done  bool
	wrote bool
}

// auditMethods maps a bridge method to the kind of record.
func auditKind(method string) string {
	switch method {
	case "plugins.exec", "plugins.execStream":
		return "command"
	case "plugins.pty":
		return "pty"
	case "plugins.http", "plugins.httpStream":
		return "http"
	case "plugins.writeFile":
		return "file.write"
	case "plugins.mkdir":
		return "file.mkdir"
	case "plugins.remove":
		return "file.remove"
	case "plugins.install":
		return "plugin.install"
	case "plugins.uninstall":
		return "plugin.uninstall"
	case "plugins.setEnabled":
		return "plugin.enable"
	case "config.set":
		return "settings"
	}
	return ""
}

// auditNameRe is what a plugin id, a capability or command name and an
// environment id look like.
var auditNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// auditName is a name taken from a request for the activity log: the
// value when it looks like a plugin id or a capability name, else "?" (the
// bridge refuses such a call anyway; the log must not hold the caller's
// text, security review L2).
func auditName(s string) string {
	if s == "" || auditNameRe.MatchString(s) {
		return s
	}
	return "?"
}

// auditBegin returns the record for a call, or nil when the call is not
// logged (reads, other methods, the log is off).
//
// A browser's call never comes "via" a paired server: whatever the params
// say, its origin stays empty (only the pairing relay, auditBeginFor after
// addVia, sets one).
func (s *Server) auditBegin(sess *Session, ip, method string, params json.RawMessage, admin bool) *auditRec {
	r := s.auditBeginFor(sess.Account.Name, ip, method, params, admin)
	if r != nil {
		r.e.Origin = ""
	}
	return r
}

// auditBeginFor is auditBegin for a user name: the pairing relay has no session.
func (s *Server) auditBeginFor(user, ip, method string, params json.RawMessage, admin bool) *auditRec {
	if !s.audit.Enabled() {
		return nil
	}
	kind := auditKind(method)
	if kind == "" {
		return nil
	}
	var p struct {
		Plugin  string   `json:"plugin"`
		Name    string   `json:"name"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Method  string   `json:"method"`
		Path    string   `json:"path"`
		Query   string   `json:"query"`
		Data    string   `json:"data"`
		B64     bool     `json:"b64"`
		ID      string   `json:"id"`
		Enabled bool     `json:"enabled"`
		Source  string   `json:"source"`
		Key     string   `json:"key"`
		Value   any      `json:"value"`
		// Env is the environment a call is for, Via says that a paired
		// Ervisio server proxied it ("via <server> by <user>").
		Env string `json:"env"`
		Via string `json:"via"`
	}
	_ = json.Unmarshal(params, &p)
	r := &auditRec{s: s, kind: kind}
	r.e = audit.Entry{Time: time.Now(), User: user, IP: ip, Source: audit.SourcePlugin, Plugin: auditName(p.Plugin), Action: kind, Admin: admin, Env: auditName(p.Env), Origin: p.Via}
	switch kind {
	case "command", "pty":
		r.e.Target = audit.CommandTarget(p.Command, p.Args)
	case "http":
		if p.Method == http.MethodGet || p.Method == http.MethodHead {
			return nil
		}
		r.e.Via = auditName(p.Name)
		r.e.Target = audit.HTTPTarget(p.Method, p.Path, p.Query)
		if len(p.Data) > 0 {
			r.size = int64(len(p.Data))
		}
	case "file.write":
		r.e.Target = p.Path
		n := int64(len(p.Data))
		if p.B64 {
			n = int64(base64.StdEncoding.DecodedLen(len(p.Data)))
		}
		r.e.Bytes = &n
	case "file.mkdir", "file.remove":
		r.e.Target = p.Path
	case "plugin.install":
		r.e.Source, r.e.Plugin = audit.SourceCore, ""
		r.e.Target = audit.RedactURL(p.Source)
	case "plugin.uninstall":
		r.e.Source, r.e.Plugin = audit.SourceCore, p.ID
	case "plugin.enable":
		r.e.Source, r.e.Plugin = audit.SourceCore, p.ID
		if !p.Enabled {
			r.e.Action = "plugin.disable"
		}
	case "settings":
		r.e.Source = audit.SourceCore
		r.e.Target = p.Key
		if k, ok := config.Lookup(p.Key); ok && !k.Secret {
			if b, err := json.Marshal(p.Value); err == nil {
				r.e.Target = p.Key + " = " + audit.RedactText(string(b))
			}
		}
	}
	return r
}

// write stores the entry once.
func (r *auditRec) write() {
	if r == nil || r.wrote {
		return
	}
	r.wrote = true
	if err := r.s.audit.Add(r.e); err != nil {
		r.s.log.Printf("activity log: %v", err)
	}
}

func (r *auditRec) setCode(n int) {
	r.e.Code = &n
	r.done = true
	switch r.kind {
	case "command", "pty":
		if n != 0 {
			r.e.Result = audit.Failed
			return
		}
	case "http", "upload", "download":
		if n >= 400 {
			r.e.Result = audit.Failed
			return
		}
	}
	r.e.Result = audit.OK
}

func (r *auditRec) setErr(err error) {
	e := rpc.ToError(err, false)
	switch e.Code {
	case rpc.Forbidden, rpc.NeedsAdmin, rpc.Unauthenticated:
		r.e.Result = audit.Denied
	default:
		r.e.Result = audit.Error
	}
	r.e.Detail = string(e.Code) + ": " + e.Message
	r.done = true
}

// callDone records the end of a request/response call.
func (r *auditRec) callDone(res json.RawMessage, err error) {
	if r == nil {
		return
	}
	switch {
	case err != nil:
		r.setErr(err)
	case r.kind == "command":
		var v struct {
			ExitCode int `json:"exitCode"`
		}
		_ = json.Unmarshal(res, &v)
		r.setCode(v.ExitCode)
	case r.kind == "http":
		var v struct {
			Status int `json:"status"`
		}
		_ = json.Unmarshal(res, &v)
		r.setCode(v.Status)
	default:
		r.e.Result = audit.OK
	}
	r.write()
}

// observe looks at a stream event: the exit code of a command, the status of
// an HTTP response. A pty is written at its first output (it started).
func (r *auditRec) observe(ev rpc.Event) {
	if r == nil || r.wrote {
		return
	}
	if r.kind == "pty" {
		r.e.Result = audit.OK
		r.e.Detail = "terminal started"
		r.write()
		return
	}
	if ev.B64 || r.done {
		return
	}
	switch r.kind {
	case "command":
		if !bytes.Contains(ev.Data, []byte(`"exit"`)) {
			return
		}
		var v struct {
			Exit *int `json:"exit"`
		}
		if json.Unmarshal(ev.Data, &v) == nil && v.Exit != nil {
			r.setCode(*v.Exit)
		}
	case "http", "download":
		var v struct {
			Status int `json:"status"`
		}
		if json.Unmarshal(ev.Data, &v) == nil && v.Status != 0 {
			r.setCode(v.Status)
		}
	}
}

// streamDone records the end of a stream.
func (r *auditRec) streamDone(err error) {
	if r == nil || r.wrote {
		return
	}
	switch {
	case err != nil:
		r.setErr(err)
	case !r.done:
		r.e.Result = audit.OK
	}
	r.write()
}

// auditCore records a core action that is not a bridge call.
func (s *Server) auditCore(user, ip, action, target, result, detail string) {
	if !s.audit.Enabled() {
		return
	}
	if err := s.audit.Add(audit.Entry{User: user, IP: ip, Source: audit.SourceCore, Action: action, Target: target, Result: result, Detail: detail}); err != nil {
		s.log.Printf("activity log: %v", err)
	}
}

// ---- reading ----

// auditParams are the params of audit.list / plugins.audit.list.
type auditParams struct {
	Plugin string `json:"plugin"`
	User   string `json:"user"`
	Source string `json:"source"`
	Action string `json:"action"`
	Text   string `json:"text"`
	// Since and Until are RFC 3339 times or Unix milliseconds.
	Since  any    `json:"since"`
	Until  any    `json:"until"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

func parseWhen(v any) (time.Time, bool) {
	switch x := v.(type) {
	case nil:
		return time.Time{}, true
	case string:
		if x == "" {
			return time.Time{}, true
		}
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t, true
		}
		if t, err := time.Parse("2006-01-02", x); err == nil {
			return t, true
		}
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return time.UnixMilli(n), true
		}
	case float64:
		return time.UnixMilli(int64(x)), true
	}
	return time.Time{}, false
}

// auditQuery turns the params into a query limited to what sess may see:
// administrators (root, or unlocked) see every user, others only their own
// entries. scope forces the plugin ("" = any).
func (s *Server) auditQuery(sess *Session, p auditParams, scope string) (audit.Query, *rpc.Error) {
	q := audit.Query{Plugin: p.Plugin, User: p.User, Source: p.Source, Action: p.Action, Text: p.Text, Limit: p.Limit, Cursor: p.Cursor}
	if scope != "" {
		q.Plugin = scope
		q.Source = audit.SourcePlugin
	}
	var ok1, ok2 bool
	if q.Since, ok1 = parseWhen(p.Since); !ok1 {
		return q, rpc.Errorf(rpc.Invalid, "since is not a valid time.")
	}
	if q.Until, ok2 = parseWhen(p.Until); !ok2 {
		return q, rpc.Errorf(rpc.Invalid, "until is not a valid time.")
	}
	if !s.info(sess).IsAdmin {
		q.User = sess.Account.Name
	}
	return q, nil
}

// handleAuditList serves audit.list (every entry the user may see) and
// plugins.audit.list (only the entries of params.plugin, which must be given).
func (s *Server) handleAuditList(w http.ResponseWriter, sess *Session, method string, params json.RawMessage) {
	var p auditParams
	if len(bytes.TrimSpace(params)) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			writeError(w, rpc.Errorf(rpc.Invalid, "invalid params for %s: %v", method, err))
			return
		}
	}
	scope := ""
	if method == "plugins.audit.list" {
		if p.Plugin == "" {
			writeError(w, rpc.Errorf(rpc.Invalid, "Give the plugin whose entries to list."))
			return
		}
		scope = p.Plugin
	}
	q, e := s.auditQuery(sess, p, scope)
	if e != nil {
		writeError(w, e)
		return
	}
	entries, next, err := s.audit.List(q)
	if err != nil {
		if err == audit.ErrCursor {
			writeError(w, rpc.Errorf(rpc.Invalid, "The cursor is not valid."))
			return
		}
		s.log.Printf("activity log: %v", err)
		writeError(w, rpc.Errorf(rpc.Internal, "Could not read the activity log."))
		return
	}
	if entries == nil {
		entries = []audit.Entry{}
	}
	writeJSON(w, http.StatusOK, struct {
		Result any `json:"result"`
	}{map[string]any{"entries": entries, "next": next, "enabled": s.audit.Enabled()}})
}

// handleAuditExport is GET /api/audit/export?format=csv|json&...: every
// matching entry the user may see, oldest first, as a download.
func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request, sess *Session) {
	qs := r.URL.Query()
	p := auditParams{Plugin: qs.Get("plugin"), User: qs.Get("user"), Source: qs.Get("source"), Action: qs.Get("action"), Text: qs.Get("text")}
	if v := qs.Get("since"); v != "" {
		p.Since = v
	}
	if v := qs.Get("until"); v != "" {
		p.Until = v
	}
	q, e := s.auditQuery(sess, p, "")
	if e != nil {
		writeError(w, e)
		return
	}
	format := qs.Get("format")
	if format != "csv" && format != "json" {
		writeError(w, rpc.Errorf(rpc.Invalid, "format must be csv or json"))
		return
	}
	name := "activity-log-" + time.Now().UTC().Format("20060102-150405") + "." + format
	h := w.Header()
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	if format == "csv" {
		h.Set("Content-Type", "text/csv; charset=utf-8")
		cw := csv.NewWriter(w)
		_ = cw.Write(audit.CSVHeader)
		_ = s.audit.Each(q, func(e audit.Entry) error { return cw.Write(audit.CSVRecord(e)) })
		cw.Flush()
		return
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	_, _ = w.Write([]byte("[\n"))
	first := true
	_ = s.audit.Each(q, func(e audit.Entry) error {
		if !first {
			_, _ = w.Write([]byte(",\n"))
		}
		first = false
		return enc.Encode(e)
	})
	_, _ = w.Write([]byte("]\n"))
}
