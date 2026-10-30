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
		ok := r == '.' || r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return !strings.HasPrefix(h, "-") && !strings.HasPrefix(h, ".")
}

// build turns params into a validated connection. old is the saved
// connection when editing (its secrets are kept unless replaced).
func build(p specParams, old *Conn) (*Conn, error) {
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

	switch c.Kind {
	case KindSFTP, KindFTP, KindSMB, KindWebDAV:
	default:
		return nil, rpc.Errorf(rpc.Invalid, "Choose a protocol: SFTP, FTP, SMB or WebDAV.")
	}
	if !plainText(c.User, 256) || !plainText(c.Domain, 256) {
		return nil, rpc.Errorf(rpc.Invalid, "The user name contains characters that are not allowed.")
	}
	for k, v := range c.secrets {
		if len(v) > 64<<10 || strings.ContainsRune(v, 0) || (k != secKey && strings.ContainsAny(v, "\r\n")) {
			return nil, rpc.Errorf(rpc.Invalid, "The %s is not valid.", k)
		}
	}
	if c.Path != "" {
		cp, err := cleanRemote(c.Path)
		if err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "The start folder must begin with /.")
		}
		c.Path = cp
	}
	if c.Kind == KindWebDAV {
		u, err := url.Parse(c.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, rpc.Errorf(rpc.Invalid, "Enter the WebDAV address as https://server/path (without a user name, query or #).")
		}
		if len(c.URL) > 2048 || !validHost(u.Hostname()) {
			return nil, rpc.Errorf(rpc.Invalid, "The WebDAV address is not valid.")
		}
		c.Host, c.Port = u.Hostname(), 0
		if u.Port() != "" {
			c.Port, _ = strconv.Atoi(u.Port())
		}
	} else {
		c.URL = ""
		if !validHost(c.Host) {
			return nil, rpc.Errorf(rpc.Invalid, "Enter the server name or IP address, without a protocol or path.")
		}
		c.Host = strings.Trim(c.Host, "[]")
		if c.Port == 0 {
			c.Port = defaultPort(c)
		}
	}
	if c.Port < 0 || c.Port > 65535 {
		return nil, rpc.Errorf(rpc.Invalid, "The port must be between 1 and 65535.")
	}
	switch c.Kind {
	case KindSFTP:
		if c.User == "" {
			return nil, rpc.Errorf(rpc.Invalid, "SFTP needs a user name.")
		}
		if c.Auth != "key" {
			c.Auth = "password"
		}
		if c.HostKey != "" {
			if _, err := parseHostKey(c.HostKey); err != nil {
				return nil, rpc.Errorf(rpc.Invalid, "The pinned host key is not valid.")
			}
		}
		if c.Auth == "key" {
			delete(c.secrets, secPassword)
		} else {
			delete(c.secrets, secKey)
			delete(c.secrets, secPassphrase)
		}
	case KindFTP:
		switch c.TLS {
		case "explicit", "implicit", "none":
		case "":
			c.TLS = "explicit"
		default:
			return nil, rpc.Errorf(rpc.Invalid, "Choose how FTP is secured: explicit TLS, implicit TLS or none.")
		}
	case KindSMB:
		if c.Share == "" || !plainText(c.Share, 80) || strings.ContainsAny(c.Share, `/\:*?"<>|`) {
			return nil, rpc.Errorf(rpc.Invalid, "Enter the name of the shared folder (for example: public).")
		}
	}
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
	if c.Name == "" {
		c.Name = c.Host
		if c.Kind == KindSMB {
			c.Name = c.Host + "/" + c.Share
		}
	}
	if !plainText(c.Name, 64) {
		return nil, rpc.Errorf(rpc.Invalid, "Use a shorter name (64 characters at most).")
	}
	// An edit keeps the pinned key of the same server; a different server
	// must be trusted again.
	if c.Kind == KindSFTP && c.HostKey == "" && !p.ResetHostKey && old != nil && old.Kind == c.Kind && old.Host == c.Host && old.Port == c.Port {
		c.HostKey = old.HostKey
	}
	c.Updated = time.Now().UnixNano()
	return c, nil
}

// address is host:port for the dialers.
func (c *Conn) address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}
