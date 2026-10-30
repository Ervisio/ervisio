package remote

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// davFS is a small WebDAV client (RFC 4918): PROPFIND, MKCOL, MOVE,
// DELETE, GET and PUT. Uploads are streamed; Basic credentials are sent
// with every request, Digest ones once the server named its realm.
type davFS struct {
	base   *url.URL // ends without "/"
	client *http.Client
	user   string
	pass   string

	mu     sync.Mutex
	digest map[string]string // the last Digest challenge
	nc     uint32
}

func dialWebDAV(ctx context.Context, spec *Conn) (*davFS, error) {
	u, err := url.Parse(spec.URL)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "The WebDAV address is not valid.")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: spec.Insecure, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: 2 * time.Minute,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
	}
	f := &davFS{
		base: u, user: spec.User, pass: spec.secret(secPassword),
		client: &http.Client{
			Transport: tr,
			// Credentials are never replayed to another place.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	// Check the address and the credentials (and learn a Digest realm).
	if _, err := f.Stat(ctx, "/"); err != nil {
		f.Close()
		var cert *tls.CertificateVerificationError
		if errors.As(err, &cert) {
			return nil, rpc.Errorf(rpc.Unavailable, "The TLS certificate of %s cannot be verified (%v). If you trust this server, allow unverified certificates in the connection.", u.Hostname(), cert.Err)
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, rpc.Errorf(rpc.Unavailable, "Could not connect to %s: %s", u.Host, netErrText(ue.Err))
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, rpc.Errorf(rpc.NotFound, "Nothing was found at %s. Check the address.", spec.URL)
		}
		return nil, err
	}
	return f, nil
}

func (f *davFS) Close() error {
	f.client.CloseIdleConnections()
	return nil
}

// href is the escaped URL path of p.
func (f *davFS) href(p string, dir bool) string {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	var b strings.Builder
	b.WriteString(f.base.EscapedPath())
	for _, s := range segs {
		if s == "" {
			continue
		}
		b.WriteByte('/')
		b.WriteString(url.PathEscape(s))
	}
	if dir || p == "/" {
		b.WriteByte('/')
	}
	return b.String()
}

func (f *davFS) urlOf(p string, dir bool) string {
	u := *f.base
	u.Path, u.RawPath = "", ""
	return u.String() + f.href(p, dir)
}

type davStatusError struct {
	Method string
	Path   string
	Code   int
}

func (e *davStatusError) Error() string {
	switch e.Code {
	case 401:
		return "The server refused the sign-in. Check the user name and the password."
	case 403:
		return fmt.Sprintf("%s: the server does not allow this (403).", e.Path)
	case 404:
		return fmt.Sprintf("%s: not found.", e.Path)
	case 405, 412:
		return fmt.Sprintf("%s already exists.", e.Path)
	case 507:
		return "The server has no room left (507)."
	}
	return fmt.Sprintf("%s %s: the server answered %d %s.", e.Method, e.Path, e.Code, http.StatusText(e.Code))
}

func (e *davStatusError) Unwrap() error {
	switch e.Code {
	case 404, 409:
		return os.ErrNotExist
	case 403:
		return os.ErrPermission
	case 405, 412:
		return os.ErrExist
	}
	return nil
}

func statusErr(method, p string, code int) error {
	if code == 401 {
		return rpc.Errorf(rpc.Forbidden, "%s", (&davStatusError{Code: 401}).Error())
	}
	return &davStatusError{Method: method, Path: p, Code: code}
}

// do sends one request. A body is only replayed when it is nil or a
// *bytes.Reader (Digest needs a second round trip the first time).
func (f *davFS) do(ctx context.Context, method, target string, body io.Reader, size int64, hdr map[string]string) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, target, body)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.ContentLength = size
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		f.authorize(req)
		res, err := f.client.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusUnauthorized || attempt > 0 {
			return res, nil
		}
		ch := parseDigest(res.Header.Values("Www-Authenticate"))
		replay := body == nil
		if br, ok := body.(*bytes.Reader); ok {
			_, _ = br.Seek(0, io.SeekStart)
			replay = true
		}
		if ch == nil || !replay {
			return res, nil
		}
		drain(res)
		f.mu.Lock()
		f.digest, f.nc = ch, 0
		f.mu.Unlock()
	}
}

func drain(res *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	res.Body.Close()
}

func (f *davFS) authorize(req *http.Request) {
	if f.user == "" && f.pass == "" {
		return
	}
	f.mu.Lock()
	ch := f.digest
	f.nc++
	nc := f.nc
	f.mu.Unlock()
	if ch == nil {
		req.SetBasicAuth(f.user, f.pass)
		return
	}
	req.Header.Set("Authorization", digestHeader(ch, f.user, f.pass, req.Method, req.URL.RequestURI(), nc))
}

