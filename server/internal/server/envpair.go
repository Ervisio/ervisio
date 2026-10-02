package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/envs"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// pairAllowed lists what another Ervisio server may ask this one's bridge
// for: the plugin capabilities and commands, nothing else.
var pairAllowed = map[string]bool{
	"plugins.http": true, "plugins.httpStream": true,
	"plugins.exec": true, "plugins.execStream": true, "plugins.pty": true,
	"plugins.download": true, "plugins.upload": true,
}

func (s *Server) registerPair(mux *http.ServeMux) {
	mux.HandleFunc("GET "+envs.PathInfo, s.handlePairInfo)
	mux.HandleFunc("POST "+envs.PathRedeem, s.handlePairRedeem)
	mux.HandleFunc("GET "+envs.PathStatus, s.handlePairStatus)
	mux.HandleFunc("POST "+envs.PathRevoke, s.handlePairRevoke)
	mux.HandleFunc("GET "+envs.PathBridge, s.handlePairBridge)
}

func (s *Server) pairReady(w http.ResponseWriter) bool {
	if s.env == nil || s.env.m == nil {
		writeError(w, errNotFound)
		return false
	}
	return true
}

func (s *Server) handlePairInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"service": "ervisio", "name": sys.Hostname(), "version": brand.Version, "pairing": s.env != nil && s.env.m != nil})
}

func (s *Server) handlePairRedeem(w http.ResponseWriter, r *http.Request) {
	if !s.pairReady(w) {
		return
	}
	var req struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if e := decodeJSON(w, r, 4<<10, &req); e != nil {
		writeError(w, e)
		return
	}
	res, err := s.env.m.Redeem(req.Token, req.Name, clientIP(r))
	if err != nil {
		s.log.Printf("pairing: refused a token from %s: %v", clientIP(r), err)
		writeErrorStatus(w, http.StatusForbidden, envErr(err))
		return
	}
	s.log.Printf("pairing: %q (%s) paired as %s", req.Name, clientIP(r), res.User)
	writeJSON(w, http.StatusOK, res)
}

// pairFromRequest authenticates the Bearer credential.
func (s *Server) pairFromRequest(w http.ResponseWriter, r *http.Request) *envs.Pairing {
	if !s.pairReady(w) {
		return nil
	}
	ip := clientIP(r)
	if s.env.m.AuthBlocked(ip) {
		writeErrorStatus(w, http.StatusTooManyRequests, rpc.Errorf(rpc.Forbidden, "Too many wrong credentials from this address. Try again in a few minutes."))
		return nil
	}
	h := r.Header.Get("Authorization")
	p := (*envs.Pairing)(nil)
	if strings.HasPrefix(h, "Bearer ") {
		p = s.env.m.AuthPair(strings.TrimSpace(h[7:]))
	}
	if p == nil {
		s.env.m.NoteAuthFail(ip)
		writeErrorStatus(w, http.StatusUnauthorized, rpc.Errorf(rpc.Unauthenticated, "This pairing is not valid (it may have been revoked)."))
		return nil
	}
	return p
}

func (s *Server) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	p := s.pairFromRequest(w, r)
	if p == nil {
		return
	}
	s.env.m.NotePairUse(p.ID, "status", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"name": sys.Hostname(), "version": brand.Version, "user": p.User})
}

func (s *Server) handlePairRevoke(w http.ResponseWriter, r *http.Request) {
	p := s.pairFromRequest(w, r)
	if p == nil {
		return
	}
	_, _ = s.env.m.RevokePairing(p.ID)
	s.closePairConns(p.ID)
	s.log.Printf("pairing: %q revoked its own pairing %s", p.Name, p.ID)
	writeJSON(w, http.StatusOK, map[string]string{"id": p.ID})
}

