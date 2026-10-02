package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Type is the value type of a configuration key.
type Type string

// Key types.
const (
	String   Type = "string"
	Bool     Type = "bool"
	Int      Type = "int"
	Dur      Type = "duration"
	Enum     Type = "enum"
	FilePath Type = "path"
	List     Type = "list" // list of strings
)

// TLSHTTP is the tls.mode that serves plain HTTP (behind a reverse proxy).
const TLSHTTP = "http"

var tlsModes = []string{"self-signed", "letsencrypt", "custom", TLSHTTP}

// maxNames bounds auth.allow_users / auth.allow_groups.
const maxNames = 256

// nameRe is the shape of a user or group name: the portable POSIX set plus
// the "@" and trailing "$" that domain and Samba accounts use.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]{0,63}\$?$`)

func validateNames(v any) error {
	l := v.([]string)
	if len(l) > maxNames {
		return fmt.Errorf("at most %d entries", maxNames)
	}
	seen := map[string]bool{}
	for _, n := range l {
		if !nameRe.MatchString(n) {
			return fmt.Errorf("%q is not a valid user or group name (letters, digits, _ . @ -; at most 64 characters)", n)
		}
		if seen[n] {
			return fmt.Errorf("%q is listed twice", n)
		}
		seen[n] = true
	}
	return nil
}

// ValidatePlainHTTPListen checks that listen (host:port) is a loopback IP
// address, the only place tls.mode = "http" may be served from.
func ValidatePlainHTTPListen(listen string) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return errors.New("listen must be host:port")
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf(`"http" (plain HTTP) is only allowed when listen is a loopback address (127.0.0.1 or [::1]), but listen is %q: put a reverse proxy in front, or use another tls.mode`, listen)
	}
	return nil
}

// Key describes one dotted configuration key ("login.show_ip").
type Key struct {
	Name    string   `json:"key"`
	Type    Type     `json:"type"`
	Values  []string `json:"values,omitempty"`  // allowed values for Enum
	Secret  bool     `json:"secret,omitempty"`  // never returned by config.get
	Restart bool     `json:"restart,omitempty"` // takes effect after a daemon restart

	get      func(*Config) any
	set      func(*Config, any)
	validate func(any) error
}

func durationRange(min, max time.Duration) func(any) error {
	return func(v any) error {
		d := v.(Duration).Duration
		if d < min || d > max {
			return fmt.Errorf("must be between %s and %s", formatDuration(min), formatDuration(max))
		}
		return nil
	}
}

func noCheck(any) error { return nil }

