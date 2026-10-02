// Package config loads and saves the daemon configuration
// (/etc/linuxadmin/linuxadmin.conf, TOML).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// MaxFileSize bounds the size of a configuration file we accept to parse.
const MaxFileSize = 1 << 20

// Duration is a time.Duration written as a Go duration string ("12h", "5m").
type Duration struct{ time.Duration }

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) { return []byte(formatDuration(d.Duration)), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q", string(b))
	}
	d.Duration = v
	return nil
}

// formatDuration prints durations without the noisy zero units of
// time.Duration.String ("12h" instead of "12h0m0s").
func formatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// Config is the daemon configuration.
type Config struct {
	Listen    string  `toml:"listen"`
	AllowRoot bool    `toml:"allow_root"`
	Login     Login   `toml:"login"`
	Auth      Auth    `toml:"auth"`
	Session   Session `toml:"session"`
	TLS       TLS     `toml:"tls"`
	Plugins   Plugins `toml:"plugins"`
	Updates   Updates `toml:"updates"`
	Web       Web     `toml:"web"`
}

// Login holds sign-in page and brute-force settings.
type Login struct {
	ShowIP      bool `toml:"show_ip"`
	MaxFailures int  `toml:"max_failures"`
}

// Auth holds the sign-in methods.
type Auth struct {
	// SSHKeys allows signing in with an SSH key listed in the user's
	// authorized_keys (the browser signs a challenge; the private key
	// never leaves it).
	SSHKeys bool `toml:"ssh_keys"`
	// AllowUsers, AllowGroups and AdminsOnly restrict who may sign in. All
	// empty/false = every local account with a valid login (the default).
	// Otherwise an account may sign in when it is listed in AllowUsers, is
	// a member (primary or supplementary) of a group in AllowGroups, or
	// AdminsOnly is set and it is an administrator (root when allow_root,
	// or a member of sudo, wheel or admin). Checked for password and SSH-key
	// sign-in and for every live session (see server.revalidate).
	AllowUsers  []string `toml:"allow_users"`
	AllowGroups []string `toml:"allow_groups"`
	AdminsOnly  bool     `toml:"admins_only"`
}

// Restricted reports whether any sign-in restriction is configured.
func (a Auth) Restricted() bool {
	return len(a.AllowUsers) > 0 || len(a.AllowGroups) > 0 || a.AdminsOnly
}

// Session holds session lifetimes.
type Session struct {
	Timeout     Duration `toml:"timeout"`
	AdminUnlock Duration `toml:"admin_unlock"`
}

// TLS holds HTTPS settings. Cert/Key are used when Mode is "custom". Mode
// "http" serves plain HTTP for a reverse proxy on the same machine and is
// only accepted when listen is a loopback address.
type TLS struct {
	Mode     string `toml:"mode"`
	Redirect bool   `toml:"redirect"`
	Cert     string `toml:"cert"`
	Key      string `toml:"key"`
}

// Plugins holds the plugin policy.
type Plugins struct {
	AllowUnsigned bool `toml:"allow_unsigned"`
	Dev           bool `toml:"dev"`
}

// Updates holds the self-update settings (internal/update).
type Updates struct {
	// Channel is "stable" (releases without a pre-release suffix) or
	// "prerelease" (also -rc / -beta releases).
	Channel string `toml:"channel"`
	// AutoCheck: the daemon checks GitHub every few hours and admins see a
	// notification when a newer version exists.
	AutoCheck bool `toml:"auto_check"`
	// AutoInstall: the daemon installs a newer version by itself every day
	// at AutoInstallAt (local time, "HH:MM").
	AutoInstall   bool   `toml:"auto_install"`
	AutoInstallAt string `toml:"auto_install_at"`
}

// Web holds settings for reaching the console under other names or through
// a reverse proxy.
type Web struct {
	// AllowedOrigins are extra browser origins accepted for sign-in, API
	// calls and WebSockets, e.g. "https://admin.example.com". The address
	// the browser used is always accepted when it matches the Host header.
	AllowedOrigins []string `toml:"allowed_origins"`
	// TrustedProxies are addresses or CIDRs of reverse proxies whose
	// X-Forwarded-Host, X-Forwarded-Proto and X-Forwarded-For headers are
	// believed. Loopback is trusted by default (a proxy on the same machine).
	TrustedProxies []string `toml:"trusted_proxies"`
}

