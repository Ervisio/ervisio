package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/bridge"
	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
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
	np, err := bridge.StartUser(ctx, s.spec(sess.Account))
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
// it is gone or idle for longer than session.admin_unlock.
func (s *Server) rootBridge(sess *Session) *bridge.Proc {
	idle := s.Config().Session.AdminUnlock.Duration
	sess.mu.Lock()
	p := sess.root
	if p != nil && (!p.Alive() || (!p.Busy() && time.Since(sess.rootUsed) > idle)) {
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

// MaxRPCBody bounds /api/rpc request bodies.
const MaxRPCBody = 1 << 20

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request, sess *Session) {
	var req rpcRequest
	if e := decodeJSON(w, r, MaxRPCBody, &req); e != nil {
		writeError(w, e)
		return
	}
	p, isAdmin, e := s.route(r.Context(), sess, req.Method, req.Admin)
	if e != nil {
		writeError(w, e)
		return
	}
	defer sess.hold(p, isAdmin)()
	res, err := p.Call(r.Context(), req.Method, req.Params)
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
