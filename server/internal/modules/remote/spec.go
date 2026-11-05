package remote

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// specParams is what remote.save and remote.test receive. Secrets are
// pointers: absent keeps the saved one, "" removes it.
type specParams struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Path     string `json:"path"`
	Auth     string `json:"auth"`
	HostKey  string `json:"hostKey"`
	TLS      string `json:"tls"`
	Share    string `json:"share"`
	Domain   string `json:"domain"`
	URL      string `json:"url"`
	Insecure bool   `json:"insecure"`
	// ResetHostKey forgets the pinned SFTP key (the server was reinstalled).
	ResetHostKey bool    `json:"resetHostKey"`
	Password     *string `json:"password"`
	Key          *string `json:"key"`
	Pass         *string `json:"passphrase"`
}

func defaultPort(c *Conn) int {
	switch c.Kind {
	case KindSFTP:
		return 22
	case KindFTP:
		if c.TLS == "implicit" {
			return 990
		}
		return 21
	case KindSMB:
		return 445
	}
	return 0
}

func plainText(s string, max int) bool {
	if len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil {
		return true
	}
	for _, r := range h {
		if !hostRune(r) {
			return false
		}
	}
	return !strings.HasPrefix(h, "-") && !strings.HasPrefix(h, ".")
}

func hostRune(r rune) bool {
	switch {
	case r == '.' || r == '-' || r == '_':
		return true
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return false
}

// build turns params into a validated connection. old is the saved
// connection when editing (its secrets are kept unless replaced).
func build(p specParams, old *Conn) (*Conn, error) {
	c := connFromParams(p, old)
	steps := []func(*Conn) error{validateBasics, resolveAddress, applyKindRules}
	for _, step := range steps {
		if err := step(c); err != nil {
			return nil, err
		}
	}
	dropUnusedFields(c)
	if err := applyName(c); err != nil {
		return nil, err
	}
	keepPinnedHostKey(c, old, p.ResetHostKey)
	c.Updated = time.Now().UnixNano()
	return c, nil
}

func connFromParams(p specParams, old *Conn) *Conn {
	c := &Conn{
		Name: strings.TrimSpace(p.Name), Kind: p.Kind, Host: strings.TrimSpace(p.Host), Port: p.Port,
		User: p.User, Path: strings.TrimSpace(p.Path), Auth: p.Auth, HostKey: strings.TrimSpace(p.HostKey),
		TLS: p.TLS, Share: strings.Trim(strings.TrimSpace(p.Share), `/\`), Domain: strings.TrimSpace(p.Domain),
		URL: strings.TrimSpace(p.URL), Insecure: p.Insecure, secrets: map[string]string{},
	}
	if old != nil {
		c.ID = old.ID
		for k, v := range old.secrets {
			c.secrets[k] = v
		}
	}
	set := func(k string, v *string) {
		if v != nil {
			c.secrets[k] = *v
		}
	}
	set(secPassword, p.Password)
	set(secKey, p.Key)
	set(secPassphrase, p.Pass)
	return c
}

func validateBasics(c *Conn) error {
	switch c.Kind {
	case KindSFTP, KindFTP, KindSMB, KindWebDAV:
	default:
		return rpc.Errorf(rpc.Invalid, "Choose a protocol: SFTP, FTP, SMB or WebDAV.")
	}
	if !plainText(c.User, 256) || !plainText(c.Domain, 256) {
		return rpc.Errorf(rpc.Invalid, "The user name contains characters that are not allowed.")
	}
	if err := validateSecrets(c.secrets); err != nil {
		return err
	}
	if c.Path == "" {
		return nil
	}
	cp, err := cleanRemote(c.Path)
	if err != nil {
		return rpc.Errorf(rpc.Invalid, "The start folder must begin with /.")
	}
	c.Path = cp
	return nil
}

func validateSecrets(secrets map[string]string) error {
	for k, v := range secrets {
		if len(v) > 64<<10 || strings.ContainsRune(v, 0) || (k != secKey && strings.ContainsAny(v, "\r\n")) {
			return rpc.Errorf(rpc.Invalid, "The %s is not valid.", k)
		}
	}
	return nil
}

func resolveAddress(c *Conn) error {
	resolve := resolveHostAddress
	if c.Kind == KindWebDAV {
		resolve = resolveWebDAVAddress
	}
	if err := resolve(c); err != nil {
		return err
	}
	if c.Port < 0 || c.Port > 65535 {
		return rpc.Errorf(rpc.Invalid, "The port must be between 1 and 65535.")
	}
	return nil
}

func resolveWebDAVAddress(c *Conn) error {
	u, err := url.Parse(c.URL)
	if err != nil || !plainWebDAVURL(u) {
		return rpc.Errorf(rpc.Invalid, "Enter the WebDAV address as https://server/path (without a user name, query or #).")
	}
	if len(c.URL) > 2048 || !validHost(u.Hostname()) {
		return rpc.Errorf(rpc.Invalid, "The WebDAV address is not valid.")
	}
	c.Host, c.Port = u.Hostname(), 0
	if u.Port() != "" {
		c.Port, _ = strconv.Atoi(u.Port())
	}
	return nil
}

func plainWebDAVURL(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func resolveHostAddress(c *Conn) error {
	c.URL = ""
	if !validHost(c.Host) {
		return rpc.Errorf(rpc.Invalid, "Enter the server name or IP address, without a protocol or path.")
	}
	c.Host = strings.Trim(c.Host, "[]")
	if c.Port == 0 {
		c.Port = defaultPort(c)
	}
	return nil
}

func applyKindRules(c *Conn) error {
	switch c.Kind {
	case KindSFTP:
		return applySFTPRules(c)
	case KindFTP:
		switch c.TLS {
		case "explicit", "implicit", "none":
		case "":
			c.TLS = "explicit"
		default:
			return rpc.Errorf(rpc.Invalid, "Choose how FTP is secured: explicit TLS, implicit TLS or none.")
		}
	case KindSMB:
		if c.Share == "" || !plainText(c.Share, 80) || strings.ContainsAny(c.Share, `/\:*?"<>|`) {
			return rpc.Errorf(rpc.Invalid, "Enter the name of the shared folder (for example: public).")
		}
	}
	return nil
}

func applySFTPRules(c *Conn) error {
	if c.User == "" {
		return rpc.Errorf(rpc.Invalid, "SFTP needs a user name.")
	}
	if c.Auth != "key" {
		c.Auth = "password"
	}
	if c.HostKey != "" {
		if _, err := parseHostKey(c.HostKey); err != nil {
			return rpc.Errorf(rpc.Invalid, "The pinned host key is not valid.")
		}
	}
	if c.Auth == "key" {
		delete(c.secrets, secPassword)
	} else {
		delete(c.secrets, secKey)
		delete(c.secrets, secPassphrase)
	}
	return nil
}

// dropUnusedFields clears what the chosen protocol does not use.
func dropUnusedFields(c *Conn) {
	if c.Kind != KindSFTP {
		c.Auth, c.HostKey = "", ""
		delete(c.secrets, secKey)
		delete(c.secrets, secPassphrase)
	}
	if c.Kind != KindSMB {
		c.Share, c.Domain = "", ""
	}
	if c.Kind != KindFTP {
		c.TLS = ""
	}
	if c.Kind != KindFTP && c.Kind != KindWebDAV {
		c.Insecure = false
	}
}

func applyName(c *Conn) error {
	if c.Name == "" {
		c.Name = c.Host
		if c.Kind == KindSMB {
			c.Name = c.Host + "/" + c.Share
		}
	}
	if !plainText(c.Name, 64) {
		return rpc.Errorf(rpc.Invalid, "Use a shorter name (64 characters at most).")
	}
	return nil
}

// keepPinnedHostKey: an edit keeps the pinned key of the same server; a
// different server must be trusted again.
func keepPinnedHostKey(c, old *Conn, reset bool) {
	sameServer := old != nil && old.Kind == c.Kind && old.Host == c.Host && old.Port == c.Port
	if c.Kind == KindSFTP && c.HostKey == "" && !reset && sameServer {
		c.HostKey = old.HostKey
	}
}

// address is host:port for the dialers.
func (c *Conn) address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}
