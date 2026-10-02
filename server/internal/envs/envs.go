// Package envs manages environments: admin-configured remote Docker hosts
// (tcp+TLS, SSH, Portainer agent, another Ervisio server) that a plugin's
// HTTP capability or declared command can target instead of its local unix
// socket. The daemon (root) owns the configuration, the secrets and the
// connections; a user's bridge only ever sees a per-user unix socket
// ("tunnel") the daemon serves, or, for another Ervisio server, a remote
// bridge. See docs/api/environments.md.
package envs

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kinds of environment.
const (
	KindTCPTLS         = "tcp-tls"
	KindSSH            = "ssh"
	KindPortainerAgent = "portainer-agent"
	KindErvisio        = "ervisio"
)

// FamilyDocker is the kind family a plugin capability opts into with
// "remote": "docker". Every kind can stand in for a Docker engine.
const FamilyDocker = "docker"

// Families maps a family to the kinds that serve it.
var Families = map[string][]string{
	FamilyDocker: {KindTCPTLS, KindSSH, KindPortainerAgent, KindErvisio},
}

// FamilyLocalHost is what the {env} placeholder of a command stands for when
// the command runs without an environment.
var FamilyLocalHost = map[string]string{
	FamilyDocker: "unix:///var/run/docker.sock",
}

// ServesFamily reports whether kind can stand in for the family.
func ServesFamily(kind, family string) bool {
	for _, k := range Families[family] {
		if k == kind {
			return true
		}
	}
	return false
}

// Limits.
const (
	MaxEnvs        = 64
	MaxNameLen     = 60
	MaxPEM         = 64 << 10
	DefaultSSHSock = "/var/run/docker.sock"
)

var (
	idRe       = regexp.MustCompile(`^env-[0-9a-f]{8}$`)
	hostRe     = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
	sshUserRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)
	sockPathRe = regexp.MustCompile(`^/[A-Za-z0-9_./-]{1,200}$`)
	nameBad    = regexp.MustCompile(`[\x00-\x1f\x7f]`)
	fpRe       = regexp.MustCompile(`^(sha256:[0-9a-f]{64}|SHA256:[A-Za-z0-9+/]{43})$`)
)

// ValidID reports whether s looks like an environment id.
func ValidID(s string) bool { return idRe.MatchString(s) }

// Access says who may use an environment.
type Access struct {
	// Mode is "all" (everyone who can use the plugin) or "restricted".
	Mode   string   `json:"mode"`
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
}

// Env is one environment as stored and shown. Secrets are not fields of it:
// they live in the unexported map and in the sealed part of the store, so no
// JSON encoding of an Env can carry them.
type Env struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	Created time.Time `json:"created"`
	Access  Access    `json:"access"`
	// Address is host:port (tcp-tls, ssh, portainer-agent) or the base URL
	// of the other server (ervisio).
	Address string `json:"address"`
	// User is the SSH login name.
	User string `json:"user,omitempty"`
	// SocketPath is the Docker socket on the remote host (ssh).
	SocketPath string `json:"socketPath,omitempty"`
	// Insecure allows plain tcp (tcp-tls) or plain http (ervisio).
	Insecure bool `json:"insecure,omitempty"`
	// SkipVerify turns certificate checking off (tcp-tls).
	SkipVerify bool `json:"skipVerify,omitempty"`
	// CA and ClientCert are public PEM text (tcp-tls).
	CA         string `json:"ca,omitempty"`
	ClientCert string `json:"clientCert,omitempty"`
	// Fingerprint pins the server: "sha256:<hex>" of the TLS leaf
	// certificate (tcp-tls, portainer-agent, ervisio) or the SSH host key
	// "SHA256:<base64>" (ssh).
	Fingerprint string `json:"fingerprint,omitempty"`
	// HostKey is the pinned SSH host key in authorized_keys format.
	HostKey string `json:"hostKey,omitempty"`
	// PairedWith is the other server's name (ervisio).
	PairedWith string `json:"pairedWith,omitempty"`
	// PairID is the id of the pairing on the other server (ervisio).
	PairID string `json:"pairId,omitempty"`

	secrets map[string]string
}

