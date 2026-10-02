package envs

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"time"
)

// dialTimeout bounds connecting (TCP, TLS or SSH handshake).
const dialTimeout = 10 * time.Second

// CertFingerprint is "sha256:<hex>" of a DER certificate.
func CertFingerprint(der []byte) string {
	h := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(h[:])
}

func checkKeyPair(certPEM, keyPEM string) error {
	_, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	return err
}

// pinnedConfig is a TLS config that accepts exactly the certificate with the
// pinned fingerprint (the chain is not checked: self-signed certificates are
// the point). An empty pin accepts anything and records the leaf, which is
// how a first connection learns the fingerprint to show to the admin.
func pinnedConfig(host, pin string, seen *string, certs ...tls.Certificate) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         host,
		InsecureSkipVerify: true, // replaced by VerifyConnection below
		Certificates:       certs,
		ClientSessionCache: tls.NewLRUClientSessionCache(8),
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the server sent no certificate")
			}
			fp := CertFingerprint(cs.PeerCertificates[0].Raw)
			if seen != nil {
				*seen = fp
			}
			if pin != "" && fp != pin {
				return errf("The certificate of this server changed (now %s, pinned %s). If you expected that, remove the pin and confirm the new fingerprint.", fp, pin)
			}
			return nil
		},
	}
}

// tlsClientConfig builds the TLS config of a tcp-tls environment.
func tlsClientConfig(e *Env) (*tls.Config, error) {
	host, _, err := SplitHostPort(e.Address)
	if err != nil {
		return nil, err
	}
	var certs []tls.Certificate
	if e.ClientCert != "" {
		kp, err := tls.X509KeyPair([]byte(e.ClientCert), []byte(e.Secret(SecretClientKey)))
		if err != nil {
			return nil, errf("The client certificate is not usable: %v", err)
		}
		certs = append(certs, kp)
	}
	if e.Fingerprint != "" {
		return pinnedConfig(host, e.Fingerprint, nil, certs...), nil
	}
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         host,
		Certificates:       certs,
		InsecureSkipVerify: e.SkipVerify, //nolint:gosec // an explicit admin choice, off by default
		ClientSessionCache: tls.NewLRUClientSessionCache(8),
	}
	if e.CA != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(e.CA)) {
			return nil, errf("The CA certificate holds no certificate.")
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

// ProbeTLS connects to host:port over TLS without verifying and returns the
// leaf certificate fingerprint (shown to the admin to confirm and pin).
func ProbeTLS(ctx context.Context, addr string) (string, error) {
	host, _, err := SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	var seen string
	cfg := pinnedConfig(host, "", &seen)
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: dialTimeout}, Config: cfg}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", errf("Could not connect to %s: %v", addr, cleanNetErr(err))
	}
	c.Close()
	return seen, nil
}

// dialTLS opens a TLS connection with cfg.
func dialTLS(ctx context.Context, addr string, cfg *tls.Config) (net.Conn, error) {
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}, Config: cfg}
	return d.DialContext(ctx, "tcp", addr)
}

func dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, "tcp", addr)
}

func cleanNetErr(err error) string {
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Err != nil {
		return oe.Err.Error()
	}
	return err.Error()
}