// parseDigest returns the parameters of a Digest challenge, or nil.
func parseDigest(vals []string) map[string]string {
	for _, v := range vals {
		if len(v) < 7 || !strings.EqualFold(v[:7], "digest ") {
			continue
		}
		m := map[string]string{}
		s := v[7:]
		for len(s) > 0 {
			s = strings.TrimLeft(s, " ,")
			eq := strings.IndexByte(s, '=')
			if eq <= 0 {
				break
			}
			k := strings.ToLower(strings.TrimSpace(s[:eq]))
			s = s[eq+1:]
			var val string
			if strings.HasPrefix(s, `"`) {
				end := strings.IndexByte(s[1:], '"')
				if end < 0 {
					break
				}
				val, s = s[1:end+1], s[end+2:]
			} else {
				end := strings.IndexByte(s, ',')
				if end < 0 {
					end = len(s)
				}
				val, s = strings.TrimSpace(s[:end]), s[end:]
			}
			m[k] = val
		}
		alg := strings.ToUpper(m["algorithm"])
		if m["nonce"] != "" && (alg == "" || alg == "MD5" || alg == "SHA-256") {
			return m
		}
	}
	return nil
}

func digestHeader(ch map[string]string, user, pass, method, uri string, nc uint32) string {
	alg := strings.ToUpper(ch["algorithm"])
	var h func() hash.Hash = md5.New
	if alg == "SHA-256" {
		h = sha256.New
	}
	sum := func(s string) string {
		x := h()
		x.Write([]byte(s))
		return hex.EncodeToString(x.Sum(nil))
	}
	ha1 := sum(user + ":" + ch["realm"] + ":" + pass)
	ha2 := sum(method + ":" + uri)
	qop := ""
	for _, q := range strings.Split(ch["qop"], ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
		}
	}
	var resp, extra string
	if qop != "" {
		cb := make([]byte, 8)
		_, _ = rand.Read(cb)
		cnonce := hex.EncodeToString(cb)
		ncs := fmt.Sprintf("%08x", nc)
		resp = sum(ha1 + ":" + ch["nonce"] + ":" + ncs + ":" + cnonce + ":auth:" + ha2)
		extra = fmt.Sprintf(`, qop=auth, nc=%s, cnonce="%s"`, ncs, cnonce)
	} else {
		resp = sum(ha1 + ":" + ch["nonce"] + ":" + ha2)
	}
	q := func(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) }
	out := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"%s`, q(user), q(ch["realm"]), q(ch["nonce"]), q(uri), resp, extra)
	if ch["opaque"] != "" {
		out += fmt.Sprintf(`, opaque="%s"`, q(ch["opaque"]))
	}
	if alg != "" {
		out += ", algorithm=" + alg
	}
	return out
}

/* ---------- PROPFIND ---------- */

type davMultistatus struct {
	Responses []davResponse `xml:"DAV: response"`
}

type davResponse struct {
	Href     string        `xml:"DAV: href"`
	Propstat []davPropstat `xml:"DAV: propstat"`
}

type davPropstat struct {
	Status string  `xml:"DAV: status"`
	Prop   davProp `xml:"DAV: prop"`
}

