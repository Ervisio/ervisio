package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func newService(t *testing.T) *Service {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "notify.json"))
}

func TestWebhookTemplateEscapesValues(t *testing.T) {
	out, _ := renderWebhook(`{"t":"{title}","b":"{body}","x":{"y":"{level}"}}`, map[string]string{
		"title": `He said "hi"` + "\n", "body": `a\b</script>`, "level": "warn",
	})
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if v["t"] != "He said \"hi\"\n" || v["b"] != `a\b</script>` {
		t.Fatalf("values changed: %v", v)
	}
}

func TestDefaultTemplateIsValidJSON(t *testing.T) {
	out, _ := renderWebhook(DefaultTemplate, sampleVars())
	if !json.Valid([]byte(out)) {
		t.Fatal(out)
	}
	s := Spec{Name: "w", Type: TypeWebhook, URL: "https://example.org/hook", Enabled: true}
	s.Normalise()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.Template = `{"text": "{title}"`
	if err := s.Validate(); err == nil {
		t.Fatal("a template that is not JSON must be refused")
	}
}

func TestSecretsNeverSerialized(t *testing.T) {
	svc := newService(t)
	secrets := []string{"s3cr3t-PASSWORD", "123456789:AAEhBP0av28-secret-bot-token-xyz", "https://hooks.example.org/services/T000/B000/XXXXSECRET", "hdr-secret-value", "ntfy-token-secret", "gotify-token-secret"}
	for _, in := range []Spec{
		{Name: "mail", Type: TypeEmail, Enabled: true, Host: "smtp.example.org", Username: "u", Password: secrets[0], From: "a@example.org", To: []string{"b@example.org"}},
		{Name: "tg", Type: TypeTelegram, Enabled: true, BotToken: secrets[1], ChatID: "-100123"},
		{Name: "wh", Type: TypeWebhook, Enabled: true, URL: secrets[2], HeaderName: "X-Token", HeaderValue: secrets[3]},
		{Name: "ntfy", Type: TypeNtfy, Enabled: true, Topic: "alerts", Token: secrets[4]},
		{Name: "gotify", Type: TypeGotify, Enabled: true, Server: "https://gotify.example.org", Token: secrets[5]},
	} {
		if _, err := svc.Save(in); err != nil {
			t.Fatalf("%s: %v", in.Name, err)
		}
	}
	list, err := svc.List()
	if err != nil || len(list) != 5 {
		t.Fatalf("%v %d", err, len(list))
	}
	b, _ := json.Marshal(list)
	for _, s := range secrets {
		if strings.Contains(string(b), s) {
			t.Errorf("the channel list leaks %q", s)
		}
	}
	if !list[0].Secrets["password"] || !list[2].Secrets["url"] || list[2].URLHint != "https://hooks.example.org" {
		t.Errorf("secret flags: %+v %+v", list[0].Secrets, list[2])
	}
	// The file holds them, readable by root only.
	fi, err := os.Stat(svc.Store.Path)
	if err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) { // no POSIX modes on Windows
		t.Fatalf("channels file: %v %v", err, fi.Mode())
	}
	raw, _ := os.ReadFile(svc.Store.Path)
	if !strings.Contains(string(raw), secrets[0]) {
		t.Error("the file should hold the password")
	}
	// Saving again without secrets keeps them.
	id := list[0].ID
	edit := Spec{ID: id, Name: "mail 2", Type: TypeEmail, Enabled: true, Host: "smtp.example.org", Username: "u", From: "a@example.org", To: []string{"b@example.org"}}
	if _, err := svc.Save(edit); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Store.Get(id)
	if got.Password != secrets[0] || got.Name != "mail 2" {
		t.Errorf("secret not kept: %+v", got)
	}
	// Changing the type drops the old secrets.
	if _, err := svc.Save(Spec{ID: id, Name: "now ntfy", Type: TypeNtfy, Enabled: true, Topic: "x"}); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Store.Get(id)
	if got.Password != "" {
		t.Error("a password must not survive a type change")
	}
}

