// Package server is ervisiod's HTTP side: sign-in with PAM, sessions,
// routing of API calls and streams to per-user bridges, file transfer,
// plugin assets and the web app.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/audit"
	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/config"
	"github.com/ervisio/ervisio/server/internal/jobs"
	"github.com/ervisio/ervisio/server/internal/notify"
	"github.com/ervisio/ervisio/server/internal/sshauth"
)

// Options configure the daemon.
type Options struct {
	ConfigPath string
	// Dev: no TLS, no root needed, only the daemon's own user may sign in,
	// bridges run without changing uid, web proxied to Vite unless WebDir.
	Dev bool
	// NoAuth (dev only) signs every request in as the daemon's user.
	NoAuth bool
	// Listen overrides the configured listen address.
	Listen string
	// WebDir is the built web app ("" in dev = proxy to ViteURL).
	WebDir string
	// ViteURL is the Vite dev server.
	ViteURL string
	// Bridge is the absolute path of ervisio-bridge.
	Bridge string
	// PluginDirs are searched in order for /plugins/<id>/<file>; then the
	// folders the user loaded with plugins.loadDev (through their bridge).
	PluginDirs []string
	// DevPluginsDir (dev only) is the repository's ./plugins folder, passed
	// to the bridges so they list its plugins.
	DevPluginsDir string
	// DevPluginsFile (dev only, with --dev-state-dir) keeps the list of
	// plugins.loadDev folders, instead of ~/.config/ervisio/plugins-dev.json.
	DevPluginsFile string
	// SessionHelper is the daemon's own binary, started as
	// `--pam-session-helper` to open a PAM session around each user bridge
	// (not used in --dev). "" = no PAM session.
	SessionHelper string
	// DevAuthorizedKeys (dev only) replaces the authorized_keys files for
	// SSH-key sign-in, so the flow can be tried without touching ~/.ssh.
	DevAuthorizedKeys string
	// StateDir keeps the daemon's state: the activity log (StateDir/audit)
	// and the plugin job instances (StateDir/jobs). "" = /var/lib/ervisio;
	// --dev-state-dir in dev.
	StateDir string
	// NotifyFile is the notification channels file, which holds secrets
	// (0600). "" = notify.json next to the configuration file.
	NotifyFile string
	// EnvsDir and TunnelDir override where environments are stored
	// (default /var/lib/ervisio/envs) and where their per-user tunnel sockets
	// live (default /run/ervisio/tunnels).
	EnvsDir, TunnelDir string
	Logger             *log.Logger
}

// configAudit gives the activity log the live configuration.
type configAudit struct{ h *configHolder }

func (c configAudit) AuditEnabled() bool      { return c.h.get().Audit.Enabled }
func (c configAudit) AuditRetentionDays() int { return c.h.get().Audit.RetentionDays }

// Server is the daemon.
type Server struct {
	opts     Options
	log      *log.Logger
	cfg      *configHolder
	sessions *store
	limiter  *limiter
	// audit is the activity log; transfers the one-time plugin transfers.
	audit     *audit.Log
	transfers *transferStore
	// challenges holds outstanding SSH-key sign-in nonces.
	challenges *sshauth.Store
	pamSem     chan struct{}
	devUser    *account.Account // dev mode: the only account allowed

	noAuthMu    sync.Mutex
	noAuthToken string // --dev-insecure-noauth one-time sign-in token

	viteHost string // dev: host:port of the Vite dev server (allowed origin)
	checker  *accountChecker
	// pamAcctHook replaces PAM account management in tests.
	pamAcctHook func(name, rhost string) error
	checking    sync.Mutex // one revalidation pass at a time

	baseCtx context.Context
	cancel  context.CancelFunc
	vite    http.Handler

	// jobs runs plugin job instances; notifier sends notifications
	// (jobsglue.go). jobs is nil when its state file cannot be read.
	jobs     *jobs.Manager
	notifier *notify.Service
	jobPool  *bridgePool
	// env is the environments feature (envs.go).
	env *envState
}

