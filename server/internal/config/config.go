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
	Session   Session `toml:"session"`
	TLS       TLS     `toml:"tls"`
	Plugins   Plugins `toml:"plugins"`
}

// Login holds sign-in page and brute-force settings.
type Login struct {
	ShowIP      bool `toml:"show_ip"`
	MaxFailures int  `toml:"max_failures"`
}

// Session holds session lifetimes.
type Session struct {
	Timeout     Duration `toml:"timeout"`
	AdminUnlock Duration `toml:"admin_unlock"`
}

// TLS holds HTTPS settings. Cert/Key are used when Mode is "custom".
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

// Default returns the built-in configuration used when no file exists.
func Default() *Config {
	return &Config{
		Listen:  "0.0.0.0:9090",
		Login:   Login{ShowIP: true, MaxFailures: 5},
		Session: Session{Timeout: Duration{12 * time.Hour}, AdminUnlock: Duration{5 * time.Minute}},
		TLS:     TLS{Mode: "self-signed", Redirect: true},
		Plugins: Plugins{AllowUnsigned: true},
	}
}

// Clone returns a deep copy.
func (c *Config) Clone() *Config {
	cp := *c
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