func TestErrorsRedactSecrets(t *testing.T) {
	old := telegramBase
	defer func() { telegramBase = old }()
	telegramBase = "http://127.0.0.1:1" // refused
	s := Spec{Name: "tg", Type: TypeTelegram, BotToken: "123456789:AAEhBP0av28-secret-bot-token-xyz", ChatID: "1"}
	err := send(context.Background(), &s, Message{Title: "x"}.Clean())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "secret-bot-token") {
		t.Fatalf("the error leaks the token: %v", err)
	}
	// A webhook URL is a secret too.
	w := Spec{Name: "w", Type: TypeWebhook, Method: "POST", Template: DefaultTemplate, URL: "http://127.0.0.1:1/services/T0/B0/SECRETPART"}
	err = send(context.Background(), &w, Message{Title: "x"}.Clean())
	if err == nil || strings.Contains(err.Error(), "SECRETPART") {
		t.Fatalf("the error leaks the URL: %v", err)
	}
}

func TestSendFiltersByLevelAndEvent(t *testing.T) {
	var mu sync.Mutex
	got := map[string][]map[string]any{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		_ = json.NewDecoder(r.Body).Decode(&v)
		v["_hdr"] = r.Header.Get("X-Secret")
		mu.Lock()
		got[r.URL.Path] = append(got[r.URL.Path], v)
		mu.Unlock()
	}))
	defer ts.Close()
	svc := newService(t)
	mk := func(name, path, min string, events []string, enabled bool) {
		if _, err := svc.Save(Spec{Name: name, Type: TypeWebhook, Enabled: enabled, URL: ts.URL + path, MinLevel: min, Events: events, HeaderName: "X-Secret", HeaderValue: "hv-" + name}); err != nil {
			t.Fatal(err)
		}
	}
	mk("all", "/all", "", nil, true)
	mk("errors", "/errors", "error", nil, true)
	mk("jobsonly", "/jobs", "", []string{EventJobs}, true)
	mk("off", "/off", "", nil, false)

	d := svc.Send(context.Background(), Message{Title: "Hello", Body: "b", Level: "warn"}, EventPlugins)
	if d.Channels != 1 || d.Delivered != 1 || len(got["/all"]) != 1 {
		t.Fatalf("warn/plugins: %+v %v", d, got)
	}
	if got["/all"][0]["title"] != "Hello" || got["/all"][0]["_hdr"] != "hv-all" || got["/all"][0]["source"] != "Ervisio" {
		t.Errorf("payload: %v", got["/all"][0])
	}
	d = svc.Send(context.Background(), Message{Title: "Boom", Level: "error"}, EventJobs)
	if d.Channels != 3 || d.Delivered != 3 {
		t.Fatalf("error/jobs: %+v", d)
	}
	if len(got["/off"]) != 0 {
		t.Error("a disabled channel must not be used")
	}
	// A failing channel counts as failed and the sender learns nothing else.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 500) }))
	defer bad.Close()
	if _, err := svc.Save(Spec{Name: "bad", Type: TypeWebhook, Enabled: true, URL: bad.URL, Events: []string{EventUpdates}}); err != nil {
		t.Fatal(err)
	}
	d = svc.Send(context.Background(), Message{Title: "x"}, EventUpdates)
	if d.Failed != 1 || d.Delivered != 1 {
		t.Fatalf("updates: %+v", d)
	}
	list, _ := svc.List()
	var badLast *Result
	for _, c := range list {
		if c.Name == "bad" {
			badLast = c.Last
		}
	}
	if badLast == nil || badLast.OK || !strings.Contains(badLast.Error, "500") {
		t.Errorf("last result: %+v", badLast)
	}
}