// Default returns the built-in configuration used when no file exists.
func Default() *Config {
	return &Config{
		Listen:  "0.0.0.0:9090",
		Login:   Login{ShowIP: true, MaxFailures: 5},
		Auth:    Auth{SSHKeys: true},
		Session: Session{Timeout: Duration{12 * time.Hour}, AdminUnlock: Duration{5 * time.Minute}},
		TLS:     TLS{Mode: "self-signed", Redirect: true},
		// Only signed plugins by default (security review H2); dev
		// folders are still loaded in developer mode, marked unsigned.
		Plugins: Plugins{AllowUnsigned: false},
		Updates: Updates{Channel: "stable", AutoCheck: true, AutoInstall: false, AutoInstallAt: "03:30"},
		Web:     Web{TrustedProxies: []string{"127.0.0.0/8", "::1/128"}},
	}
}

// Clone returns a deep copy.
func (c *Config) Clone() *Config {
	cp := *c
	cp.Auth.AllowUsers = append([]string(nil), c.Auth.AllowUsers...)
	cp.Auth.AllowGroups = append([]string(nil), c.Auth.AllowGroups...)
	cp.Web.AllowedOrigins = append([]string(nil), c.Web.AllowedOrigins...)
	cp.Web.TrustedProxies = append([]string(nil), c.Web.TrustedProxies...)
	return &cp
}

// Validate checks every key.
func (c *Config) Validate() error {
	var errs []error
	for _, k := range Keys() {
		if err := k.validate(k.get(c)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", k.Name, err))
		}
	}
	if c.TLS.Mode == TLSHTTP {
		if err := ValidatePlainHTTPListen(c.Listen); err != nil {
			errs = append(errs, fmt.Errorf("tls.mode: %w", err))
		}
	}
	for _, o := range c.Web.AllowedOrigins {
		if _, err := ParseOrigin(o); err != nil {
			errs = append(errs, fmt.Errorf("web.allowed_origins: %q: %w", o, err))
		}
	}
	for _, p := range c.Web.TrustedProxies {
		if _, err := ParsePrefix(p); err != nil {
			errs = append(errs, fmt.Errorf("web.trusted_proxies: %q: %w", p, err))
		}
	}
	return errors.Join(errs...)
}

// Load reads the configuration at path. A missing file yields the defaults
// and exists=false. Keys missing from the file keep their default values.
// Unknown keys are returned as warnings, not errors.
func Load(path string) (cfg *Config, exists bool, warnings []string, err error) {
	cfg = Default()
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, false, nil, nil
	}
	if err != nil {
		return nil, false, nil, err
	}
	defer f.Close()
	data, err := readLimited(f, MaxFileSize)
	if err != nil {
		return nil, true, nil, fmt.Errorf("read %s: %w", path, err)
	}
	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, true, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		warnings = append(warnings, fmt.Sprintf("unknown key %q ignored", k.String()))
	}
	sort.Strings(warnings)
	if err := cfg.Validate(); err != nil {
		return nil, true, warnings, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, true, warnings, nil
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file larger than %d bytes", limit)
	}
	return data, nil
}

// Encode renders cfg as TOML.
func Encode(cfg *Config) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# LinuxAdmin server configuration. Edited from Settings; a backup of the\n# previous version is kept next to this file with the .bak suffix.\n\n")
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Save validates cfg and writes it atomically to path. If path already
// exists its current content is first copied to path+".bak".
func Save(path string, cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := Encode(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil {
		if err := writeAtomic(path+".bak", old, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeAtomic(path, data, 0o644)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Check is what `linuxadmind --check-config` runs: it parses and validates
// the file at path (which must exist) plus the rules that only matter when
// the daemon starts (a custom certificate must be configured and present).
// Nothing is started or written.
func Check(path string) (warnings []string, err error) {
	cfg, exists, warnings, err := Load(path)
	if err != nil {
		return warnings, err
	}
	if !exists {
		return warnings, fmt.Errorf("%s does not exist", path)
	}
	var errs []error
	if cfg.TLS.Mode == "custom" {
		if cfg.TLS.Cert == "" || cfg.TLS.Key == "" {
			errs = append(errs, errors.New("tls.mode = custom needs tls.cert and tls.key"))
		}
		for _, f := range []string{cfg.TLS.Cert, cfg.TLS.Key} {
			if f == "" {
				continue
			}
			if _, e := os.Stat(f); e != nil {
				errs = append(errs, fmt.Errorf("tls: %w", e))
			}
		}
	}
	return warnings, errors.Join(errs...)
}
