package envs

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Options configure a Manager.
type Options struct {
	// Dir keeps envs.json and master.key (0700).
	Dir string
	// TunnelDir holds the per-user tunnel sockets (/run/ervisio/tunnels).
	TunnelDir string
	// ServerName is how this server introduces itself when it pairs.
	ServerName string
	Log        *log.Logger
}

// Manager owns the environments of one daemon.
type Manager struct {
	opts   Options
	st     *store
	signer *AgentSigner
	sigMu  sync.Mutex
	poolMu sync.Mutex

	mu      sync.Mutex
	tunnels map[string]*tunnel
	pools   map[string]*sshPool
	status  map[string]*Status

	pairMu sync.Mutex
	tokens map[string]*pairToken
	fails  map[string][]time.Time
}

// NewManager opens the store.
func NewManager(o Options) (*Manager, error) {
	if o.Log == nil {
		o.Log = log.Default()
	}
	st, err := openStore(o.Dir)
	if err != nil {
		return nil, err
	}
	m := &Manager{opts: o, st: st, tunnels: map[string]*tunnel{}, pools: map[string]*sshPool{},
		status: map[string]*Status{}, tokens: map[string]*pairToken{}, fails: map[string][]time.Time{}}
	return m, nil
}

// Run checks every environment's health regularly and closes idle tunnels,
// until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(45 * time.Second)
	defer t.Stop()
	first := time.After(2 * time.Second)
	for {
		select {
		case <-ctx.Done():
			m.closeAll()
			return
		case <-first:
			m.checkAll(ctx)
		case <-t.C:
			m.gcTunnels()
			m.checkAll(ctx)
		}
	}
}

func (m *Manager) closeAll() {
	m.mu.Lock()
	ts := make([]*tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		ts = append(ts, t)
	}
	m.tunnels = map[string]*tunnel{}
	m.mu.Unlock()
	m.poolMu.Lock()
	ps := m.pools
	m.pools = map[string]*sshPool{}
	m.poolMu.Unlock()
	for _, t := range ts {
		t.close()
	}
	for _, p := range ps {
		p.Close()
	}
}

func (m *Manager) checkAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, e := range m.st.list() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			m.test(cctx, e.ID, false)
		}()
	}
	wg.Wait()
}

// Agent returns the signer used for Portainer agents (created on first use).
func (m *Manager) agent() (*AgentSigner, error) {
	m.sigMu.Lock()
	defer m.sigMu.Unlock()
	if m.signer != nil {
		return m.signer, nil
	}
	if p := m.st.agentKey(); p != "" {
		if s, err := ParseAgentSigner(p); err == nil {
			m.signer = s
			return s, nil
		}
	}
	s, err := NewAgentSigner()
	if err != nil {
		return nil, err
	}
	p, err := s.MarshalPEM()
	if err != nil {
		return nil, err
	}
	if err := m.st.setAgentKey(p); err != nil {
		return nil, err
	}
	m.signer = s
	return s, nil
}

// dialFn returns the function that opens a raw connection to the
// environment's Docker API (tcp-tls, ssh). Portainer agents go through
// agentTransport.
func (m *Manager) dialFn(e *Env) (func(context.Context) (net.Conn, error), error) {
	switch e.Kind {
	case KindTCPTLS:
		if e.Insecure {
			addr := e.Address
			return func(ctx context.Context) (net.Conn, error) { return dialTCP(ctx, addr) }, nil
		}
		cfg, err := tlsClientConfig(e)
		if err != nil {
			return nil, err
		}
		addr := e.Address
		return func(ctx context.Context) (net.Conn, error) {
			c, err := dialTLS(ctx, addr, cfg.Clone())
			if err != nil {
				return nil, wrapDialErr(addr, err)
			}
			return c, nil
		}, nil
	case KindSSH:
		return m.pool(e).Dial, nil
	}
	return nil, errf("%s environments have no direct connection.", e.Kind)
}

func wrapDialErr(addr string, err error) error {
	var e *Error
	if asErr(err, &e) {
		return e
	}
	return errf("Could not connect to %s: %v", addr, cleanNetErr(err))
}

func (m *Manager) pool(e *Env) *sshPool {
	m.poolMu.Lock()
	defer m.poolMu.Unlock()
	if p := m.pools[e.ID]; p != nil {
		return p
	}
	p := newSSHPool(e)
	m.pools[e.ID] = p
	return p
}

func (m *Manager) dropPool(id string) {
	m.poolMu.Lock()
	p := m.pools[id]
	delete(m.pools, id)
	m.poolMu.Unlock()
	if p != nil {
		p.Close()
	}
}

