package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/maintenance"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

type resetPlan struct {
	plan    *maintenance.Plan
	owner   string
	expires time.Time
}
type resetCoordinator struct {
	mu     sync.Mutex
	plans  map[string]resetPlan
	active string
}

// RPC keeps its own exception for reset status; logout remains available to
// release open streams while reset preparation refuses new mutating requests.
func (s *Server) resetHTTPGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		if mutation && r.URL.Path != "/api/rpc" && r.URL.Path != "/api/auth/logout" && s.resetInProgress() {
			writeError(w, rpc.Errorf(rpc.Unavailable, "Ervisio is preparing a factory reset. Wait until it finishes."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) resetState() *resetCoordinator {
	s.resetMu.Lock()
	defer s.resetMu.Unlock()
	if s.reset == nil {
		s.reset = &resetCoordinator{plans: map[string]resetPlan{}}
	}
	return s.reset
}
func (s *Server) resetInProgress() bool {
	state := s.resetState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.active == "" {
		return false
	}
	job, err := maintenance.ReadStatus(state.active)
	if err == nil && job.State != "running" {
		state.active = ""
		return false
	}
	return true
}
func (s *Server) resetAdmin(sess *Session) *rpc.Error {
	if sess == nil || sess.Account == nil {
		return errUnauthenticated
	}
	if !sess.Account.IsRoot() && s.rootBridge(sess) == nil {
		return rpc.Errorf(rpc.NeedsAdmin, "Administrator rights are required for factory reset.")
	}
	if runtime.GOOS != "linux" || !bridge.Privileged() || s.opts.Dev {
		return rpc.Errorf(rpc.Unavailable, "Full factory reset requires an installed privileged Linux daemon. No data was changed.")
	}
	if reason := s.revalidate(sess, true); reason != "" {
		s.endSession(sess, reason)
		return errUnauthenticated
	}
	return nil
}

func (s *Server) resetOptions(r *http.Request, sess *Session, preserveAccess bool) (maintenance.Options, error) {
	if filepath.Clean(s.opts.ConfigPath) != brand.ConfigPath {
		return maintenance.Options{}, errors.New("Full factory reset is unavailable with a custom configuration file location.")
	}
	if s.opts.StateDir != "" && filepath.Clean(s.opts.StateDir) != brand.StateDir {
		return maintenance.Options{}, errors.New("Full factory reset is unavailable with a custom state directory.")
	}
	if s.opts.NotifyFile != "" && filepath.Clean(s.opts.NotifyFile) != filepath.Join(brand.ConfigDir, "notify.json") {
		return maintenance.Options{}, errors.New("Move the custom notification store into the managed configuration directory before factory reset.")
	}
	if s.opts.EnvsDir != "" && filepath.Clean(s.opts.EnvsDir) != filepath.Join(brand.StateDir, "envs") {
		return maintenance.Options{}, errors.New("Full factory reset is unavailable with a custom environments directory.")
	}
	if s.opts.TunnelDir != "" && filepath.Clean(s.opts.TunnelDir) != filepath.Join(brand.RunDir, "tunnels") {
		return maintenance.Options{}, errors.New("Full factory reset is unavailable with a custom tunnel directory.")
	}
	if s.opts.Listen != "" {
		return maintenance.Options{}, errors.New("Move the service's --listen override into the managed configuration file before factory reset.")
	}
	scheme := "http"
	if s.secureCookies(r) {
		scheme = "https"
	}
	current := scheme + "://" + r.Host
	parsed, err := url.Parse(current)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return maintenance.Options{}, errors.New("The console origin could not be determined.")
	}
	return maintenance.Options{ConfigPath: brand.ConfigPath, StateDir: brand.StateDir, CacheDir: brand.CacheDir, BackupDir: maintenance.DefaultBackupDir, CurrentURL: current, Operator: sess.Account.Name, PreserveAccess: preserveAccess}, nil
}

func (s *Server) handleResetRPC(w http.ResponseWriter, r *http.Request, sess *Session, req rpcRequest) bool {
	if !strings.HasPrefix(req.Method, "maintenance.reset.") {
		return false
	}
	if e := s.resetAdmin(sess); e != nil {
		writeError(w, e)
		return true
	}
	value, err := s.resetCall(r, sess, req)
	if err != nil {
		writeError(w, rpc.ToError(err, false))
		return true
	}
	writeJSON(w, http.StatusOK, struct {
		Result any `json:"result"`
	}{value})
	return true
}
func (s *Server) resetCall(r *http.Request, sess *Session, req rpcRequest) (any, error) {
	state := s.resetState()
	switch req.Method {
	case "maintenance.reset.preview":
		if s.resetInProgress() {
			return nil, rpc.Errorf(rpc.Conflict, "A factory reset is already running.")
		}
		var p struct {
			PreserveAccess bool `json:"preserveAccess"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "Invalid reset preview parameters.")
		}
		options, err := s.resetOptions(r, sess, p.PreserveAccess)
		if err != nil {
			return nil, rpc.Errorf(rpc.Unavailable, "%v", err)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		plan, err := maintenance.Preview(ctx, options)
		if err != nil {
			return nil, rpc.Errorf(rpc.Unavailable, "%v", err)
		}
		token := sys.RandHex(32)
		expires := time.Now().Add(5 * time.Minute)
		state.mu.Lock()
		defer state.mu.Unlock()
		for key, item := range state.plans {
			if time.Now().After(item.expires) {
				delete(state.plans, key)
			}
		}
		if len(state.plans) >= 16 {
			return nil, rpc.Errorf(rpc.Unavailable, "Too many pending reset previews. Wait for them to expire.")
		}
		state.plans[token] = resetPlan{plan: plan, owner: sess.key, expires: expires}
		return map[string]any{"token": token, "expiresAt": expires.UnixMilli(), "paths": plan.Paths, "preserved": plan.Preserved, "files": plan.Files, "bytes": plan.Bytes, "backupRoot": maintenance.DefaultBackupDir, "nextURL": plan.NextURL}, nil
	case "maintenance.reset.apply":
		var p struct {
			Token   string `json:"token"`
			Confirm string `json:"confirm"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "Invalid reset confirmation.")
		}
		if p.Confirm != "RESET ERVISIO" {
			return nil, rpc.Errorf(rpc.Invalid, "Type RESET ERVISIO to confirm the complete configuration and data reset.")
		}
		state.mu.Lock()
		item, ok := state.plans[p.Token]
		if !ok || item.owner != sess.key || time.Now().After(item.expires) {
			state.mu.Unlock()
			return nil, rpc.Errorf(rpc.Conflict, "The reset preview expired or belongs to another session. Preview again.")
		}
		if state.active != "" {
			state.mu.Unlock()
			return nil, rpc.Errorf(rpc.Conflict, "A factory reset is already running.")
		}
		delete(state.plans, p.Token)
		state.active = item.plan.ID
		state.mu.Unlock()
		statusToken := sys.RandHex(32)
		job, err := maintenance.PrepareHelper(item.plan, statusToken)
		if err == nil {
			err = maintenance.LaunchHelper(r.Context(), job.ID)
		}
		if err != nil {
			maintenance.FailPrepared(item.plan.ID, err)
			state.mu.Lock()
			state.active = ""
			state.mu.Unlock()
			return nil, rpc.Errorf(rpc.Unavailable, "Factory reset could not start: %v", err)
		}
		// Never audit the private preview/status capabilities or serialized configuration.
		record := s.auditBegin(sess, s.realClientIP(r), req.Method, json.RawMessage(`{"scope":"all-ervisio-configuration-and-private-data"}`), true)
		record.callDone(json.RawMessage(`{"state":"running"}`), nil)
		return map[string]any{"id": job.ID, "state": "running", "phase": "backup", "statusToken": statusToken, "expectedBackupPath": filepath.Join(maintenance.DefaultBackupDir, "factory-reset-"+job.ID+".tar.gz"), "nextURL": job.NextURL}, nil
	case "maintenance.reset.status":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "Invalid reset status parameters.")
		}
		job, err := maintenance.ReadStatus(p.ID)
		if err != nil {
			return nil, rpc.Errorf(rpc.NotFound, "Factory-reset status was not found.")
		}
		job.TokenHash = ""
		return job, nil
	default:
		return nil, rpc.Errorf(rpc.NotFound, "Unknown maintenance operation.")
	}
}

// A random read-only status capability survives the intentional daemon restart.
func (s *Server) handleResetPublicStatus(w http.ResponseWriter, r *http.Request) {
	id, token := r.URL.Query().Get("id"), r.URL.Query().Get("token")
	if len(token) != 64 || len(id) != 32 {
		http.NotFound(w, r)
		return
	}
	state, err := maintenance.ReadStatus(id)
	if err != nil || !maintenance.VerifyStatusToken(state, token) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	writeJSON(w, http.StatusOK, maintenance.PublicStatus(state))
}
