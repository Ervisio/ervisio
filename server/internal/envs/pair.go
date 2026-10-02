package envs

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Pairing protocol (see docs/api/environments.md, "Another Ervisio server").
//
//	B (the server whose Docker is used) creates a one-time token: an admin of B
//	picks the Linux user the pairing runs as. A's admin pastes B's address and
//	the token; A POSTs /api/pair/redeem and gets a long-lived credential.
//	A then opens GET /api/pair/bridge (Upgrade: ervisio-bridge/1) with the
//	credential: B starts that user's bridge and relays the bridge protocol
//	for the allowed plugins.* methods only.
const (
	TokenTTL     = 15 * time.Minute
	maxTokens    = 16
	maxPairings  = 32
	UpgradeProto = "ervisio-bridge/1"
	PathInfo     = "/api/pair/info"
	PathRedeem   = "/api/pair/redeem"
	PathBridge   = "/api/pair/bridge"
	PathRevoke   = "/api/pair/revoke"
	PathStatus   = "/api/pair/status"
	HeaderViaSrv = "X-Ervisio-Via-Server"
	HeaderViaUsr = "X-Ervisio-Via-User"
)

type pairToken struct {
	user, by string
	expires  time.Time
}

func randString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// CreatePairToken makes a single-use token valid for TokenTTL. user is the
// local Linux user the pairing will run as.
func (m *Manager) CreatePairToken(by, user string) (string, time.Time, error) {
	m.pairMu.Lock()
	defer m.pairMu.Unlock()
	now := time.Now()
	for k, t := range m.tokens {
		if now.After(t.expires) {
			delete(m.tokens, k)
		}
	}
	if len(m.tokens) >= maxTokens {
		return "", time.Time{}, errf("Too many pairing tokens are open. Wait for them to expire (%d minutes) or use one.", int(TokenTTL.Minutes()))
	}
	if len(m.st.pairings()) >= maxPairings {
		return "", time.Time{}, errf("This server already has %d pairings, the most it accepts. Revoke one first.", maxPairings)
	}
	tok := "ept_" + randString(24)
	exp := now.Add(TokenTTL)
	m.tokens[hashCred(tok)] = &pairToken{user: user, by: by, expires: exp}
	return tok, exp, nil
}

// OpenTokens is how many unexpired tokens exist (shown in Settings).
func (m *Manager) OpenTokens() int {
	m.pairMu.Lock()
	defer m.pairMu.Unlock()
	n := 0
	for _, t := range m.tokens {
		if time.Now().Before(t.expires) {
			n++
		}
	}
	return n
}

func (m *Manager) tooManyFails(addr string) bool {
	m.pairMu.Lock()
	defer m.pairMu.Unlock()
	cut := time.Now().Add(-15 * time.Minute)
	var keep []time.Time
	for _, t := range m.fails[addr] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	m.fails[addr] = keep
	return len(keep) >= 10
}

func (m *Manager) noteFail(addr string) {
	m.pairMu.Lock()
	m.fails[addr] = append(m.fails[addr], time.Now())
	m.pairMu.Unlock()
}

// RedeemResult is what the server gives the one who redeems a token.
type RedeemResult struct {
	Credential string `json:"credential"`
	PairID     string `json:"pairId"`
	Server     string `json:"server"`
	User       string `json:"user"`
}

