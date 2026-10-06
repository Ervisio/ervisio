package plugins

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Limits of plugins.http / plugins.httpStream.
const (
	defaultHTTPBody = 8 << 20
	// maxHTTPResult bounds a plugins.http response body further: the
	// result travels base64-encoded in one protocol line (rpc.MaxLine).
	maxHTTPResult  = 11 << 20
	maxHTTPPath    = 2048
	maxHTTPQuery   = 8 << 10
	maxHeaderValue = 8 << 10
	httpChunk      = 32 << 10
)

// forbiddenHeaders may never be listed in capabilities.http[].headers:
// the host, credentials, hop-by-hop and framing headers are the daemon's.
var forbiddenHeaders = map[string]bool{
	"Host": true, "Cookie": true, "Cookie2": true, "Authorization": true, "Proxy-Authorization": true,
	"Connection": true, "Upgrade": true, "Transfer-Encoding": true, "Content-Length": true, "Keep-Alive": true,
	"Te": true, "Trailer": true, "Proxy-Connection": true, "Expect": true, "Forwarded": true, "Via": true,
	"Http2-Settings": true, "Origin": true, "Referer": true,
}

// safeHeader reports whether a plugin may be allowed to set the header name.
func safeHeader(name string) bool {
	k := http.CanonicalHeaderKey(name)
	if forbiddenHeaders[k] {
		return false
	}
	for _, p := range []string{"Proxy-", "Sec-", "X-Forwarded-"} {
		if strings.HasPrefix(k, p) {
			return false
		}
	}
	return true
}