// Secret names kept for an environment.
const (
	SecretClientKey  = "clientKey"  // tcp-tls: PEM
	SecretSSHKey     = "sshKey"     // ssh: PEM
	SecretPassphrase = "passphrase" // ssh: key passphrase
	SecretAgent      = "agentSecret"
	SecretCredential = "credential" // ervisio: per-pair credential
)

// Secret returns a stored secret ("" when not set).
func (e *Env) Secret(name string) string { return e.secrets[name] }

// SetSecret stores a secret in memory (tests, and the pairing flow).
func (e *Env) SetSecret(name, v string) {
	if e.secrets == nil {
		e.secrets = map[string]string{}
	}
	e.secrets[name] = v
}

// Has reports which secrets are set, never their values.
func (e *Env) Has() map[string]bool {
	out := map[string]bool{}
	for k, v := range e.secrets {
		out[k] = v != ""
	}
	return out
}

func (e *Env) clone() *Env {
	c := *e
	c.secrets = map[string]string{}
	for k, v := range e.secrets {
		c.secrets[k] = v
	}
	c.Access.Users = append([]string{}, e.Access.Users...)
	c.Access.Groups = append([]string{}, e.Access.Groups...)
	return &c
}

// Status is the last health check of an environment.
type Status struct {
	Reachable     bool      `json:"reachable"`
	EngineVersion string    `json:"engineVersion,omitempty"`
	APIVersion    string    `json:"apiVersion,omitempty"`
	LatencyMs     int64     `json:"latencyMs"`
	Error         string    `json:"error,omitempty"`
	Checked       time.Time `json:"checked"`
}

// View is an environment for the Settings page: the Env, which secrets are
// set, and the last status.
type View struct {
	*Env
	HasSecrets map[string]bool `json:"hasSecrets"`
	Status     *Status         `json:"status,omitempty"`
}

// MarshalJSON flattens the Env into the view.
func (v View) MarshalJSON() ([]byte, error) {
	type flat struct {
		*Env
		HasSecrets map[string]bool `json:"hasSecrets"`
		Status     *Status         `json:"status,omitempty"`
	}
	return json.Marshal(flat{v.Env, v.HasSecrets, v.Status})
}

// Brief is what plugins.envs.list gives a plugin.
type Brief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Address is for display only: host:port (tcp-tls, portainer-agent),
	// user@host:port (ssh), the host of the other server's URL (ervisio).
	// It holds no path, credential or secret.
	Address string  `json:"address,omitempty"`
	Status  *Status `json:"status,omitempty"`
}

// DisplayAddress is the non-secret address a user who may use the
// environment sees (plugins.envs.list). Never anything but a host name or
// address, a port and, for ssh, the login name.
func (e *Env) DisplayAddress() string {
	switch e.Kind {
	case KindErvisio:
		u, err := url.Parse(e.Address)
		if err != nil || u.Host == "" {
			return ""
		}
		return u.Host // the userinfo, path and query are left out
	case KindSSH:
		if e.User != "" {
			return e.User + "@" + e.Address
		}
	}
	return e.Address
}

// Input is a create or update request. Secret fields are only read: an
// empty one on update keeps the stored value.
type Input struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Address     string  `json:"address"`
	User        string  `json:"user"`
	SocketPath  string  `json:"socketPath"`
	Insecure    bool    `json:"insecure"`
	SkipVerify  bool    `json:"skipVerify"`
	CA          string  `json:"ca"`
	ClientCert  string  `json:"clientCert"`
	Fingerprint string  `json:"fingerprint"`
	HostKey     string  `json:"hostKey"`
	Access      *Access `json:"access"`

	ClientKey   string `json:"clientKey"`
	SSHKey      string `json:"sshKey"`
	Passphrase  string `json:"passphrase"`
	AgentSecret string `json:"agentSecret"`
	// Token is the one-time pairing token created on the other server
	// (ervisio kind; Address is that server's URL).
	Token string `json:"token"`
}

// Error is a validation or runtime failure with a message fit for the UI.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errf(format string, a ...any) error { return &Error{fmt.Sprintf(format, a...)} }