// Redeem trades a token for a credential (server side). name is what the
// other server calls itself; addr the caller's address (rate limit key).
func (m *Manager) Redeem(token, name, addr string) (*RedeemResult, error) {
	if m.tooManyFails(addr) {
		return nil, errf("Too many wrong tokens from this address. Try again in a few minutes.")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > MaxNameLen || nameBad.MatchString(name) {
		return nil, errf("The other server did not give a usable name.")
	}
	h := hashCred(token)
	m.pairMu.Lock()
	var tok *pairToken
	for k, t := range m.tokens {
		if subtle.ConstantTimeCompare([]byte(k), []byte(h)) == 1 {
			tok = t
			delete(m.tokens, k) // single use, valid or not
		}
	}
	m.pairMu.Unlock()
	if tok == nil || time.Now().After(tok.expires) {
		m.noteFail(addr)
		return nil, errf("This pairing token is not valid. It may have expired (they last %d minutes) or been used already. Create a new one.", int(TokenTTL.Minutes()))
	}
	cred := "epc_" + randString(32)
	idb := make([]byte, 4)
	_, _ = rand.Read(idb)
	p := Pairing{ID: "pair-" + hex.EncodeToString(idb), Name: name, CredHash: hashCred(cred), User: tok.user,
		CreatedBy: tok.by, Created: time.Now().UTC().Truncate(time.Second)}
	if err := m.st.addPairing(p); err != nil {
		return nil, err
	}
	return &RedeemResult{Credential: cred, PairID: p.ID, Server: m.opts.ServerName, User: tok.user}, nil
}

// AuthPair finds the pairing of a credential.
func (m *Manager) AuthPair(cred string) *Pairing {
	if !strings.HasPrefix(cred, "epc_") || len(cred) > 128 {
		return nil
	}
	h := hashCred(cred)
	for _, p := range m.st.pairings() {
		if subtle.ConstantTimeCompare([]byte(p.CredHash), []byte(h)) == 1 {
			p := p
			return &p
		}
	}
	return nil
}

// Pairings lists the pairings other servers hold on this one.
func (m *Manager) Pairings() []Pairing { return m.st.pairings() }

// RevokePairing removes a pairing; its credential stops working at once.
func (m *Manager) RevokePairing(id string) (bool, error) { return m.st.removePairing(id) }

// NotePairUse records that a pairing was used.
func (m *Manager) NotePairUse(id, via, addr string) { m.st.touchPairing(id, via, addr) }

// ---- client side (A) ----

func hostPort(base string) (scheme, host, hp string, err error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", "", "", err
	}
	hp = u.Host
	host = u.Hostname()
	if u.Port() == "" {
		if u.Scheme == "https" {
			hp = net.JoinHostPort(host, "443")
		} else {
			hp = net.JoinHostPort(host, "80")
		}
	}
	return u.Scheme, host, hp, nil
}

// serverTransport dials another Ervisio server: plain, or TLS pinned to pin
// (when pin is empty and seen is non-nil the certificate is recorded).
func serverTransport(base, pin string, seen *string) (*http.Transport, error) {
	scheme, host, hp, err := hostPort(base)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	if scheme == "https" {
		cfg := pinnedConfig(host, pin, seen)
		cfg.NextProtos = []string{"http/1.1"}
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialTLS(ctx, hp, cfg.Clone())
		}
		// requests use http:// URLs so the transport does not wrap TLS twice
	} else {
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) { return dialTCP(ctx, hp) }
	}
	return tr, nil
}

func plainURL(base, path string) string {
	_, _, hp, _ := hostPort(base)
	return "http://" + hp + path
}