// agentTransport is an http.RoundTripper that signs requests and sends them
// to the agent over TLS pinned to the stored fingerprint.
func (m *Manager) agentTransport(e *Env) (http.RoundTripper, error) {
	signer, err := m.agent()
	if err != nil {
		return nil, err
	}
	host, _, err := SplitHostPort(e.Address)
	if err != nil {
		return nil, err
	}
	addr, secret := e.Address, e.Secret(SecretAgent)
	cfg := pinnedConfig(host, e.Fingerprint, nil)
	cfg.NextProtos = []string{"http/1.1"}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			c, err := dialTLS(ctx, addr, cfg.Clone())
			if err != nil {
				return nil, wrapDialErr(addr, err)
			}
			return c, nil
		},
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 0,
		DisableCompression:    true,
	}
	return &signingRT{rt: tr, signer: signer, secret: secret}, nil
}

type signingRT struct {
	rt     http.RoundTripper
	signer *AgentSigner
	sigMu  sync.Mutex
	poolMu sync.Mutex
	secret string
}

func (s *signingRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	if err := s.signer.Sign(r2.Header, s.secret); err != nil {
		return nil, err
	}
	return s.rt.RoundTrip(r2)
}

// httpClient returns a client for short API calls (status checks) to a
// docker-kind environment.
func (m *Manager) httpClient(e *Env, timeout time.Duration) (*http.Client, error) {
	if e.Kind == KindPortainerAgent {
		rt, err := m.agentTransport(e)
		if err != nil {
			return nil, err
		}
		return &http.Client{Transport: rt, Timeout: timeout}, nil
	}
	dial, err := m.dialFn(e)
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		DialContext:       func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
	}}, nil
}

// Test checks the environment now and stores the result.
func (m *Manager) Test(ctx context.Context, id string) *Status { return m.test(ctx, id, true) }