var keys = []Key{
	{Name: "listen", Type: String, Restart: true,
		get: func(c *Config) any { return c.Listen }, set: func(c *Config, v any) { c.Listen = v.(string) },
		validate: func(v any) error { return validateListen(v.(string)) }},
	{Name: "allow_root", Type: Bool,
		get: func(c *Config) any { return c.AllowRoot }, set: func(c *Config, v any) { c.AllowRoot = v.(bool) }, validate: noCheck},
	{Name: "login.show_ip", Type: Bool,
		get: func(c *Config) any { return c.Login.ShowIP }, set: func(c *Config, v any) { c.Login.ShowIP = v.(bool) }, validate: noCheck},
	{Name: "login.max_failures", Type: Int,
		get: func(c *Config) any { return c.Login.MaxFailures }, set: func(c *Config, v any) { c.Login.MaxFailures = v.(int) },
		validate: func(v any) error {
			if n := v.(int); n < 1 || n > 1000 {
				return errors.New("must be between 1 and 1000")
			}
			return nil
		}},
	{Name: "auth.ssh_keys", Type: Bool,
		get: func(c *Config) any { return c.Auth.SSHKeys }, set: func(c *Config, v any) { c.Auth.SSHKeys = v.(bool) }, validate: noCheck},
	{Name: "auth.allow_users", Type: List,
		get: func(c *Config) any { return c.Auth.AllowUsers }, set: func(c *Config, v any) { c.Auth.AllowUsers = v.([]string) }, validate: validateNames},
	{Name: "auth.allow_groups", Type: List,
		get: func(c *Config) any { return c.Auth.AllowGroups }, set: func(c *Config, v any) { c.Auth.AllowGroups = v.([]string) }, validate: validateNames},
	{Name: "auth.admins_only", Type: Bool,
		get: func(c *Config) any { return c.Auth.AdminsOnly }, set: func(c *Config, v any) { c.Auth.AdminsOnly = v.(bool) }, validate: noCheck},
	{Name: "session.timeout", Type: Dur,
		get: func(c *Config) any { return c.Session.Timeout }, set: func(c *Config, v any) { c.Session.Timeout = v.(Duration) },
		validate: durationRange(5*time.Minute, 30*24*time.Hour)},
	{Name: "session.admin_unlock", Type: Dur,
		get: func(c *Config) any { return c.Session.AdminUnlock }, set: func(c *Config, v any) { c.Session.AdminUnlock = v.(Duration) },
		validate: func(v any) error {
			// 0 keeps admin rights until sign-out.
			if v.(Duration).Duration == 0 {
				return nil
			}
			return durationRange(30*time.Second, 24*time.Hour)(v)
		}},
	{Name: "tls.mode", Type: Enum, Values: tlsModes, Restart: true,
		get: func(c *Config) any { return c.TLS.Mode }, set: func(c *Config, v any) { c.TLS.Mode = v.(string) },
		validate: func(v any) error {
			if !slices.Contains(tlsModes, v.(string)) {
				return errors.New("must be self-signed, letsencrypt, custom or http")
			}
			return nil
		}},
	{Name: "tls.redirect", Type: Bool, Restart: true,
		get: func(c *Config) any { return c.TLS.Redirect }, set: func(c *Config, v any) { c.TLS.Redirect = v.(bool) }, validate: noCheck},
	{Name: "tls.cert", Type: FilePath, Restart: true,
		get: func(c *Config) any { return c.TLS.Cert }, set: func(c *Config, v any) { c.TLS.Cert = v.(string) }, validate: validatePath},
	{Name: "tls.key", Type: FilePath, Restart: true,
		get: func(c *Config) any { return c.TLS.Key }, set: func(c *Config, v any) { c.TLS.Key = v.(string) }, validate: validatePath},
	{Name: "plugins.allow_unsigned", Type: Bool,
		get: func(c *Config) any { return c.Plugins.AllowUnsigned }, set: func(c *Config, v any) { c.Plugins.AllowUnsigned = v.(bool) }, validate: noCheck},
	{Name: "plugins.dev", Type: Bool,
		get: func(c *Config) any { return c.Plugins.Dev }, set: func(c *Config, v any) { c.Plugins.Dev = v.(bool) }, validate: noCheck},
	{Name: "plugins.catalog_url", Type: String,
		get: func(c *Config) any { return c.Plugins.CatalogURL }, set: func(c *Config, v any) { c.Plugins.CatalogURL = v.(string) }, validate: validateCatalogURL},
	{Name: "plugins.catalog_key", Type: String,
		get: func(c *Config) any { return c.Plugins.CatalogKey }, set: func(c *Config, v any) { c.Plugins.CatalogKey = v.(string) }, validate: validateEd25519Key},
	{Name: "updates.channel", Type: Enum, Values: []string{"stable", "prerelease"},
		get: func(c *Config) any { return c.Updates.Channel }, set: func(c *Config, v any) { c.Updates.Channel = v.(string) },
		validate: func(v any) error {
			if !slices.Contains([]string{"stable", "prerelease"}, v.(string)) {
				return errors.New("must be stable or prerelease")
			}
			return nil
		}},
	{Name: "updates.auto_check", Type: Bool,
		get: func(c *Config) any { return c.Updates.AutoCheck }, set: func(c *Config, v any) { c.Updates.AutoCheck = v.(bool) }, validate: noCheck},
	{Name: "updates.auto_install", Type: Bool,
		get: func(c *Config) any { return c.Updates.AutoInstall }, set: func(c *Config, v any) { c.Updates.AutoInstall = v.(bool) }, validate: noCheck},
	{Name: "updates.auto_install_at", Type: String,
		get: func(c *Config) any { return c.Updates.AutoInstallAt }, set: func(c *Config, v any) { c.Updates.AutoInstallAt = v.(string) },
		validate: func(v any) error {
			_, err := ParseClock(v.(string))
			return err
		}},
}

