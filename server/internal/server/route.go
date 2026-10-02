package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// userBridge returns the session's user bridge, restarting it if it died.
func (s *Server) userBridge(ctx context.Context, sess *Session) (*bridge.Proc, *rpc.Error) {
	sess.mu.Lock()
	p, closed := sess.user, sess.closed
	sess.mu.Unlock()
	if closed {
		return nil, errUnauthenticated
	}
	if p != nil && p.Alive() {
		return p, nil
	}
	// Restart, serialised per session.
	sess.spawnMu.Lock()
	defer sess.spawnMu.Unlock()
	sess.mu.Lock()
	p = sess.user
	sess.mu.Unlock()
	if p != nil && p.Alive() {
		return p, nil
	}
	if p != nil {
		s.log.Printf("user bridge for %q exited (%v), restarting", sess.Account.Name, p.Err())
	}
	// The account may have changed since sign-in: check it again (with
	// PAM) before giving it a new bridge.
	if reason := s.revalidate(sess, true); reason != "" {
		s.endSession(sess, reason)
		return nil, errUnauthenticated
	}
	np, err := bridge.StartUser(ctx, s.spec(sess.Account, sess.RHost))
	if err != nil {
		s.log.Printf("restart bridge for %q: %v", sess.Account.Name, err)
		return nil, rpc.Errorf(rpc.Unavailable, "the session's bridge is not running")
	}
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		np.Stop()
		return nil, errUnauthenticated
	}
	sess.user = np
	sess.mu.Unlock()
	return np, nil
}

// rootBridge returns the unlocked root bridge, or nil (and stops it) when
// it is gone or idle for longer than session.admin_unlock (0 = never).
func (s *Server) rootBridge(sess *Session) *bridge.Proc {
	idle := s.Config().Session.AdminUnlock.Duration
	sess.mu.Lock()
	p := sess.root
	if p != nil && (!p.Alive() || (idle > 0 && !p.Busy() && time.Since(sess.rootUsed) > idle)) {
		sess.root = nil
		sess.mu.Unlock()
		go p.Stop()
		return nil
	}
	if p != nil {
		sess.rootUsed = time.Now()
	}
	sess.mu.Unlock()
	return p
}

// touchAdmin records admin activity (keeps the unlock alive).
func (sess *Session) touchAdmin() {
	sess.mu.Lock()
	if sess.root != nil {
		sess.rootUsed = time.Now()
	}
	sess.mu.Unlock()
}

// hold marks a call or stream in progress on p (when it is the root bridge,
// so the admin idle timeout does not stop it meanwhile). The release
// function records admin activity at the end.
func (sess *Session) hold(p *bridge.Proc, isAdmin bool) func() {
	if !isAdmin {
		return func() {}
	}
	release := p.Hold()
	return func() {
		release()
		sess.touchAdmin()
	}
}

// route picks the bridge for a call: the root bridge when admin is asked
// or the method is admin-level (needs_admin if not unlocked), otherwise
// the user bridge. Root sessions always use the user bridge.
func (s *Server) route(ctx context.Context, sess *Session, method string, admin bool) (*bridge.Proc, bool, *rpc.Error) {
	if !rpc.ValidMethodName(method) {
		return nil, false, rpc.Errorf(rpc.Invalid, "invalid method name")
	}
	ub, e := s.userBridge(ctx, sess)
	if e != nil {
		return nil, false, e
	}
	lvl, ok := ub.Level(method)
	if !ok {
		return nil, false, rpc.Errorf(rpc.NotFound, "unknown method %s", method)
	}
	if sess.Account.IsRoot() {
		return ub, false, nil
	}
	if admin || lvl == rpc.Admin {
		rb := s.rootBridge(sess)
		if rb == nil {
			return nil, false, rpc.Errorf(rpc.NeedsAdmin, "administrator rights are needed").
				WithData(map[string]string{"method": method})
		}
		return rb, true, nil
	}
	return ub, false, nil
}

type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Admin  bool            `json:"admin"`
}

// MaxRPCBody bounds /api/rpc request bodies. It carries a plugins.http body of
// up to 8 MiB (the default maxBody) as base64 (11.2 MiB), and stays under the
// 16 MiB protocol line to the bridge. Larger bodies use plugins.upload.
const MaxRPCBody = 12 << 20

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request, sess *Session) {
	var req rpcRequest
	if e := decodeJSON(w, r, MaxRPCBody, &req); e != nil {
		writeError(w, e)
		return
	}
	// The activity log is read by the daemon itself (it is root's).
	if req.Method == "audit.list" || req.Method == "plugins.audit.list" {
		s.handleAuditList(w, sess, req.Method, req.Params)
		return
	}
	if s.handleLocalRPC(w, r, sess, req) {
		return
	}
	p, isAdmin, e := s.route(r.Context(), sess, req.Method, req.Admin)
	if e != nil {
		writeError(w, e)
		return
	}
	defer sess.hold(p, isAdmin)()
	rec := s.auditBegin(sess, s.realClientIP(r), req.Method, req.Params, isAdmin)
	res, err := p.Call(r.Context(), req.Method, req.Params)
	if err == nil || r.Context().Err() == nil {
		rec.callDone(res, err)
	}
	if err != nil {
		if r.Context().Err() != nil {
			return // client went away
		}
		writeError(w, rpc.ToError(err, false))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Result json.RawMessage `json:"result"`
	}{res})
}
