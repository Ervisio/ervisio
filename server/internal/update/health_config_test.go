package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/config"
)

// Exercise the real constructor, rather than overriding Base and hiding transport bugs.
func TestHTTPHealthUsesConfiguredTransport(t *testing.T) {
	for _, mode := range []string{config.TLSHTTP, "self-signed", "custom"} {
		t.Run(mode, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/health" {
					t.Errorf("health path %s", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(HealthInfo{Status: "ok", Version: "1.2.3"})
			})
			var srv *httptest.Server
			if mode == config.TLSHTTP {
				srv = httptest.NewServer(handler)
			} else {
				srv = httptest.NewTLSServer(handler)
			}
			defer srv.Close()
			address, _ := url.Parse(srv.URL)
			cfg := config.Default()
			cfg.Listen = address.Host
			cfg.TLS.Mode = mode
			if mode == "custom" {
				cfg.TLS.Cert = filepath.Join(t.TempDir(), "cert.pem")
				cfg.TLS.Key = filepath.Join(t.TempDir(), "key.pem")
			}
			path := filepath.Join(t.TempDir(), "custom config.conf")
			if err := config.Save(path, cfg); err != nil {
				t.Fatal(err)
			}
			health := NewHTTPHealth(path)
			if health.Base != srv.URL {
				t.Fatalf("base %q, want %q", health.Base, srv.URL)
			}
			if err := health.Wait(context.Background(), "1.2.3", time.Second); err != nil {
				t.Fatal(err)
			}
		})
	}
}
