package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Message is one notification.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	// Level is info (default), success, warn or error.
	Level string `json:"level,omitempty"`
	// Link is an http(s) address or an app path ("/p/docker/stacks").
	Link string `json:"link,omitempty"`
	// Source says where it comes from: "Ervisio" or a plugin name.
	Source string `json:"source,omitempty"`
}

const (
	maxTitle   = 200
	maxBody    = 4000
	maxLink    = 500
	sendTimout = 15 * time.Second
)

// Clean trims the message to what channels accept: no control characters
// (newlines stay in the body only), bounded lengths, a known level.
func (m Message) Clean() Message {
	m.Title = cut(oneLine(m.Title), maxTitle)
	m.Body = cut(stripControl(m.Body, true), maxBody)
	m.Link = cut(oneLine(m.Link), maxLink)
	m.Source = cut(oneLine(m.Source), 80)
	if !ValidLevel(m.Level) {
		m.Level = "info"
	}
	return m
}

// ValidLink reports whether a link is an http(s) address or an app path.
func ValidLink(l string) bool {
	if l == "" {
		return true
	}
	if len(l) > maxLink || strings.ContainsAny(l, " \r\n\x00\t") {
		return false
	}
	if strings.HasPrefix(l, "/") && !strings.HasPrefix(l, "//") {
		return true
	}
	u, err := url.Parse(l)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

func stripControl(s string, keepNL bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && keepNL, r == '\t':
			return r
		case r == '\r':
			return -1
		case r < 0x20 || r == 0x7f:
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "?"))
}

func oneLine(s string) string { return strings.TrimSpace(stripControl(s, false)) }

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// text is the plain-text form used by email, Telegram and ntfy bodies.
func (m Message) text() string {
	var b strings.Builder
	b.WriteString(m.Body)
	if m.Link != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(m.Link)
	}
	return b.String()
}

// redact removes the channel's secrets (and the text a URL error would
// carry) from an error message.
//
// A *url.Error (what net/http returns) carries the request URL, re-encoded
// by Go (non-ASCII and some characters become %XX), so matching the stored
// value is not enough: its URL is replaced by scheme://host before the
// message is built, and the inner error is redacted on its own.
func redact(err error, spec *Spec) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		where := "the server"
		if u, perr := url.Parse(ue.URL); perr == nil && u.Host != "" {
			where = u.Scheme + "://" + u.Host
		}
		inner := "unknown error"
		if ue.Err != nil {
			inner = redactText(ue.Err.Error(), spec)
		}
		return cut(oneLine(ue.Op+" "+where+": "+inner), 300)
	}
	return cut(oneLine(redactText(err.Error(), spec)), 300)
}

// redactText replaces every secret of the channel in msg, as typed and in
// the encoded forms a URL may take.
func redactText(msg string, spec *Spec) string {
	for _, s := range spec.secretValues() {
		forms := []string{s, url.PathEscape(s), url.QueryEscape(s)}
		if u, err := url.Parse(s); err == nil {
			forms = append(forms, u.String(), u.EscapedPath(), u.Path)
		}
		for _, f := range forms {
			if len(f) >= 4 {
				msg = strings.ReplaceAll(msg, f, "***")
			}
		}
	}
	return msg
}

// ---- sending ----

// Hooks for tests.
var (
	// telegramBase is the Bot API address.
	telegramBase = "https://api.telegram.org"
	// tlsConfig makes the TLS configuration for SMTP.
	tlsConfig = func(host string) *tls.Config { return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12} }
)

func httpClient() *http.Client {
	return &http.Client{
		Timeout:       sendTimout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// send delivers msg on one channel.
func send(ctx context.Context, spec *Spec, msg Message) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimout)
	defer cancel()
	var err error
	switch spec.Type {
	case TypeEmail:
		err = sendEmail(ctx, spec, msg)
	case TypeTelegram:
		err = sendTelegram(ctx, spec, msg)
	case TypeWebhook:
		err = sendWebhook(ctx, spec, msg)
	case TypeNtfy:
		err = sendNtfy(ctx, spec, msg)
	case TypeGotify:
		err = sendGotify(ctx, spec, msg)
	default:
		err = fmt.Errorf("unknown channel type %q", spec.Type)
	}
	if err != nil {
		return errors.New(redact(err, spec))
	}
	return nil
}