func (m *Manager) test(ctx context.Context, id string, full bool) *Status {
	e := m.st.get(id)
	if e == nil {
		return nil
	}
	st := m.check(ctx, e, full)
	if e.Kind == KindErvisio && !full && st.Reachable {
		m.mu.Lock()
		if old := m.status[id]; old != nil {
			st.EngineVersion, st.APIVersion = old.EngineVersion, old.APIVersion
		}
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.status[id] = st
	m.mu.Unlock()
	return st
}

func (m *Manager) check(ctx context.Context, e *Env, full bool) *Status {
	t0 := time.Now()
	st := &Status{Checked: t0}
	fail := func(err error) *Status {
		st.Reachable, st.Error = false, err.Error()
		st.LatencyMs = time.Since(t0).Milliseconds()
		return st
	}
	if e.Kind == KindErvisio {
		if !full {
			if _, err := m.pairPing(ctx, e); err != nil {
				return fail(err)
			}
			st.Reachable = true
			st.LatencyMs = time.Since(t0).Milliseconds()
			return st
		}
		_, eng, api, err := m.pairCheck(ctx, e)
		if err != nil {
			return fail(err)
		}
		st.Reachable, st.EngineVersion, st.APIVersion = true, eng, api
		st.LatencyMs = time.Since(t0).Milliseconds()
		return st
	}
	cl, err := m.httpClient(e, 15*time.Second)
	if err != nil {
		return fail(err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/version", nil)
	resp, err := cl.Do(req)
	if err != nil {
		return fail(friendlyTLS(unwrapURLErr(err)))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		msg := "The Docker API answered " + resp.Status + "."
		if resp.StatusCode == 403 && e.Kind == KindPortainerAgent {
			msg = "The agent refused our signature. If it has no AGENT_SECRET it may already be tied to another Portainer; set AGENT_SECRET on the agent and enter it here."
		}
		return fail(errf("%s", msg))
	}
	var v struct {
		Version    string `json:"Version"`
		APIVersion string `json:"ApiVersion"`
	}
	if json.Unmarshal(body, &v) != nil || v.Version == "" {
		return fail(errf("That address answered, but it does not look like a Docker engine."))
	}
	st.Reachable, st.EngineVersion, st.APIVersion = true, v.Version, v.APIVersion
	st.LatencyMs = time.Since(t0).Milliseconds()
	return st
}

// ProbeResult is what "Check connection" learns before anything is stored.
type ProbeResult struct {
	// Fingerprint is the certificate ("sha256:…") or host key ("SHA256:…") seen.
	Fingerprint string `json:"fingerprint"`
	// HostKey is the SSH host key in authorized_keys format.
	HostKey string `json:"hostKey,omitempty"`
	Kind    string `json:"kind"`
	// Plain is true when the server speaks no TLS (nothing to pin).
	Plain bool `json:"plain,omitempty"`
}

// Probe connects to the address of an Input and returns the fingerprint the
// admin has to confirm.
func (m *Manager) Probe(ctx context.Context, in Input) (*ProbeResult, error) {
	switch in.Kind {
	case KindTCPTLS, KindPortainerAgent:
		fp, err := ProbeTLS(ctx, in.Address)
		if err != nil {
			return nil, err
		}
		return &ProbeResult{Kind: in.Kind, Fingerprint: fp}, nil
	case KindSSH:
		fp, hk, err := ProbeSSH(ctx, in.Address, in.User)
		if err != nil {
			return nil, err
		}
		return &ProbeResult{Kind: in.Kind, Fingerprint: fp, HostKey: hk}, nil
	case KindErvisio:
		u, err := ParseServerURL(in.Address, in.Insecure)
		if err != nil {
			return nil, err
		}
		return probeServer(ctx, u)
	}
	return nil, errf("Unknown kind %q.", in.Kind)
}

// List returns every environment with its last status.
func (m *Manager) List() []View {
	envs := m.st.list()
	out := make([]View, 0, len(envs))
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range envs {
		out = append(out, View{Env: e, HasSecrets: e.Has(), Status: m.status[e.ID]})
	}
	return out
}

// Get returns an environment (with secrets; for the daemon only).
func (m *Manager) Get(id string) *Env { return m.st.get(id) }

// Briefs lists what a user may use.
func (m *Manager) Briefs(user string, groups []string) []Brief {
	out := []Brief{}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.st.list() {
		if e.Allowed(user, groups) {
			out = append(out, Brief{ID: e.ID, Name: e.Name, Kind: e.Kind, Address: e.DisplayAddress(), Status: m.status[e.ID]})
		}
	}
	return out
}

// Create validates and stores a new environment. For an Ervisio server it
// first redeems the pairing token.
func (m *Manager) Create(ctx context.Context, in Input, by string) (*View, error) {
	e, err := in.Validate(nil)
	if err != nil {
		return nil, err
	}
	e.ID = newID()
	e.Created = time.Now().UTC().Truncate(time.Second)
	if e.Kind == KindErvisio {
		res, err := m.redeemRemote(ctx, e, in.Token, by)
		if err != nil {
			return nil, err
		}
		e.PairedWith, e.PairID = res.Server, res.PairID
		e.SetSecret(SecretCredential, res.Credential)
	}
	if err := m.st.put(e); err != nil {
		if e.Kind == KindErvisio {
			m.revokeRemote(ctx, e)
		}
		return nil, err
	}
	st := m.Test(ctx, e.ID)
	return &View{Env: e, HasSecrets: e.Has(), Status: st}, nil
}

// Update changes an environment; empty secrets stay.
func (m *Manager) Update(ctx context.Context, id string, in Input) (*View, error) {
	old := m.st.get(id)
	if old == nil {
		return nil, errf("There is no such environment.")
	}
	in.Kind = old.Kind
	e, err := in.Validate(old)
	if err != nil {
		return nil, err
	}
	if err := m.st.put(e); err != nil {
		return nil, err
	}
	m.closeTunnels(id)
	m.dropPool(id)
	st := m.Test(ctx, id)
	return &View{Env: e, HasSecrets: e.Has(), Status: st}, nil
}

// Delete removes an environment, closes its tunnels and, for an Ervisio
// server, revokes the pairing there (best effort).
func (m *Manager) Delete(ctx context.Context, id string) error {
	e, err := m.st.remove(id)
	if err != nil {
		return err
	}
	if e == nil {
		return errf("There is no such environment.")
	}
	m.closeTunnels(id)
	m.dropPool(id)
	m.mu.Lock()
	delete(m.status, id)
	m.mu.Unlock()
	if e.Kind == KindErvisio {
		m.revokeRemote(ctx, e)
	}
	return nil
}

// friendlyTLS explains the TLS alerts an admin meets most.
func friendlyTLS(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "certificate required"), strings.Contains(msg, "bad certificate"):
		return errf("The server did not accept our client certificate (%s). Check that the certificate and key are the ones issued by the server's CA.", msg)
	case strings.Contains(msg, "tls: first record does not look like a TLS handshake"):
		return errf("That port does not speak TLS. If the Docker daemon listens without TLS, choose plain tcp (insecure).")
	}
	return err
}