// New validates options and loads the configuration.
func New(opts Options) (*Server, error) {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	if opts.NoAuth && !opts.Dev {
		return nil, errors.New("--dev-insecure-noauth requires --dev")
	}
	if opts.DevAuthorizedKeys != "" && !opts.Dev {
		return nil, errors.New("--dev-authorized-keys requires --dev")
	}
	if opts.NoAuth && os.Geteuid() == 0 {
		return nil, errors.New("--dev-insecure-noauth refuses to run as root")
	}
	if !opts.Dev && os.Geteuid() != 0 {
		return nil, errors.New("ervisiod must run as root (use --dev for development)")
	}
	if fi, err := os.Stat(opts.Bridge); err != nil || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("bridge binary %q not found (use --bridge)", opts.Bridge)
	}
	holder, err := newConfigHolder(opts.ConfigPath, opts.Logger)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		opts:       opts,
		log:        opts.Logger,
		cfg:        holder,
		sessions:   newStore(),
		limiter:    newLimiter(),
		challenges: sshauth.NewStore(),
		pamSem:     make(chan struct{}, 8),
		checker:    newAccountChecker(),
		baseCtx:    ctx,
		cancel:     cancel,
	}
	s.checker.keyAuth = s.keyAuthorized
	stateDir := opts.StateDir
	if stateDir == "" {
		stateDir = brand.StateDir
	}
	s.audit = audit.New(filepath.Join(stateDir, "audit"), configAudit{holder})
	s.transfers = newTransferStore()
	if err := s.initJobs(); err != nil {
		opts.Logger.Printf("background jobs are off: %v", err)
	}
	s.env = s.newEnvState()
	if opts.Dev {
		if s.devUser, err = account.Current(); err != nil {
			cancel()
			return nil, fmt.Errorf("resolve current user: %w", err)
		}
		if opts.WebDir == "" {
			u, err := url.Parse(opts.ViteURL)
			if err != nil || u.Host == "" {
				cancel()
				return nil, fmt.Errorf("invalid Vite URL %q", opts.ViteURL)
			}
			s.vite = newViteProxy(u, s.log)
			s.viteHost = u.Host
		}
	}
	return s, nil
}

// Config returns the current configuration.
func (s *Server) Config() *config.Config { return s.cfg.get() }

// ListenAddr is the effective listen address.
func (s *Server) ListenAddr() string {
	if s.opts.Listen != "" {
		return s.opts.Listen
	}
	if s.opts.Dev {
		return "127.0.0.1:9090"
	}
	return s.Config().Listen
}

// Handler returns the HTTP handler with every route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/public/host", s.handlePublicHost)
	mux.HandleFunc("GET /api/public/logo", s.handlePublicLogo)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/auth/login", s.csrf(s.handleLogin))
	mux.HandleFunc("POST /api/auth/challenge", s.csrf(s.handleChallenge))
	mux.HandleFunc("POST /api/auth/login-key", s.csrf(s.handleLoginKey))
	mux.HandleFunc("POST /api/auth/logout", s.csrf(s.handleLogout))
	mux.HandleFunc("GET /api/auth/session", s.authed(s.handleSession))
	mux.HandleFunc("POST /api/auth/unlock", s.authed(s.csrfS(s.handleUnlock)))
	mux.HandleFunc("POST /api/auth/lock", s.authed(s.csrfS(s.handleLock)))
	mux.HandleFunc("POST /api/rpc", s.authed(s.csrfS(s.handleRPC)))
	mux.HandleFunc("GET /api/ws", s.authed(s.handleWS))
	mux.HandleFunc("POST /api/plugins/transfer", s.authed(s.csrfS(s.handleTransferStart)))
	mux.HandleFunc("GET /api/plugins/transfer/{token}", s.authed(s.handleTransfer))
	mux.HandleFunc("GET /api/plugins/transfer/{token}/status", s.authed(s.handleTransferStatus))
	mux.HandleFunc("POST /api/plugins/transfer/{token}", s.authed(s.csrfS(s.handleTransfer)))
	mux.HandleFunc("GET /api/audit/export", s.authed(s.handleAuditExport))
	mux.HandleFunc("GET /api/files/download", s.authed(s.handleDownload))
	mux.HandleFunc("POST /api/files/upload", s.authed(s.csrfS(s.handleUpload)))
	s.registerPair(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, errNotFound)
	})
	if s.opts.NoAuth {
		mux.HandleFunc("GET /api/dev/noauth", s.handleNoAuth)
	}
	mux.HandleFunc("GET /plugins/{id}/{file...}", s.authed(s.handlePlugin))
	mux.HandleFunc("GET /plugin-frame/{id}", s.authed(s.handlePluginFrame))
	// Webhooks of plugin jobs: no session and no CSRF header by design,
	// the random token in the path is the credential (docs/api/jobs.md).
	mux.HandleFunc("POST /hooks/{plugin}/{token}", s.handleHook)
	mux.Handle("/", s.webHandler())
	var h http.Handler = mux
	if s.opts.Dev {
		h = s.devHostCheck(h)
	}
	return securityHeaders(h)
}