// HTTPParams are the params of plugins.http and plugins.httpStream.
type HTTPParams struct {
	Plugin string `json:"plugin"`
	// Name is the capabilities.http entry.
	Name   string `json:"name"`
	Method string `json:"method"`
	// Path is the URL path (percent-encoded as sent), without a query.
	Path string `json:"path"`
	// Query is the raw query string, without "?".
	Query   string            `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Body is UTF-8 text, or base64 when B64 is true.
	Body string `json:"body,omitempty"`
	B64  bool   `json:"b64,omitempty"`
	// JSON sends Content-Type: application/json (an object body in the SDK).
	JSON bool `json:"json,omitempty"`
	// Env is the id of an environment (SDK: the env option). The daemon
	// handles it: it checks access and replaces it with EnvSocket.
	Env string `json:"env,omitempty"`
	// EnvSocket is the tunnel socket the daemon set for Env; a value from
	// the browser never reaches the bridge.
	EnvSocket string `json:"envSocket,omitempty"`
}

// HTTPResult is the result of plugins.http.
type HTTPResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	// Body is UTF-8 text, or base64 when B64 is true (binary content).
	Body string `json:"body"`
	B64  bool   `json:"b64,omitempty"`
}

// httpPlan is an authorised request, ready to send.
type httpPlan struct {
	api *HTTPAPI
	// socket is the API's socket, or the environment's tunnel.
	socket  string
	req     *http.Request
	maxBody int64
	timeout time.Duration
}

// cleanHTTPPath checks a request path and returns it decoded. The path must
// be origin-form ("/…"), with no query, fragment, control characters,
// backslashes, empty, "." or ".." segments, and no encoded "/", "\" or NUL
// (the API would see a different path than the rule matched).
func cleanHTTPPath(raw string) (string, bool) {
	if raw == "" || raw[0] != '/' || len(raw) > maxHTTPPath || strings.HasPrefix(raw, "//") {
		return "", false
	}
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c <= 0x20 || c == 0x7f || c == '?' || c == '#' || c == '\\' {
			return "", false
		}
	}
	low := strings.ToLower(raw)
	for _, bad := range []string{"%2f", "%5c", "%00", "%0a", "%0d"} {
		if strings.Contains(low, bad) {
			return "", false
		}
	}
	dec, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(dec) {
		return "", false
	}
	for _, r := range dec {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return "", false
		}
	}
	if dec != "/" && path.Clean(dec) != dec {
		return "", false // "//", "/./", "/../" or a trailing "/"
	}
	for _, seg := range strings.Split(dec, "/") {
		if seg == "." || seg == ".." {
			return "", false
		}
	}
	return dec, true
}

// validQuery accepts printable ASCII without spaces or "#".
func validQuery(q string) bool {
	if len(q) > maxHTTPQuery {
		return false
	}
	for i := 0; i < len(q); i++ {
		if c := q[i]; c <= 0x20 || c >= 0x7f || c == '#' {
			return false
		}
	}
	return true
}

// match reports whether one of the API's rules allows method on the decoded path.
func (h *HTTPAPI) match(method, decoded string) bool {
	for i := range h.Rules {
		r := &h.Rules[i]
		ok := false
		for _, m := range r.Methods {
			if m == method {
				ok = true
			}
		}
		if !ok {
			continue
		}
		re := r.re
		if re == nil { // manifest built without ParseManifest (tests)
			var err error
			if re, err = compileSpec(r.Path); err != nil {
				continue
			}
		}
		if re.MatchString(decoded) {
			return true
		}
	}
	return false
}

// jsonTextBudget bounds the JSON-escaped size of a text body sent in one
// protocol line (rpc.MaxLine). Past it the body goes out as base64, which
// is never more than 4/3 of its size: a line over rpc.MaxLine is dropped and
// the call would never be answered.
const jsonTextBudget = 12 << 20

// jsonTextLen is an upper bound of the length of b (valid UTF-8) encoded as
// a JSON string: encoding/json writes <, >, & and control characters as
// \u00XX, and U+2028/U+2029 (3 bytes) as \u2028/\u2029.
func jsonTextLen(b []byte) int {
	n := 2
	for _, c := range b {
		switch {
		case c < 0x20 || c == '<' || c == '>' || c == '&':
			n += 6
		case c == '"' || c == '\\' || c >= 0x80:
			n += 2
		default:
			n++
		}
	}
	return n
}

// resolveHTTP finds the plugin and API entry, applies the same level rules
// as commands and checks the request against the declared rules and headers.
func resolveHTTP(ctx context.Context, c *rpc.Call, p HTTPParams) (*httpPlan, error) {
	if !idRe.MatchString(p.Plugin) || !httpNameRe.MatchString(p.Name) {
		return nil, rpc.Errorf(rpc.Invalid, "Give a plugin id and the name of one of its HTTP APIs.")
	}
	f, who, err := authorize(c, p.Plugin)
	if err != nil {
		return nil, err
	}
	m := f.M
	var api *HTTPAPI
	for i := range m.Capabilities.HTTP {
		if m.Capabilities.HTTP[i].Name == p.Name {
			api = &m.Capabilities.HTTP[i]
		}
	}
	if api == nil {
		return nil, rpc.Errorf(rpc.NotFound, "%s does not declare an HTTP API %q.", m.Name, p.Name)
	}
	socket := api.Socket
	if p.EnvSocket != "" {
		// An environment replaces the socket; the API must opt in, and the
		// daemon's tunnel is the user's own, so no administrator rights apply.
		if api.Remote == "" || c.Admin {
			return nil, rpc.Errorf(rpc.Forbidden, "%s does not allow %q to target an environment.", m.Name, p.Name)
		}
		if err := envSocketOK(p.EnvSocket); err != nil {
			return nil, err
		}
		socket = p.EnvSocket
	} else {
		// As for commands: a user-level API is never called from the root bridge.
		if c.Admin && !api.Admin {
			return nil, rpc.Errorf(rpc.Forbidden, "%s declares %q as a user API; it is not called with administrator rights.", m.Name, p.Name)
		}
		root := c.Admin || os.Geteuid() == 0
		if api.Admin && !root && !(api.AdminUnlessGroup != "" && who.Groups[api.AdminUnlessGroup]) {
			return nil, rpc.Errorf(rpc.NeedsAdmin, "%s needs administrator rights to use %q.", m.Name, p.Name)
		}
	}
	if !httpMethods[p.Method] {
		return nil, rpc.Errorf(rpc.Invalid, "%q is not an allowed HTTP method.", p.Method)
	}
	dec, ok := cleanHTTPPath(p.Path)
	if !ok {
		return nil, rpc.Errorf(rpc.Invalid, "%q is not a valid request path.", p.Path)
	}
	if !api.match(p.Method, dec) {
		return nil, rpc.Errorf(rpc.Invalid, "%s does not declare %s %s for %q.", m.Name, p.Method, dec, p.Name)
	}
	if !validQuery(p.Query) {
		return nil, rpc.Errorf(rpc.Invalid, "The query string is too long or holds characters that are not allowed.")
	}
	hdr := http.Header{}
	if len(p.Headers) > maxHTTPHeaders {
		return nil, rpc.Errorf(rpc.Invalid, "Too many request headers.")
	}
	for k, v := range p.Headers {
		ck := http.CanonicalHeaderKey(k)
		allowed := false
		for _, h := range api.Headers {
			if http.CanonicalHeaderKey(h) == ck {
				allowed = true
			}
		}
		if !allowed || !headerNameRe.MatchString(k) || !safeHeader(k) {
			return nil, rpc.Errorf(rpc.Invalid, "%s may not set the header %q on %q.", m.Name, k, p.Name)
		}
		if len(v) > maxHeaderValue || strings.ContainsAny(v, "\r\n\x00") {
			return nil, rpc.Errorf(rpc.Invalid, "The value of the header %q is not allowed.", k)
		}
		hdr.Set(ck, v)
	}
	if p.JSON && hdr.Get("Content-Type") == "" {
		hdr.Set("Content-Type", "application/json")
	}
	maxBody := api.MaxBody
	if maxBody == 0 {
		maxBody = defaultHTTPBody
	}
	body := []byte(p.Body)
	if p.B64 {
		if body, err = base64.StdEncoding.DecodeString(p.Body); err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "body is not valid base64.")
		}
	}
	if int64(len(body)) > maxBody {
		return nil, rpc.Errorf(rpc.Invalid, "The request body is larger than %d bytes.", maxBody)
	}
	u := &url.URL{Scheme: "http", Host: "localhost", Path: dec, RawPath: p.Path, RawQuery: p.Query}
	var rd io.Reader
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, p.Method, u.String(), rd)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "Could not build the request: %v", err)
	}
	req.Header = hdr
	req.Host = "localhost"
	to := defaultTimeout
	if api.TimeoutSec > 0 {
		to = time.Duration(api.TimeoutSec) * time.Second
	}
	return &httpPlan{api: api, socket: socket, req: req, maxBody: maxBody, timeout: to}, nil
}

// unixClient talks HTTP to one unix socket: no proxy, no redirects
// followed, no keep-alive (one connection per call, closed with it).
func unixClient(socket string, headerTimeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			if isPipe(socket) {
				return dialPipe(ctx, socket)
			}
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		DisableKeepAlives:      true,
		DisableCompression:     true,
		MaxResponseHeaderBytes: 64 << 10,
		ResponseHeaderTimeout:  headerTimeout,
	}
	return &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func httpErr(ctx context.Context, err error, pl *httpPlan) error {
	sock := pl.socket
	if pl.socket != pl.api.Socket {
		sock = "the environment"
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return rpc.Errorf(rpc.Unavailable, "%s did not answer within %s.", sock, pl.timeout)
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM):
		return rpc.Errorf(rpc.Forbidden, "You may not connect to %s.", sock)
	case errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED):
		return rpc.Errorf(rpc.Unavailable, "Nothing is listening on %s.", sock)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return rpc.Errorf(rpc.Unavailable, "%s did not answer within %s.", sock, pl.timeout)
	}
	return rpc.Errorf(rpc.Unavailable, "Request to %s failed: %v", sock, err)
}

// flatHeaders returns the response headers as one value per name
// (repeated headers joined with ", "), without Set-Cookie.
func flatHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if k == "Set-Cookie" {
			continue
		}
		out[k] = strings.Join(v, ", ")
	}
	return out
}

func runHTTP(ctx context.Context, c *rpc.Call, p HTTPParams) (*HTTPResult, error) {
	pl, err := resolveHTTP(ctx, c, p)
	if err != nil {
		return nil, err
	}
	tctx, tcancel := context.WithTimeout(ctx, pl.timeout)
	defer tcancel()
	resp, err := unixClient(pl.socket, 0).Do(pl.req.WithContext(tctx))
	if err != nil {
		return nil, httpErr(tctx, err, pl)
	}
	defer resp.Body.Close()
	limit := min(pl.maxBody, maxHTTPResult)
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, httpErr(tctx, err, pl)
	}
	if int64(len(b)) > limit {
		return nil, rpc.Errorf(rpc.Unavailable, "The response is larger than %d bytes; use httpStream for large answers.", limit)
	}
	res := &HTTPResult{Status: resp.StatusCode, Headers: flatHeaders(resp.Header)}
	if utf8.Valid(b) && jsonTextLen(b) <= jsonTextBudget {
		res.Body = string(b)
	} else {
		res.Body, res.B64 = base64.StdEncoding.EncodeToString(b), true
	}
	return res, nil
}

// runHTTPStream sends {"status","headers"} once the response starts, then
// the body as binary chunks as they arrive. There is no total timeout (only
// for the response headers); closing the stream closes the connection.
func runHTTPStream(ctx context.Context, c *rpc.Call, s rpc.Stream, p HTTPParams) error {
	pl, err := resolveHTTP(ctx, c, p)
	if err != nil {
		return err
	}
	// The stream has no input: a cancelled stream closes Input, and ctx
	// with it ends the request.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		in := s.Input()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-in:
				if !ok {
					cancel()
					return
				}
			}
		}
	}()
	resp, err := unixClient(pl.socket, pl.timeout).Do(pl.req.WithContext(ctx))
	if err != nil {
		return httpErr(ctx, err, pl)
	}
	defer resp.Body.Close()
	if err := s.Send(map[string]any{"status": resp.StatusCode, "headers": flatHeaders(resp.Header)}); err != nil {
		return nil
	}
	buf := make([]byte, httpChunk)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if serr := s.SendBytes(buf[:n]); serr != nil {
				return nil
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return rpc.Errorf(rpc.Unavailable, "The response from %s broke off: %v", pl.api.Socket, err)
		}
	}
}