// do sends the request and checks for a 2xx answer.
func do(req *http.Request) error {
	resp, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the server answered %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func jsonReq(ctx context.Context, method, u string, v any) (*http.Request, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func sendTelegram(ctx context.Context, s *Spec, m Message) error {
	text := m.Title
	if t := m.text(); t != "" {
		text += "\n\n" + t
	}
	req, err := jsonReq(ctx, "POST", telegramBase+"/bot"+s.BotToken+"/sendMessage", map[string]any{
		"chat_id": s.ChatID, "text": cut(text, 4096), "disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	return do(req)
}

var webhookVarRe = regexp.MustCompile(`\{(title|body|level|link|source|time)\}`)

func sampleVars() map[string]string {
	return map[string]string{"title": "Title \"quoted\"", "body": "Line 1\nLine 2", "level": "info", "link": "https://example.org/x", "source": "Ervisio", "time": "2026-01-01T00:00:00Z"}
}

// renderWebhook replaces the placeholders with JSON-escaped text (without
// the quotes), so a value can never break out of a JSON string.
func renderWebhook(tpl string, vars map[string]string) (string, error) {
	return webhookVarRe.ReplaceAllStringFunc(tpl, func(tok string) string {
		b, _ := json.Marshal(vars[tok[1:len(tok)-1]])
		return string(b[1 : len(b)-1])
	}), nil
}

func sendWebhook(ctx context.Context, s *Spec, m Message) error {
	body, _ := renderWebhook(s.Template, map[string]string{
		"title": m.Title, "body": m.Body, "level": m.Level, "link": m.Link, "source": m.Source, "time": time.Now().UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequestWithContext(ctx, s.Method, s.URL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.HeaderName != "" {
		req.Header.Set(s.HeaderName, s.HeaderValue)
	}
	return do(req)
}

func headerText(s string) string {
	for _, r := range s {
		if r > 0x7e {
			return mime.QEncoding.Encode("utf-8", s)
		}
	}
	return s
}

func sendNtfy(ctx context.Context, s *Spec, m Message) error {
	req, err := http.NewRequestWithContext(ctx, "POST", s.Server+"/"+s.Topic, strings.NewReader(m.text()))
	if err != nil {
		return err
	}
	req.Header.Set("Title", headerText(m.Title))
	prio, tag := "3", "information_source"
	switch m.Level {
	case "success":
		tag = "white_check_mark"
	case "warn":
		prio, tag = "4", "warning"
	case "error":
		prio, tag = "5", "rotating_light"
	}
	req.Header.Set("Priority", prio)
	req.Header.Set("Tags", tag)
	if strings.HasPrefix(m.Link, "http") {
		req.Header.Set("Click", m.Link)
	}
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	return do(req)
}

func sendGotify(ctx context.Context, s *Spec, m Message) error {
	prio := 4
	switch m.Level {
	case "warn":
		prio = 6
	case "error":
		prio = 8
	}
	req, err := jsonReq(ctx, "POST", s.Server+"/message", map[string]any{"title": m.Title, "message": m.text(), "priority": prio})
	if err != nil {
		return err
	}
	req.Header.Set("X-Gotify-Key", s.Token)
	return do(req)
}

// ---- email ----

func newMessageID(host string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "<" + hex.EncodeToString(b) + "@" + host + ">"
}

// buildEmail makes the RFC 5322 message: the subject is Q-encoded, the body
// quoted-printable UTF-8; every header value is one line.
func buildEmail(s *Spec, m Message, now time.Time) []byte {
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + oneLine(v) + "\r\n") }
	h("From", s.From)
	h("To", strings.Join(s.To, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", m.Title))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", newMessageID(s.Host))
	h("MIME-Version", "1.0")
	h("Content-Type", `text/plain; charset="utf-8"`)
	h("Content-Transfer-Encoding", "quoted-printable")
	h("X-Mailer", "Ervisio")
	b.WriteString("\r\n")
	w := quotedprintable.NewWriter(&b)
	text := strings.ReplaceAll(m.text(), "\n", "\r\n")
	_, _ = w.Write([]byte(text))
	_ = w.Close()
	b.WriteString("\r\n")
	return b.Bytes()
}

func sendEmail(ctx context.Context, s *Spec, m Message) error {
	addr := hostPort(s.Host, s.Port)
	host := strings.Trim(s.Host, "[]")
	d := net.Dialer{Timeout: sendTimout}
	var conn net.Conn
	var err error
	if s.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: tlsConfig(host)}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if s.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the server does not offer STARTTLS; choose another security setting")
		}
		if err := c.StartTLS(tlsConfig(host)); err != nil {
			return err
		}
	}
	if s.Username != "" {
		// PlainAuth refuses to send the password over an unencrypted
		// connection (except to localhost).
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return err
		}
	}
	from, err := bareAddr(s.From)
	if err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, to := range s.To {
		a, err := bareAddr(to)
		if err != nil {
			return err
		}
		if err := c.Rcpt(a); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildEmail(s, m, time.Now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