// handlePairBridge upgrades the connection and relays the bridge protocol
// of the pairing's local user, filtered to pairAllowed. Every call is
// logged as "via <server> by <user>"; the same text goes to the bridge as
// the "via" param (for the activity log).
func (s *Server) handlePairBridge(w http.ResponseWriter, r *http.Request) {
	p := s.pairFromRequest(w, r)
	if p == nil {
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), envs.UpgradeProto) {
		writeError(w, rpc.Errorf(rpc.Invalid, "expected Upgrade: %s", envs.UpgradeProto))
		return
	}
	acc, e := s.pairUser(p.User)
	if e != nil {
		writeError(w, e)
		return
	}
	viaSrv, viaUser := r.Header.Get(envs.HeaderViaSrv), r.Header.Get(envs.HeaderViaUsr)
	via := p.Name
	if viaSrv != "" {
		via = viaSrv
	}
	if viaUser != "" {
		via += " by " + viaUser
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, rpc.Errorf(rpc.Unavailable, "this connection cannot be upgraded"))
		return
	}
	raw, err := bridge.StartRaw(s.spec(acc, clientIP(r)))
	if err != nil {
		s.log.Printf("pairing: start bridge for %s: %v", acc.Name, err)
		writeError(w, rpc.Errorf(rpc.Unavailable, "the bridge could not be started"))
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		raw.Stop()
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Time{})
	if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+envs.UpgradeProto+"\r\n\r\n"); err != nil {
		raw.Stop()
		return
	}
	s.env.m.NotePairUse(p.ID, via, clientIP(r))
	s.log.Printf("pairing: %s connected via pairing %s, running as %s", via, p.ID, acc.Name)

	var wmu sync.Mutex
	writeLine := func(b []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		_, err := conn.Write(append(bytes.TrimRight(b, "\n"), '\n'))
		return err
	}
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done); conn.Close(); raw.Stop() }) }
	defer stop()
	defer s.trackPairConn(p.ID, stop)()

	// bridge -> other server
	go func() {
		defer stop()
		br := bufio.NewReaderSize(raw.Stdout, 64<<10)
		for {
			line, err := readLongLine(br)
			if err != nil {
				return
			}
			if writeLine(line) != nil {
				return
			}
		}
	}()
	// other server -> bridge, filtered
	in := brw.Reader
	for {
		line, err := readLongLine(in)
		if err != nil {
			return
		}
		var m rpc.Message
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m.Method != "" {
			if !pairAllowed[m.Method] {
				s.log.Printf("pairing: %s asked for %s, refused", via, m.Method)
				b, _ := json.Marshal(rpc.Message{ID: m.ID, Error: rpc.Errorf(rpc.Forbidden, "%s may not be called through a pairing.", m.Method)})
				if writeLine(b) != nil {
					return
				}
				continue
			}
			m.Params = addVia(m.Params, via)
			s.log.Printf("pairing: via %s -> %s %s", via, acc.Name, describeCall(m))
			line, _ = json.Marshal(m)
		}
		if _, err := raw.Stdin.Write(append(bytes.TrimRight(line, "\n"), '\n')); err != nil {
			return
		}
	}
}

// readLongLine reads one protocol line of at most rpc.MaxLine bytes.
func readLongLine(br *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(out)+len(chunk) > rpc.MaxLine {
			return nil, io.ErrShortBuffer
		}
		out = append(out, chunk...)
		if !isPrefix {
			return out, nil
		}
	}
}

func addVia(params json.RawMessage, via string) json.RawMessage {
	var obj map[string]json.RawMessage
	if json.Unmarshal(params, &obj) != nil || obj == nil {
		return params
	}
	delete(obj, "envSocket")
	delete(obj, "env")
	obj["via"], _ = json.Marshal(via)
	b, err := json.Marshal(obj)
	if err != nil {
		return params
	}
	return b
}

// describeCall is "<method> <plugin>[/<capability or command>]" for the log.
func describeCall(m rpc.Message) string {
	var p struct{ Plugin, Name, Command string }
	_ = json.Unmarshal(m.Params, &p)
	s := m.Method + " " + p.Plugin
	if p.Name != "" {
		s += "/" + p.Name
	}
	if p.Command != "" {
		s += "/" + p.Command
	}
	return s
}
