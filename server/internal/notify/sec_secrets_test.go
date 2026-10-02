package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoredSecretsDoNotFollowANewServer(t *testing.T) {
	var got string
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Gotify-Key")
	}))
	defer evil.Close()
	s := New(filepath.Join(t.TempDir(), "notify.json"))
	saved, err := s.Store.Save(Spec{Name: "g", Type: TypeGotify, Enabled: true, Server: "https://gotify.example.org", Token: "SECRET-GOTIFY-TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Test(context.Background(), Spec{ID: saved.ID, Name: "g", Type: TypeGotify, Server: evil.URL})
	if got == "SECRET-GOTIFY-TOKEN" {
		t.Fatal("the stored token went to the new server")
	}
	// Saving with a new server does not keep the token either.
	if _, err := s.Store.Save(Spec{ID: saved.ID, Name: "g", Type: TypeGotify, Enabled: true, Server: "https://other.example.org"}); err == nil {
		if sp, _ := s.Store.Get(saved.ID); sp != nil && sp.Token == "SECRET-GOTIFY-TOKEN" {
			t.Fatal("the stored token was kept for a new server")
		}
	}
	// The same server (other spelling) keeps it.
	sp, err := s.Store.Save(Spec{Name: "g2", Type: TypeGotify, Enabled: true, Server: "https://gotify.example.org", Token: "TOKEN-TWO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Save(Spec{ID: sp.ID, Name: "g2b", Type: TypeGotify, Enabled: true, Server: " https://GOTIFY.example.org/ "}); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Store.Get(sp.ID); again == nil || again.Token != "TOKEN-TWO" {
		t.Fatalf("same server lost its token: %+v", again)
	}
}

func TestStoredWebhookHeaderDoesNotFollowANewURL(t *testing.T) {
	var got string
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Secret")
	}))
	defer evil.Close()
	s := New(filepath.Join(t.TempDir(), "notify.json"))
	saved, err := s.Store.Save(Spec{Name: "w", Type: TypeWebhook, Enabled: true, URL: "https://hooks.example.org/abc", HeaderName: "X-Secret", HeaderValue: "SHARED-SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Test(context.Background(), Spec{ID: saved.ID, Name: "w", Type: TypeWebhook, URL: evil.URL, HeaderName: "X-Secret"})
	if got == "SHARED-SECRET" {
		t.Fatal("the stored header went to the new URL")
	}
	// Editing without a new URL keeps both secrets.
	if _, err := s.Store.Save(Spec{ID: saved.ID, Name: "w2", Type: TypeWebhook, Enabled: true, HeaderName: "X-Secret"}); err != nil {
		t.Fatal(err)
	}
	if sp, _ := s.Store.Get(saved.ID); sp.URL != "https://hooks.example.org/abc" || sp.HeaderValue != "SHARED-SECRET" {
		t.Fatalf("kept secrets lost: %+v", sp)
	}
}

func TestStoredMailPasswordDoesNotFollowANewHost(t *testing.T) {
	old := &Spec{Type: TypeEmail, Host: "smtp.example.org", Port: 587, Security: "starttls", Username: "u", Password: "MAILPASS"}
	for _, n := range []Spec{
		{Type: TypeEmail, Host: "evil.example.net", Port: 587, Security: "starttls", Username: "u"},
		{Type: TypeEmail, Host: "smtp.example.org", Port: 25, Security: "none", Username: "u"},
	} {
		n.keepSecrets(old)
		if n.Password != "" {
			t.Fatalf("password kept for %s:%d %s", n.Host, n.Port, n.Security)
		}
	}
	same := Spec{Type: TypeEmail, Host: "SMTP.example.org ", Security: "starttls", Username: "u"}
	same.keepSecrets(old)
	if same.Password != "MAILPASS" {
		t.Fatal("same server lost its password")
	}
}

// Redaction: Go re-encodes the URL in its error; the secret part must not
// come back in any form.

func TestRedactHidesAReencodedWebhookURL(t *testing.T) {
	spec := &Spec{Type: TypeWebhook, URL: "http://127.0.0.1:1/hook/tökén-SECRET"}
	req, _ := http.NewRequest("POST", spec.URL, nil)
	_, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err == nil {
		t.Skip("something listens on port 1")
	}
	msg := redact(err, spec)
	if strings.Contains(msg, "SECRET") || strings.Contains(msg, "hook") {
		t.Fatalf("redacted: %s", msg)
	}
	if !strings.Contains(msg, "127.0.0.1:1") {
		t.Fatalf("the host should stay: %s", msg)
	}
}