func probeServer(ctx context.Context, base string) (*ProbeResult, error) {
	var seen string
	tr, err := serverTransport(base, "", &seen)
	if err != nil {
		return nil, errf("%v", err)
	}
	cl := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, plainURL(base, PathInfo), nil)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, errf("Could not connect to %s: %v", base, cleanNetErr(unwrapURLErr(err)))
	}
	defer resp.Body.Close()
	var info struct {
		Service string `json:"service"`
		Name    string `json:"name"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != 200 || json.Unmarshal(b, &info) != nil || info.Service != "ervisio" {
		return nil, errf("%s does not look like an Ervisio server (or it is too old to be paired).", base)
	}
	return &ProbeResult{Kind: KindErvisio, Fingerprint: seen, Plain: strings.HasPrefix(base, "http://")}, nil
}

func (m *Manager) redeemRemote(ctx context.Context, e *Env, token, by string) (*RedeemResult, error) {
	tr, err := serverTransport(e.Address, e.Fingerprint, nil)
	if err != nil {
		return nil, errf("%v", err)
	}
	body, _ := json.Marshal(map[string]string{"token": strings.TrimSpace(token), "name": m.opts.ServerName})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, plainURL(e.Address, PathRedeem), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: tr, Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, wrapPairErr(e.Address, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != 200 {
		return nil, errf("%s", remoteMessage(b, "The other server refused the pairing ("+resp.Status+")."))
	}
	var res RedeemResult
	if json.Unmarshal(b, &res) != nil || res.Credential == "" {
		return nil, errf("The other server sent an answer that could not be understood.")
	}
	return &res, nil
}

func (m *Manager) revokeRemote(ctx context.Context, e *Env) {
	cred := e.Secret(SecretCredential)
	if cred == "" {
		return
	}
	tr, err := serverTransport(e.Address, e.Fingerprint, nil)
	if err != nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodPost, plainURL(e.Address, PathRevoke), strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+cred)
	req.Header.Set("Content-Type", "application/json")
	if resp, err := (&http.Client{Transport: tr}).Do(req); err == nil {
		resp.Body.Close()
	}
}

func wrapPairErr(addr string, err error) error {
	err = unwrapURLErr(err)
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return errf("Could not connect to %s: %v", addr, cleanNetErr(err))
}

func unwrapURLErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func remoteMessage(b []byte, def string) string {
	var v struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &v) == nil && v.Error.Message != "" {
		return v.Error.Message
	}
	return def
}

func asErr(err error, target **Error) bool { return errors.As(err, target) }

// bufConn gives back the bytes the HTTP reader already buffered.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// DialPair opens the bridge connection to the other server for a call made
// by user (a local account name) and returns it after the 101 answer: the
// bridge protocol follows on the connection, starting with the hello line.
func (m *Manager) DialPair(ctx context.Context, e *Env, user string) (net.Conn, error) {
	scheme, host, hp, err := hostPort(e.Address)
	if err != nil {
		return nil, errf("%v", err)
	}
	var c net.Conn
	if scheme == "https" {
		cfg := pinnedConfig(host, e.Fingerprint, nil)
		cfg.NextProtos = []string{"http/1.1"}
		c, err = dialTLS(ctx, hp, cfg)
	} else {
		c, err = dialTCP(ctx, hp)
	}
	if err != nil {
		return nil, wrapPairErr(e.Address, err)
	}
	fail := func(err error) (net.Conn, error) { c.Close(); return nil, err }
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: %s\r\nAuthorization: Bearer %s\r\n%s: %s\r\n%s: %s\r\n\r\n",
		PathBridge, hp, UpgradeProto, e.Secret(SecretCredential), HeaderViaSrv, headerSafe(m.opts.ServerName), HeaderViaUsr, headerSafe(user))
	if _, err := io.WriteString(c, req); err != nil {
		return fail(wrapPairErr(e.Address, err))
	}
	br := bufio.NewReaderSize(c, 64<<10)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		return fail(errf("The other server did not answer the pairing request: %v", err))
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		def := "The other server refused the connection (" + resp.Status + ")."
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			def = "The other server no longer accepts this pairing. It may have been revoked there; remove this environment and pair again."
		}
		return fail(errf("%s", remoteMessage(b, def)))
	}
	_ = c.SetDeadline(time.Time{})
	return &bufConn{Conn: c, r: br}, nil
}

func headerSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pairCheck connects, reads the hello line and asks the other server's
// Docker for its version (best effort: needs the Docker plugin there).
func (m *Manager) pairCheck(ctx context.Context, e *Env) (version, engine, api string, err error) {
	c, err := m.DialPair(ctx, e, "health check")
	if err != nil {
		return "", "", "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(c)
	line, err := br.ReadBytes('\n')
	var hello rpc.Message
	if err != nil || json.Unmarshal(line, &hello) != nil || hello.Hello == nil {
		return "", "", "", errf("The other server did not start a bridge for this pairing.")
	}
	version = hello.Hello.Version
	req := rpc.Message{ID: 1, Method: "plugins.http", Params: json.RawMessage(`{"plugin":"docker","name":"docker","method":"GET","path":"/version"}`)}
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return version, "", "", nil
	}
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return version, "", "", nil
		}
		var m rpc.Message
		if json.Unmarshal(line, &m) != nil || m.ID != 1 {
			continue
		}
		if m.Error != nil {
			return version, "", "", nil // plugin missing there: reachable all the same
		}
		var r struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
		}
		var v struct{ Version, ApiVersion string }
		if json.Unmarshal(m.Result, &r) == nil && r.Status == 200 && json.Unmarshal([]byte(r.Body), &v) == nil {
			return version, v.Version, v.ApiVersion, nil
		}
		return version, "", "", nil
	}
}

// pairPing asks the other server whether the credential still works
// (no bridge is started there).
func (m *Manager) pairPing(ctx context.Context, e *Env) (string, error) {
	tr, err := serverTransport(e.Address, e.Fingerprint, nil)
	if err != nil {
		return "", errf("%v", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, plainURL(e.Address, PathStatus), nil)
	req.Header.Set("Authorization", "Bearer "+e.Secret(SecretCredential))
	resp, err := (&http.Client{Transport: tr, Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", wrapPairErr(e.Address, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return "", errf("The other server no longer accepts this pairing. It may have been revoked there; remove this environment and pair again.")
	}
	if resp.StatusCode != 200 {
		return "", errf("%s", remoteMessage(b, "The other server answered "+resp.Status+"."))
	}
	var v struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(b, &v)
	return v.Version, nil
}

// AuthBlocked reports whether an address failed too often (wrong tokens or
// credentials) in the last 15 minutes.
func (m *Manager) AuthBlocked(addr string) bool { return m.tooManyFails(addr) }

// NoteAuthFail counts a failed attempt from addr.
func (m *Manager) NoteAuthFail(addr string) { m.noteFail(addr) }