func TestPluginRateLimit(t *testing.T) {
	svc := newService(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	for i := 0; i < pluginPerMinute; i++ {
		if ok, _ := svc.AllowPlugin("docker", "user:alice"); !ok {
			t.Fatalf("call %d refused", i)
		}
	}
	ok, wait := svc.AllowPlugin("docker", "user:alice")
	if ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("11th call: %v %v", ok, wait)
	}
	if ok, _ := svc.AllowPlugin("other", "user:alice"); !ok {
		t.Fatal("the limit is per plugin")
	}
	now = now.Add(61 * time.Second)
	if ok, _ := svc.AllowPlugin("docker", "user:alice"); !ok {
		t.Fatal("the minute window must slide")
	}
	// The hourly cap.
	for i := 0; i < 80; i++ {
		now = now.Add(61 * time.Second)
		svc.AllowPlugin("hourly", "user:alice")
	}
	ok, wait = svc.AllowPlugin("hourly", "user:alice")
	if !ok && wait > time.Hour {
		t.Fatalf("wait %v", wait)
	}
}

func TestMessageClean(t *testing.T) {
	m := Message{Title: "a\r\nBcc: x@y", Body: "l1\r\nl2\x00", Level: "loud", Link: " /p/x "}.Clean()
	if strings.ContainsAny(m.Title, "\r\n") || m.Level != "info" || strings.ContainsAny(m.Body, "\r\x00") || !strings.Contains(m.Body, "\n") {
		t.Fatalf("%+v", m)
	}
	if long := (Message{Title: strings.Repeat("x", 500)}).Clean(); len([]rune(long.Title)) > maxTitle {
		t.Fatal("title not cut")
	}
	for l, ok := range map[string]bool{"": true, "/p/docker": true, "//evil.example": false, "https://a.example/x": true, "javascript:alert(1)": false, "https://u:p@a.example": false, "https://a b": false} {
		if ValidLink(l) != ok {
			t.Errorf("ValidLink(%q) = %v", l, !ok)
		}
	}
}

func TestEmailHeadersCannotBeInjected(t *testing.T) {
	s := Spec{Name: "m", Type: TypeEmail, Host: "smtp.example.org", From: "Ervisio <a@example.org>", To: []string{"b@example.org"}}
	msg := string(buildEmail(&s, Message{Title: "Hi\r\nBcc: evil@example.org", Body: "body"}.Clean(), time.Unix(0, 0)))
	head, _, _ := strings.Cut(msg, "\r\n\r\n")
	for _, l := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(l, "Bcc:") {
			t.Fatalf("header injection: %q", head)
		}
	}
}

// fakeSMTP accepts one message in plain mode and returns what it received.
func fakeSMTP(t *testing.T, wantAuth bool) (addr string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got = make(chan string, 1)
	go func() {
		defer ln.Close()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { io.WriteString(c, s+"\r\n") }
		w("220 fake ESMTP")
		var data strings.Builder
		var auth string
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					inData = false
					w("250 queued")
					continue
				}
				data.WriteString(line + "\n")
				continue
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				io.WriteString(c, "250-fake\r\n250 AUTH PLAIN\r\n")
			case strings.HasPrefix(line, "AUTH"):
				auth = line
				w("235 ok")
			case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				w("250 ok")
			case line == "DATA":
				inData = true
				w("354 go")
			case line == "QUIT":
				w("221 bye")
				got <- auth + "\n" + data.String()
				return
			default:
				w("250 ok")
			}
		}
	}()
	return ln.Addr().String(), got
}