// SplitHostPort checks host:port: a DNS name, IPv4 or bracketed IPv6.
func SplitHostPort(addr string) (host string, port int, err error) {
	h, p, e := net.SplitHostPort(addr)
	if e != nil {
		return "", 0, errf("Use the form host:port, for example 10.0.0.5:2376.")
	}
	n, e := strconv.Atoi(p)
	if e != nil || n < 1 || n > 65535 {
		return "", 0, errf("The port must be a number from 1 to 65535.")
	}
	if ip := net.ParseIP(h); ip == nil && !hostRe.MatchString(h) {
		return "", 0, errf("%q is not a valid host name or address.", h)
	}
	return h, n, nil
}

func validPEM(label, s string) error {
	if len(s) > MaxPEM {
		return errf("The %s is too large.", label)
	}
	if !strings.Contains(s, "-----BEGIN ") {
		return errf("The %s is not in PEM format (it should start with -----BEGIN).", label)
	}
	return nil
}

func validAccess(a *Access) error {
	switch a.Mode {
	case "all", "restricted":
	default:
		return errf("Access mode must be \"all\" or \"restricted\".")
	}
	if len(a.Users) > 256 || len(a.Groups) > 256 {
		return errf("Too many users or groups in the access list.")
	}
	for _, n := range append(append([]string{}, a.Users...), a.Groups...) {
		if n == "" || len(n) > 64 || nameBad.MatchString(n) || strings.ContainsAny(n, " /\\,") {
			return errf("%q is not a valid user or group name.", n)
		}
	}
	return nil
}

