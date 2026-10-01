package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"path/filepath"
	"slices"
	"strconv"
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
)

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
	{Name: "tls.mode", Type: Enum, Values: []string{"self-signed", "letsencrypt", "custom"}, Restart: true,
		get: func(c *Config) any { return c.TLS.Mode }, set: func(c *Config, v any) { c.TLS.Mode = v.(string) },
		validate: func(v any) error {
			if !slices.Contains([]string{"self-signed", "letsencrypt", "custom"}, v.(string)) {
				return errors.New("must be self-signed, letsencrypt or custom")
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
}

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
	default: // String, Enum, FilePath
		s, ok := value.(string)
		if !ok {
			return nil, errors.New("must be a string")
		}
		return s, nil
	}
}