func TestEmailThroughSMTP(t *testing.T) {
	addr, got := fakeSMTP(t, true)
	host, port, _ := net.SplitHostPort(addr)
	var p int
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	svc := newService(t)
	in := Spec{Name: "mail", Type: TypeEmail, Enabled: true, Host: host, Port: p, Security: "none", Username: "user", Password: "pass1234", From: "a@example.org", To: []string{"b@example.org"}}
	if err := svc.Test(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if !strings.HasPrefix(m, "AUTH PLAIN ") || !strings.Contains(m, "Subject: Ervisio test notification") || !strings.Contains(m, "works.") {
			t.Fatalf("message: %q", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing received")
	}
}

func TestTelegramNtfyGotify(t *testing.T) {
	type req struct{ path, auth, key, title string }
	var mu sync.Mutex
	var seen []req
	var bodies []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, req{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Gotify-Key"), r.Header.Get("Title")})
		bodies = append(bodies, string(b))
		mu.Unlock()
		io.WriteString(w, `{"ok":true}`)
	}))
	defer ts.Close()
	old := telegramBase
	defer func() { telegramBase = old }()
	telegramBase = ts.URL
	ctx := context.Background()
	m := Message{Title: "T", Body: "B", Level: "error", Link: "https://x.example/y"}.Clean()
	tg := Spec{Type: TypeTelegram, BotToken: "123456789:AAEhBP0av28-secret-bot-token-xyz", ChatID: "-1001"}
	if err := send(ctx, &tg, m); err != nil {
		t.Fatal(err)
	}
	nt := Spec{Type: TypeNtfy, Server: ts.URL, Topic: "alerts", Token: "tk_abc"}
	if err := send(ctx, &nt, m); err != nil {
		t.Fatal(err)
	}
	gf := Spec{Type: TypeGotify, Server: ts.URL, Token: "gtoken"}
	if err := send(ctx, &gf, m); err != nil {
		t.Fatal(err)
	}
	if seen[0].path != "/bot123456789:AAEhBP0av28-secret-bot-token-xyz/sendMessage" || !strings.Contains(bodies[0], `"chat_id":"-1001"`) {
		t.Errorf("telegram: %+v %s", seen[0], bodies[0])
	}
	if seen[1].path != "/alerts" || seen[1].auth != "Bearer tk_abc" || seen[1].title != "T" || !strings.Contains(bodies[1], "https://x.example/y") {
		t.Errorf("ntfy: %+v %s", seen[1], bodies[1])
	}
	if seen[2].path != "/message" || seen[2].key != "gtoken" || !strings.Contains(bodies[2], `"priority":8`) {
		t.Errorf("gotify: %+v %s", seen[2], bodies[2])
	}
}

func TestValidateRefusesBadInput(t *testing.T) {
	bad := []Spec{
		{Name: "", Type: TypeWebhook, URL: "https://a.example"},
		{Name: "x", Type: "sms"},
		{Name: "x", Type: TypeWebhook, URL: "ftp://a.example"},
		{Name: "x", Type: TypeWebhook, URL: "https://user:pw@a.example"},
		{Name: "x", Type: TypeWebhook, URL: "https://a.example", HeaderName: "Host", HeaderValue: "x"},
		{Name: "x", Type: TypeEmail, Host: "smtp.example.org", From: "not an address", To: []string{"b@example.org"}},
		{Name: "x", Type: TypeEmail, Host: "smtp.example.org", From: "a@example.org", To: nil},
		{Name: "x", Type: TypeEmail, Host: "smtp.example.org", From: "a@example.org", To: []string{"b@example.org"}, Username: "u"},
		{Name: "x", Type: TypeTelegram, BotToken: "nope", ChatID: "1"},
		{Name: "x", Type: TypeTelegram, BotToken: "123456789:AAEhBP0av28-secret-bot-token-xyz", ChatID: "abc"},
		{Name: "x", Type: TypeNtfy, Topic: "a b"},
		{Name: "x", Type: TypeGotify, Server: "https://g.example"},
		{Name: "x", Type: TypeWebhook, URL: "https://a.example", MinLevel: "loud"},
		{Name: "x", Type: TypeWebhook, URL: "https://a.example", Events: []string{"everything"}},
	}
	for i, s := range bad {
		s.Normalise()
		if s.Name == "x" && s.Type == TypeWebhook && s.MinLevel == "" {
			s.MinLevel = "info"
		}
		if err := s.Validate(); err == nil {
			t.Errorf("case %d (%s) must be refused", i, s.Type)
		}
	}
}