// ParseClock parses a 24-hour "HH:MM" time of day into minutes after midnight.
func ParseClock(s string) (int, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, errClock
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, errClock
		}
	}
	h, _ := strconv.Atoi(s[:2])
	m, _ := strconv.Atoi(s[3:])
	if h > 23 || m > 59 {
		return 0, errClock
	}
	return h*60 + m, nil
}

var errClock = errors.New(`must be a time of day "HH:MM" (00:00 to 23:59)`)

// Keys returns the description of every configuration key, in file order.
func Keys() []Key { return keys }

// Lookup finds a key by its dotted name.
func Lookup(name string) (Key, bool) {
	for _, k := range keys {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

func validateListen(s string) error {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return errors.New("must be host:port")
	}
	if host != "" && net.ParseIP(host) == nil {
		return errors.New("host must be an IP address or empty")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

// validateCatalogURL accepts "" (no remote catalog) or an https URL.
func validateCatalogURL(v any) error {
	s := v.(string)
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || len(s) > 2048 {
		return errors.New("must be empty or an https:// URL")
	}
	return nil
}

// validateEd25519Key accepts "" or the base64 of a 32-byte ed25519 public key.
func validateEd25519Key(v any) error {
	s := v.(string)
	if s == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return errors.New("must be empty or the base64 of an ed25519 public key (32 bytes)")
	}
	return nil
}

func validatePath(v any) error {
	s := v.(string)
	if s == "" {
		return nil
	}
	if !filepath.IsAbs(s) || filepath.Clean(s) != s || len(s) > 4096 {
		return errors.New("must be an absolute clean path")
	}
	return nil
}

// Value returns the value of key formatted for JSON (durations as strings).
func (k Key) Value(c *Config) any {
	v := k.get(c)
	if l, ok := v.([]string); ok && l == nil {
		return []string{} // JSON [] rather than null
	}
	if d, ok := v.(Duration); ok {
		return formatDuration(d.Duration)
	}
	return v
}

// Values returns every non-secret key as a flat map for JSON.
func (c *Config) Values() map[string]any {
	m := make(map[string]any, len(keys))
	for _, k := range keys {
		if !k.Secret {
			m[k.Name] = k.Value(c)
		}
	}
	return m
}

// Set converts a JSON-decoded value (string, bool, float64, json.Number…)
// to the key's type, validates it and stores it.
func (c *Config) Set(name string, value any) error {
	k, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown key %q", name)
	}
	v, err := k.convert(value)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if err := k.validate(v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	k.set(c, v)
	return nil
}

func (k Key) convert(value any) (any, error) {
	switch k.Type {
	case Bool:
		b, ok := value.(bool)
		if !ok {
			return nil, errors.New("must be a boolean")
		}
		return b, nil
	case Int:
		switch n := value.(type) {
		case float64:
			if n != math.Trunc(n) || n < math.MinInt32 || n > math.MaxInt32 {
				return nil, errors.New("must be an integer")
			}
			return int(n), nil
		case int:
			return n, nil
		case interface{ Int64() (int64, error) }:
			i, err := n.Int64()
			if err != nil || i < math.MinInt32 || i > math.MaxInt32 {
				return nil, errors.New("must be an integer")
			}
			return int(i), nil
		}
		return nil, errors.New("must be an integer")
	case Dur:
		s, ok := value.(string)
		if !ok {
			return nil, errors.New(`must be a duration string such as "5m" or "12h"`)
		}
		var d Duration
		if err := d.UnmarshalText([]byte(s)); err != nil {
			return nil, err
		}
		return d, nil
	case List:
		var out []string
		switch l := value.(type) {
		case nil:
		case []string:
			out = append(out, l...)
		case []any:
			for _, e := range l {
				s, ok := e.(string)
				if !ok {
					return nil, errors.New("must be a list of strings")
				}
				out = append(out, s)
			}
		default:
			return nil, errors.New("must be a list of strings")
		}
		for i, s := range out {
			out[i] = strings.TrimSpace(s)
		}
		return out, nil
	default: // String, Enum, FilePath
		s, ok := value.(string)
		if !ok {
			return nil, errors.New("must be a string")
		}
		return s, nil
	}
}