// Run serves until ctx is cancelled, then shuts down gracefully and stops
// every bridge.
func (s *Server) Run(ctx context.Context) error {
	addr := s.ListenAddr()
	plain := !s.opts.Dev && s.Config().TLS.Mode == config.TLSHTTP
	if plain {
		// Also covers --listen overriding the configured address.
		if err := config.ValidatePlainHTTPListen(addr); err != nil {
			return fmt.Errorf("tls.mode: %w", err)
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if s.opts.DevAuthorizedKeys != "" {
		// The file replaces every account's authorized_keys and skips the
		// StrictModes checks on ~/.ssh: only for a daemon reachable from
		// this machine, with a file only the daemon's user can change.
		host, _, _ := net.SplitHostPort(ln.Addr().String())
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			ln.Close()
			return errors.New("--dev-authorized-keys only listens on a loopback address")
		}
		if _, err := readDevAuthorizedKeys(s.opts.DevAuthorizedKeys); err != nil && !errors.Is(err, fs.ErrNotExist) {
			ln.Close()
			return err
		}
	}
	if s.opts.NoAuth {
		host, _, _ := net.SplitHostPort(ln.Addr().String())
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			ln.Close()
			return errors.New("--dev-insecure-noauth only listens on a loopback address")
		}
		u, err := s.newNoAuthToken("http://" + ln.Addr().String())
		if err != nil {
			ln.Close()
			return err
		}
		s.log.Printf("WARNING: --dev-insecure-noauth: open this one-time URL to sign in as %q without a password: %s", s.devUser.Name, u)
	}

	handler := s.Handler()
	newSrv := func(h http.Handler) *http.Server {
		return &http.Server{
			Handler:           h,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    64 << 10,
			BaseContext:       func(net.Listener) context.Context { return s.baseCtx },
			ErrorLog:          s.log,
		}
	}
	srv := newSrv(handler)
	var redirect *http.Server

	go s.janitor(ctx)
	go s.audit.Prune()
	// Background work (jobs, webhooks) records to the same log through audit.Record.
	s.audit.Errorf = s.log.Printf
	audit.SetDefault(s.audit)
	defer audit.ClearDefault(s.audit)
	if s.jobs != nil {
		go s.jobs.Run(ctx)
	}
	go s.runAlertWatch(ctx)
	go s.envLoop(ctx)

	errCh := make(chan error, 2)
	if s.opts.Dev {
		s.log.Printf("listening on http://%s (dev mode)", ln.Addr())
		go func() { errCh <- srv.Serve(ln) }()
	} else if plain {
		s.log.Printf("listening on http://%s (tls.mode = http: plain HTTP for a reverse proxy on this machine)", ln.Addr())
		go func() { errCh <- srv.Serve(ln) }()
	} else {
		tlsCfg, err := s.tlsConfig()
		if err != nil {
			ln.Close()
			return err
		}
		srv.TLSConfig = tlsCfg
		tlsLn, plainLn := splitTLS(ln, s.log)
		go func() { errCh <- srv.ServeTLS(tlsLn, "", "") }()
		redirect = newSrv(redirectHandler(s.Config().TLS.Redirect))
		go func() { errCh <- redirect.Serve(plainLn) }()
		s.log.Printf("listening on https://%s", ln.Addr())
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			s.shutdown(srv, redirect)
			return err
		}
	}
	s.shutdown(srv, redirect)
	return nil
}

func (s *Server) shutdown(srv, redirect *http.Server) {
	s.log.Printf("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.cancel() // ends websocket handlers and in-flight calls
	if s.jobs != nil {
		s.jobs.CancelAll()
		s.jobs.Wait()
	}
	if s.jobPool != nil {
		s.jobPool.closeAll()
	}
	_ = srv.Shutdown(sctx)
	if redirect != nil {
		_ = redirect.Shutdown(sctx)
	}
	var wg sync.WaitGroup
	for _, sess := range s.sessions.all() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.sessions.remove(sess)
		}()
	}
	wg.Wait()
}

// revalidateEvery is how often live sessions are checked against the
// account database (see revalidate).
const revalidateEvery = 60 * time.Second

// janitor expires idle sessions and admin unlocks, prunes the limiter and
// re-checks the accounts of live sessions.
func (s *Server) janitor(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cfg := s.Config()
			s.sessions.expire(time.Now(), cfg.Session.Timeout.Duration, cfg.Session.AdminUnlock.Duration)
			s.limiter.gc()
			s.challenges.Prune()
			s.transfers.gc()
			go s.revalidateAll(time.Now(), revalidateEvery)
		}
	}
}

// configHolder reloads the configuration file when it changes, so settings
// saved by the root bridge (config.set) apply without a restart.
type configHolder struct {
	path string
	log  *log.Logger

	mu      sync.Mutex
	cfg     *config.Config
	mtime   time.Time
	size    int64
	checked time.Time
}

func newConfigHolder(path string, lg *log.Logger) (*configHolder, error) {
	h := &configHolder{path: path, log: lg}
	cfg, exists, warn, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	for _, w := range warn {
		lg.Printf("config: %s", w)
	}
	if !exists {
		lg.Printf("config: %s not found, using defaults", path)
	}
	h.cfg = cfg
	h.stamp()
	return h, nil
}

func (h *configHolder) stamp() {
	if fi, err := os.Stat(h.path); err == nil {
		h.mtime, h.size = fi.ModTime(), fi.Size()
	} else {
		h.mtime, h.size = time.Time{}, -1
	}
	h.checked = time.Now()
}

func (h *configHolder) get() *config.Config {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Since(h.checked) < 2*time.Second {
		return h.cfg
	}
	oldM, oldS := h.mtime, h.size
	h.stamp()
	if h.mtime.Equal(oldM) && h.size == oldS {
		return h.cfg
	}
	cfg, _, _, err := config.Load(h.path)
	if err != nil {
		h.log.Printf("config: reload failed, keeping previous settings: %v", err)
		return h.cfg
	}
	h.log.Printf("config: reloaded %s", h.path)
	h.cfg = cfg
	return h.cfg
}