// Validate checks an Input for its kind and returns the Env to store.
// existing is the stored environment on update (its secrets stay unless
// replaced).
func (in *Input) Validate(existing *Env) (*Env, error) {
	e := &Env{secrets: map[string]string{}}
	if existing != nil {
		e = existing.clone()
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > MaxNameLen || nameBad.MatchString(name) {
		return nil, errf("Give the environment a name of 1 to %d characters.", MaxNameLen)
	}
	e.Name = name
	if existing != nil && in.Kind != existing.Kind {
		return nil, errf("The kind of an environment cannot change. Remove it and add a new one.")
	}
	e.Kind = in.Kind
	if in.Access != nil {
		if err := validAccess(in.Access); err != nil {
			return nil, err
		}
		e.Access = *in.Access
	} else if existing == nil {
		e.Access = Access{Mode: "all"}
	}
	if e.Access.Users == nil {
		e.Access.Users = []string{}
	}
	if e.Access.Groups == nil {
		e.Access.Groups = []string{}
	}
	set := func(name, v string) {
		if v != "" {
			e.secrets[name] = v
		}
	}
	pin := strings.TrimSpace(in.Fingerprint)
	if pin != "" && !fpRe.MatchString(pin) {
		return nil, errf("The fingerprint is not valid. Copy it exactly as shown.")
	}
	switch in.Kind {
	case KindTCPTLS:
		if _, _, err := SplitHostPort(in.Address); err != nil {
			return nil, err
		}
		e.Address = in.Address
		e.Insecure = in.Insecure
		e.SkipVerify = in.SkipVerify
		e.CA, e.ClientCert = strings.TrimSpace(in.CA), strings.TrimSpace(in.ClientCert)
		if e.Insecure {
			e.CA, e.ClientCert, e.Fingerprint, e.SkipVerify = "", "", "", false
			delete(e.secrets, SecretClientKey)
			break
		}
		if e.CA != "" {
			if err := validPEM("CA certificate", e.CA); err != nil {
				return nil, err
			}
		}
		if e.ClientCert != "" {
			if err := validPEM("client certificate", e.ClientCert); err != nil {
				return nil, err
			}
		}
		if in.ClientKey != "" {
			if err := validPEM("client key", in.ClientKey); err != nil {
				return nil, err
			}
		}
		set(SecretClientKey, in.ClientKey)
		if (e.ClientCert == "") != (e.secrets[SecretClientKey] == "") {
			return nil, errf("Give both the client certificate and the client key, or neither.")
		}
		e.Fingerprint = pin
		if e.SkipVerify && e.Fingerprint != "" {
			return nil, errf("Choose either a pinned fingerprint or skipping verification, not both.")
		}
		if e.ClientCert != "" {
			if err := checkKeyPair(e.ClientCert, e.secrets[SecretClientKey]); err != nil {
				return nil, errf("The client certificate and key do not match or are not valid: %v", err)
			}
		}
	case KindSSH:
		if _, _, err := SplitHostPort(in.Address); err != nil {
			return nil, err
		}
		e.Address = in.Address
		if !sshUserRe.MatchString(in.User) {
			return nil, errf("Give the SSH user name, for example docker.")
		}
		e.User = in.User
		e.SocketPath = in.SocketPath
		if e.SocketPath == "" {
			e.SocketPath = DefaultSSHSock
		}
		if !sockPathRe.MatchString(e.SocketPath) || strings.Contains(e.SocketPath, "..") {
			return nil, errf("The remote Docker socket must be an absolute path such as /var/run/docker.sock.")
		}
		if in.SSHKey != "" {
			if err := validPEM("private key", in.SSHKey); err != nil {
				return nil, err
			}
		}
		set(SecretSSHKey, in.SSHKey)
		set(SecretPassphrase, in.Passphrase)
		if e.secrets[SecretSSHKey] == "" {
			return nil, errf("Add the private key used to sign in over SSH.")
		}
		if _, err := ParseSSHKey(e.secrets[SecretSSHKey], e.secrets[SecretPassphrase]); err != nil {
			return nil, err
		}
		if hk := strings.TrimSpace(in.HostKey); hk != "" {
			k, err := ParseHostKey(hk)
			if err != nil {
				return nil, errf("The host key is not valid: %v", err)
			}
			e.HostKey = hk
			e.Fingerprint = SSHFingerprint(k)
		}
		if e.HostKey == "" {
			return nil, errf("Confirm the server's host key fingerprint first (Check connection).")
		}
	case KindPortainerAgent:
		if _, _, err := SplitHostPort(in.Address); err != nil {
			return nil, err
		}
		e.Address = in.Address
		set(SecretAgent, in.AgentSecret)
		if pin != "" {
			e.Fingerprint = pin
		}
		if e.Fingerprint == "" {
			return nil, errf("Confirm the agent's certificate fingerprint first (Check connection).")
		}
	case KindErvisio:
		u, err := ParseServerURL(in.Address, in.Insecure)
		if err != nil {
			return nil, err
		}
		e.Address = u
		e.Insecure = in.Insecure
		if existing == nil && in.Token == "" {
			return nil, errf("Paste the pairing token created on the other server.")
		}
		if pin != "" {
			e.Fingerprint = pin
		}
		if strings.HasPrefix(u, "https://") && e.Fingerprint == "" {
			return nil, errf("Confirm the other server's certificate fingerprint first (Check connection).")
		}
	default:
		return nil, errf("Unknown kind %q.", in.Kind)
	}
	return e, nil
}

// ParseServerURL checks the base URL of another Ervisio server: https, or
// http only for a loopback address or when insecure is set. Returns
// scheme://host[:port] without a trailing slash.
func ParseServerURL(raw string, insecure bool) (string, error) {
	raw = strings.TrimSpace(raw)
	var scheme, rest string
	switch {
	case strings.HasPrefix(raw, "https://"):
		scheme, rest = "https", raw[8:]
	case strings.HasPrefix(raw, "http://"):
		scheme, rest = "http", raw[7:]
	default:
		return "", errf("Start the address with https:// (for example https://server.lan:9090).")
	}
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" || strings.ContainsAny(rest, "/?#@ ") {
		return "", errf("Give only the server's address, without a path.")
	}
	host := rest
	if h, _, err := net.SplitHostPort(rest); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip == nil && !hostRe.MatchString(host) {
		return "", errf("%q is not a valid host name or address.", host)
	}
	if scheme == "http" && !insecure {
		if ip := net.ParseIP(host); !(ip != nil && ip.IsLoopback()) && host != "localhost" {
			return "", errf("Plain http sends the pairing credential unencrypted. Use https, or tick \"Allow plain http\" if you accept that risk.")
		}
	}
	return scheme + "://" + rest, nil
}

// Allowed reports whether a user may use the environment.
func (e *Env) Allowed(user string, groups []string) bool {
	if e.Access.Mode != "restricted" {
		return true
	}
	for _, u := range e.Access.Users {
		if u == user {
			return true
		}
	}
	for _, g := range e.Access.Groups {
		for _, h := range groups {
			if g == h {
				return true
			}
		}
	}
	return false
}
