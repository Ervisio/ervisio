package envs

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The pairing client against a TLS server with a self-signed certificate: the
// probe learns the fingerprint, redeeming works only with the right pin.
func TestPairingOverPinnedTLS(t *testing.T) {
	cert, key, der := selfSigned(t, false)
	kp, _ := tls.X509KeyPair([]byte(cert), []byte(key))
	var gotName string
	mux := http.NewServeMux()
	mux.HandleFunc(PathInfo, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"service": "ervisio", "name": "other"})
	})
	mux.HandleFunc(PathRedeem, func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Token, Name string }
		json.NewDecoder(r.Body).Decode(&req)
		gotName = req.Name
		if req.Token != "ept_good" {
			w.WriteHeader(403)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "This pairing token is not valid."}})
			return
		}
		json.NewEncoder(w).Encode(RedeemResult{Credential: "epc_xyz", PairID: "pair-1", Server: "other", User: "alice"})
	})
	ts := httptest.NewUnstartedServer(mux)
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{kp}, NextProtos: []string{"http/1.1"}}
	ts.StartTLS()
	defer ts.Close()
	base := "https://" + ts.Listener.Addr().String()

	pr, err := probeServer(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Fingerprint != CertFingerprint(der) || pr.Plain {
		t.Fatalf("probe: %+v", pr)
	}
	m, _ := NewManager(Options{Dir: t.TempDir(), ServerName: "this-server"})
	env := &Env{Address: base, Fingerprint: pr.Fingerprint}
	res, err := m.redeemRemote(context.Background(), env, "ept_good", "admin")
	if err != nil || res.Credential != "epc_xyz" || gotName != "this-server" {
		t.Fatalf("redeem: %+v %v (name %q)", res, err, gotName)
	}
	if _, err := m.redeemRemote(context.Background(), env, "ept_bad", "admin"); err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Fatalf("a refused token must show the server's reason, got %v", err)
	}
	// A different certificate behind the same address is refused before anything is sent.
	env.Fingerprint = "sha256:" + strings.Repeat("0", 64)
	gotName = ""
	if _, err := m.redeemRemote(context.Background(), env, "ept_good", "admin"); err == nil || !strings.Contains(err.Error(), "changed") || gotName != "" {
		t.Fatalf("pin mismatch: %v (server saw %q)", err, gotName)
	}
	// Something that is not an Ervisio server is not paired.
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	if _, err := probeServer(context.Background(), other.URL); err == nil {
		t.Fatal("a non-Ervisio server was accepted")
	}
}
