package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/envs"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// pairAllowed lists what another Ervisio server may ask this one's bridge
// for: the plugin capabilities and commands, nothing else.
// maxPairConns bounds the bridges one pairing may hold open on this server.
const maxPairConns = 16

var pairAllowed = map[string]bool{
	"plugins.http": true, "plugins.httpStream": true,
	"plugins.exec": true, "plugins.execStream": true, "plugins.pty": true,
	"plugins.httpDownload": true, "plugins.httpUpload": true, "plugins.execDownload": true,
	// The plugin files capability, run by this server's user bridge under
	// its manifest (never the root bridge: an admin folder works only when
	// the mapped user is root or in the folder's adminUnlessGroup).
	"plugins.readFile": true, "plugins.writeFile": true, "plugins.listDir": true,
	"plugins.mkdir": true, "plugins.remove": true,
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
	s.log.Printf("pairing: a server that calls itself %q (%s) paired as %s, pairing %s", req.Name, clientIP(r), res.User, res.PairID)
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
	s.log.Printf("pairing: pairing %s (calls itself %q) revoked itself", p.ID, p.Name)
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
	ip := clientIP(r)
	via := pairVia(r, p)
	if open := s.pairConnCount(p.ID); open >= maxPairConns {
		writeErrorStatus(w, http.StatusTooManyRequests, rpc.Errorf(rpc.Unavailable, "This pairing already has %d connections open.", open))
		return
	}
	conn, in, raw, ok := s.upgradePairBridge(w, acc, ip)
	if !ok {
		return
	}
	defer conn.Close()
	s.env.m.NotePairUse(p.ID, via, ip)
	s.log.Printf("pairing: %s connected via pairing %s, running as %s", via, p.ID, acc.Name)

	relay := &pairRelay{s: s, conn: conn, raw: raw, user: acc.Name, ip: ip, via: via, done: make(chan struct{}), recs: map[uint64]*auditRec{}}
	defer relay.stop()
	defer s.trackPairConn(p.ID, relay.stop)()
	defer relay.closeRecords()
	go relay.bridgeToPeer()
	relay.peerToBridge(in)
}

// pairVia is the origin text of a pairing's calls. The server and user names
// come from the other server: they are what it claims. What is known is the
// pairing (its id, made when a token of this server was redeemed), so the
// text names the pairing too.
func pairVia(r *http.Request, p *envs.Pairing) string {
	viaSrv, viaUser := r.Header.Get(envs.HeaderViaSrv), r.Header.Get(envs.HeaderViaUsr)
	claimed := p.Name
	if viaSrv != "" {
		claimed = viaSrv
	}
	via := clipText(claimed, 64) + " (pairing " + p.ID + ")"
	if viaUser != "" {
		via += " by " + clipText(viaUser, 64)
	}
	return via
}

func (s *Server) pairConnCount(id string) int {
	s.env.mu.Lock()
	defer s.env.mu.Unlock()
	return len(s.env.pairConns[id])
}

// upgradePairBridge starts the user bridge and switches the connection to
// the bridge protocol. On failure it has answered or dropped the request.
func (s *Server) upgradePairBridge(w http.ResponseWriter, acc *account.Account, ip string) (net.Conn, *bufio.Reader, *bridge.Raw, bool) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, rpc.Errorf(rpc.Unavailable, "this connection cannot be upgraded"))
		return nil, nil, nil, false
	}
	raw, err := bridge.StartRaw(s.spec(acc, ip))
	if err != nil {
		s.log.Printf("pairing: start bridge for %s: %v", acc.Name, err)
		writeError(w, rpc.Errorf(rpc.Unavailable, "the bridge could not be started"))
		return nil, nil, nil, false
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		raw.Stop()
		return nil, nil, nil, false
	}
	_ = conn.SetDeadline(time.Time{})
	if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+envs.UpgradeProto+"\r\n\r\n"); err != nil {
		raw.Stop()
		conn.Close()
		return nil, nil, nil, false
	}
	return conn, brw.Reader, raw, true
}

// pairRelay is one live bridge connection of a pairing. The calls this
// server runs for the other one go to the activity log as the local user,
// with origin "via <server> by <user>".
type pairRelay struct {
	s             *Server
	conn          net.Conn
	raw           *bridge.Raw
	user, ip, via string
	wmu           sync.Mutex
	done          chan struct{}
	once          sync.Once
	rmu           sync.Mutex
	recs          map[uint64]*auditRec
}

