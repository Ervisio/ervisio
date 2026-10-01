package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Downloader fetches release files over HTTPS from GitHub only.
type Downloader struct {
	Client *http.Client
	// AllowURL decides whether a URL (first request and every redirect) may
	// be fetched. Default: https on github.com and *.githubusercontent.com.
	AllowURL func(*url.URL) bool
}

// DefaultAllowURL accepts GitHub release download hosts over HTTPS.
func DefaultAllowURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	return h == "github.com" || strings.HasSuffix(h, ".githubusercontent.com")
}

// NewDownloader returns a downloader with sane timeouts.
func NewDownloader() *Downloader {
	d := &Downloader{AllowURL: DefaultAllowURL}
	d.Client = &http.Client{
		// No overall timeout: big files on slow links. The context bounds
		// it, and the transport bounds every stall.
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       30 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if !d.allowed(req.URL) {
				return fmt.Errorf("redirect to %s refused", req.URL.Host)
			}
			return nil
		},
	}
	return d
}

func (d *Downloader) allowed(u *url.URL) bool {
	if d.AllowURL != nil {
		return d.AllowURL(u)
	}
	return DefaultAllowURL(u)
}

// stallTimeout aborts a download that receives nothing for this long.
const stallTimeout = 60 * time.Second

// Fetch downloads rawURL to dst (created 0600, must not exist), refusing
// more than max bytes. progress (may be nil) gets (done, total) at most a
// few times per second; total is -1 when unknown.
func (d *Downloader) Fetch(ctx context.Context, rawURL, dst string, max int64, progress func(done, total int64)) error {
	u, err := url.Parse(rawURL)
	if err != nil || !d.allowed(u) {
		return fmt.Errorf("download URL %q refused (only HTTPS from GitHub)", rawURL)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgentBase+"download")
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %v", cleanNetErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: server answered %s", resp.Status)
	}
	total := resp.ContentLength
	if total > max {
		return fmt.Errorf("download is %d bytes, more than the %d allowed", total, max)
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(dst)
		}
	}()
	stall := time.AfterFunc(stallTimeout, cancel)
	defer stall.Stop()
	buf := make([]byte, 64<<10)
	var done int64
	last := time.Time{}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(stallTimeout)
			done += int64(n)
			if done > max {
				return fmt.Errorf("download is larger than the %d bytes allowed", max)
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			if progress != nil && time.Since(last) > 200*time.Millisecond {
				last = time.Now()
				progress(done, total)
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil && ctx.Err() != context.Canceled {
				return ctx.Err()
			}
			return fmt.Errorf("download interrupted: %v", rerr)
		}
	}
	if total >= 0 && done != total {
		return fmt.Errorf("download incomplete (%d of %d bytes)", done, total)
	}
	if progress != nil {
		progress(done, total)
	}
	if err := f.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}