type davProp struct {
	Length   string `xml:"DAV: getcontentlength"`
	Modified string `xml:"DAV: getlastmodified"`
	Type     struct {
		Collection *struct{} `xml:"DAV: collection"`
	} `xml:"DAV: resourcetype"`
	ContentType string `xml:"DAV: getcontenttype"`
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/><d:getcontenttype/></d:prop></d:propfind>`

func (f *davFS) propfind(ctx context.Context, p string, depth string, dir bool) ([]davResponse, error) {
	res, err := f.do(ctx, "PROPFIND", f.urlOf(p, dir), bytes.NewReader([]byte(propfindBody)), int64(len(propfindBody)),
		map[string]string{"Depth": depth, "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus {
		if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusMethodNotAllowed {
			return nil, rpc.Errorf(rpc.Invalid, "This address does not answer WebDAV requests. Check the URL (for Nextcloud: https://server/remote.php/dav/files/USER).")
		}
		if res.StatusCode >= 300 && res.StatusCode < 400 {
			if !dir {
				// A folder named without its trailing slash.
				return f.propfind(ctx, p, depth, true)
			}
			return nil, rpc.Errorf(rpc.Invalid, "The server redirects to %s. Use that address in the connection.", res.Header.Get("Location"))
		}
		return nil, statusErr("PROPFIND", p, res.StatusCode)
	}
	var ms davMultistatus
	if err := xml.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(&ms); err != nil {
		return nil, rpc.Errorf(rpc.Internal, "The WebDAV answer could not be read: %v", err)
	}
	return ms.Responses, nil
}

func (r *davResponse) prop() (davProp, bool) {
	for _, ps := range r.Propstat {
		if strings.Contains(ps.Status, " 200") {
			return ps.Prop, true
		}
	}
	return davProp{}, false
}

// hrefPath turns a response href into an unescaped URL path without the
// trailing slash.
func hrefPath(h string) string {
	if u, err := url.Parse(strings.TrimSpace(h)); err == nil {
		return strings.TrimRight(u.Path, "/")
	}
	return strings.TrimRight(h, "/")
}

func davEntry(dir, name string, pr davProp) Entry {
	e := Entry{Name: name, Path: path.Join(dir, name)}
	if t, err := http.ParseTime(pr.Modified); err == nil {
		e.MTime = t.UnixMilli()
	}
	if pr.Type.Collection != nil {
		e.Type, e.Mode, e.Perm = "dir", "0755", "rwxr-xr-x"
		return e
	}
	e.Type, e.Mode, e.Perm = "file", "0644", "rw-r--r--"
	fmt.Sscan(pr.Length, &e.Size)
	e.Mime = mimeOf(name)
	if e.Mime == "" && pr.ContentType != "" {
		e.Mime = strings.TrimSpace(strings.Split(pr.ContentType, ";")[0])
	}
	return e
}

func (f *davFS) List(ctx context.Context, dir string) ([]Entry, error) {
	rs, err := f.propfind(ctx, dir, "1", true)
	if err != nil {
		return nil, err
	}
	self := strings.TrimRight(f.base.Path+"/"+strings.Trim(dir, "/"), "/")
	out := make([]Entry, 0, len(rs))
	for i := range rs {
		hp := hrefPath(rs[i].Href)
		if hp == self || hp == "" {
			continue
		}
		pr, ok := rs[i].prop()
		if !ok {
			continue
		}
		name := path.Base(hp)
		if name == "." || name == ".." || name == "/" {
			continue
		}
		out = append(out, davEntry(dir, name, pr))
	}
	return out, nil
}

func (f *davFS) Stat(ctx context.Context, p string) (Entry, error) {
	rs, err := f.propfind(ctx, p, "0", false)
	if err != nil {
		return Entry{}, err
	}
	for i := range rs {
		if pr, ok := rs[i].prop(); ok {
			e := davEntry(path.Dir(p), path.Base(p), pr)
			if p == "/" {
				e.Name, e.Path, e.Type = "/", "/", "dir"
			}
			return e, nil
		}
	}
	return Entry{}, &os.PathError{Op: "stat", Path: p, Err: os.ErrNotExist}
}

func (f *davFS) simple(ctx context.Context, method, p string, dir bool, hdr map[string]string, ok ...int) error {
	res, err := f.do(ctx, method, f.urlOf(p, dir), nil, 0, hdr)
	if err != nil {
		return err
	}
	drain(res)
	for _, c := range ok {
		if res.StatusCode == c {
			return nil
		}
	}
	return statusErr(method, p, res.StatusCode)
}

func (f *davFS) Mkdir(ctx context.Context, p string) error {
	return f.simple(ctx, "MKCOL", p, true, nil, 201, 200, 204)
}

func (f *davFS) Rename(ctx context.Context, from, to string) error {
	e, err := f.Stat(ctx, from)
	if err != nil {
		return err
	}
	dir := e.Type == "dir"
	return f.simple(ctx, "MOVE", from, dir, map[string]string{"Destination": f.urlOf(to, dir), "Overwrite": "F"}, 201, 204)
}

func (f *davFS) Remove(ctx context.Context, p string, dir bool) error {
	return f.simple(ctx, "DELETE", p, dir, nil, 200, 204)
}

// RemoveAll: DELETE removes a collection with everything inside.
func (f *davFS) RemoveAll(ctx context.Context, p string) error {
	e, err := f.Stat(ctx, p)
	if err != nil {
		return err
	}
	return f.Remove(ctx, p, e.Type == "dir")
}

func (f *davFS) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	// The body outlives this call: it is read by the caller's stream.
	res, err := f.do(context.WithoutCancel(ctx), http.MethodGet, f.urlOf(p, false), nil, 0, nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		drain(res)
		return nil, statusErr("GET", p, res.StatusCode)
	}
	return res.Body, nil
}

func (f *davFS) Create(ctx context.Context, p string, size int64) (io.WriteCloser, error) {
	target := f.urlOf(p, false)
	return newPipeWriter(func(r io.Reader) error {
		body := io.Reader(r)
		if size == 0 {
			body = http.NoBody
		}
		res, err := f.do(context.WithoutCancel(ctx), http.MethodPut, target, body, size, nil)
		if err != nil {
			return err
		}
		drain(res)
		switch res.StatusCode {
		case 200, 201, 204:
			return nil
		}
		return statusErr("PUT", p, res.StatusCode)
	}), nil
}