func (pr *pairRelay) writeLine(b []byte) error {
	pr.wmu.Lock()
	defer pr.wmu.Unlock()
	_, err := pr.conn.Write(append(bytes.TrimRight(b, "\n"), '\n'))
	return err
}

func (pr *pairRelay) stop() {
	pr.once.Do(func() { close(pr.done); pr.conn.Close(); pr.raw.Stop() })
}

func (pr *pairRelay) closeRecords() {
	pr.rmu.Lock()
	defer pr.rmu.Unlock()
	for _, rec := range pr.recs {
		rec.streamDone(errors.New("the connection to the other server closed"))
	}
}

// observe finishes the activity log entry of a call when the bridge answers it.
func (pr *pairRelay) observe(line []byte) {
	var m rpc.Message
	if json.Unmarshal(line, &m) != nil || m.ID == 0 || m.Method != "" {
		return
	}
	pr.rmu.Lock()
	defer pr.rmu.Unlock()
	rec := pr.recs[m.ID]
	if rec == nil {
		return
	}
	switch {
	case m.Error != nil:
		rec.callDone(nil, m.Error)
		delete(pr.recs, m.ID)
	case m.Event == rpc.EventEnd:
		rec.streamDone(nil)
		delete(pr.recs, m.ID)
	case m.Event == rpc.EventData:
		rec.observe(rpc.Event{Data: m.Data, B64: m.B64})
	case m.Event == "" && !m.Stream:
		rec.callDone(m.Result, nil)
		delete(pr.recs, m.ID)
	}
}

func (pr *pairRelay) bridgeToPeer() {
	defer pr.stop()
	br := bufio.NewReaderSize(pr.raw.Stdout, 64<<10)
	for {
		line, err := readLongLine(br)
		if err != nil {
			return
		}
		pr.observe(line)
		if pr.writeLine(line) != nil {
			return
		}
	}
}

func (pr *pairRelay) peerToBridge(in *bufio.Reader) {
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
				if pr.refuse(m) != nil {
					return
				}
				continue
			}
			line = pr.forwardCall(m)
		}
		if _, err := pr.raw.Stdin.Write(append(bytes.TrimRight(line, "\n"), '\n')); err != nil {
			return
		}
	}
}

func (pr *pairRelay) refuse(m rpc.Message) error {
	pr.s.log.Printf("pairing: %s asked for %s, refused", pr.via, m.Method)
	b, _ := json.Marshal(rpc.Message{ID: m.ID, Error: rpc.Errorf(rpc.Forbidden, "%s may not be called through a pairing.", m.Method)})
	return pr.writeLine(b)
}

// forwardCall tags an allowed call with its origin, opens its activity log
// entry and returns the line to send to the bridge.
func (pr *pairRelay) forwardCall(m rpc.Message) []byte {
	m.Params = addVia(m.Params, pr.via)
	if rec := pr.s.pairAudit(pr.user, pr.ip, m, pr.via); rec != nil && m.ID != 0 {
		pr.rmu.Lock()
		pr.recs[m.ID] = rec
		pr.rmu.Unlock()
	}
	pr.s.log.Printf("pairing: via %s -> %s %s", pr.via, pr.user, describeCall(m))
	line, _ := json.Marshal(m)
	return line
}

// clipText keeps a claimed name printable and short for the logs.
func clipText(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > n {
		s = strings.ToValidUTF8(s[:n], "") + "…"
	}
	return s
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
	// Every spelling: encoding/json matches keys without regard to case.
	takeKeyFold(obj, "envSocket")
	takeKeyFold(obj, "env")
	takeKeyFold(obj, "via")
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

// pairAudit starts the activity log entry of a call another server made.
func (s *Server) pairAudit(user, ip string, m rpc.Message, via string) *auditRec {
	var rec *auditRec
	switch m.Method {
	case "plugins.httpDownload", "plugins.httpUpload", "plugins.execDownload":
		q := &transferRequest{Kind: "download"}
		if m.Method == "plugins.httpUpload" {
			q.Kind = "upload"
		}
		if json.Unmarshal(m.Params, q) != nil {
			return nil
		}
		rec = s.transferRecordFor(user, ip, q)
	default:
		rec = s.auditBeginFor(user, ip, m.Method, m.Params, false)
	}
	if rec != nil {
		rec.e.Origin = via
		rec.e.Env = ""
	}
	return rec
}
