package server

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/brand"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sys"
)

// tlsConfig loads the certificate for the configured TLS mode.
func (s *Server) tlsConfig() (*tls.Config, error) {
	cfg := s.Config().TLS
	var certFile, keyFile string
	switch cfg.Mode {
	case "custom":
		if cfg.Cert == "" || cfg.Key == "" {
			return nil, errors.New("tls.mode = custom needs tls.cert and tls.key")
		}
		certFile, keyFile = cfg.Cert, cfg.Key
	case "letsencrypt":
		s.log.Printf("tls: letsencrypt is not implemented yet, using a self-signed certificate")
		fallthrough
	default:
		certFile = filepath.Join(brand.TLSDir, "self-signed.crt")
		keyFile = filepath.Join(brand.TLSDir, "self-signed.key")
		if err := ensureSelfSigned(certFile, keyFile, s.log); err != nil {
			return nil, err
		}
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load certificate: %w", err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2", "http/1.1"},
	}, nil
}

// ensureSelfSigned creates (or renews when expiring within 30 days) a
// self-signed ECDSA certificate for the host name and primary IP.
func ensureSelfSigned(certFile, keyFile string, lg *log.Logger) error {
	if data, err := os.ReadFile(certFile); err == nil {
		if block, _ := pem.Decode(data); block != nil {
			if c, err := x509.ParseCertificate(block.Bytes); err == nil && time.Until(c.NotAfter) > 30*24*time.Hour {
				if _, err := os.Stat(keyFile); err == nil {
					return nil
				}
			}
		}
	}
	lg.Printf("tls: generating self-signed certificate in %s", filepath.Dir(certFile))
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	host := sys.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host, Organization: []string{brand.Name}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(397 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{host, "localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	if ip := net.ParseIP(sys.PrimaryIP()); ip != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

// splitTLS sniffs the first byte of each connection on ln: TLS handshakes
// (0x16) go to the first listener, anything else (plain HTTP) to the second,
// which answers with a redirect to HTTPS.
func splitTLS(ln net.Listener, lg *log.Logger) (net.Listener, net.Listener) {
	tlsL := &chanListener{addr: ln.Addr(), ch: make(chan net.Conn), closed: make(chan struct{}), parent: ln}
	plainL := &chanListener{addr: ln.Addr(), ch: make(chan net.Conn), closed: tlsL.closed, parent: ln, once: &tlsL.closeOnce}
	tlsL.once = &tlsL.closeOnce
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					time.Sleep(50 * time.Millisecond)
					continue
				}
				tlsL.Close()
				return
			}
			go func() {
				_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
				br := bufio.NewReader(c)
				b, err := br.Peek(1)
				if err != nil {
					c.Close()
					return
				}
				_ = c.SetReadDeadline(time.Time{})
				pc := &peekedConn{Conn: c, r: br}
				target := plainL
				if b[0] == 0x16 {
					target = tlsL
				}
				select {
				case target.ch <- pc:
				case <-tlsL.closed:
					c.Close()
				}
			}()
		}
	}()
	return tlsL, plainL
}

type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (p *peekedConn) Read(b []byte) (int, error) { return p.r.Read(b) }

type chanListener struct {
	addr      net.Addr
	ch        chan net.Conn
	closed    chan struct{}
	parent    net.Listener
	closeOnce sync.Once
	once      *sync.Once
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *chanListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
		l.parent.Close()
	})
	return nil
}

func (l *chanListener) Addr() net.Addr { return l.addr }
