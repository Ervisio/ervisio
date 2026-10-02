package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// Channels.
const (
	ChannelStable     = "stable"
	ChannelPrerelease = "prerelease"
)

// Asset is one file attached to a GitHub release.
type Asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

// Release is the subset of the GitHub release object we use.
type Release struct {
	Tag         string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// Version returns the release version (tag without "v"), or "" when the
// tag is not a semantic version.
func (r *Release) Version() string {
	v, err := ParseVersion(r.Tag)
	if err != nil {
		return ""
	}
	return v.String()
}

// Asset finds an asset by name.
func (r *Release) Asset(name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// ArchiveName is the release archive for a version and Go architecture.
func ArchiveName(version, goarch string) string {
	return fmt.Sprintf("%s-%s-linux-%s.tar.gz", brand.Slug, version, goarch)
}

// ArchivePrefix is the top-level folder inside the archive.
func ArchivePrefix(version, goarch string) string {
	return fmt.Sprintf("%s-%s-linux-%s", brand.Slug, version, goarch)
}

// Arch is the architecture this binary was built for.
var Arch = runtime.GOARCH

// Limits on what we read from the network.
const (
	maxAPIBody    = 4 << 20
	maxNotes      = 64 << 10
	MaxArchive    = 200 << 20
	maxSumsFile   = 64 << 10
	maxSigFile    = 4 << 10
	apiTimeout    = 15 * time.Second
	cacheTTL      = time.Hour
	userAgentBase = brand.Slug + "-updater/"
)

// Checker asks GitHub for the latest release, caching the answer for an
// hour and revalidating with ETag afterwards.
type Checker struct {
	API    string // default https://api.github.com
	Repo   string // owner/name, default brand.GitHubRepo
	Client *http.Client
	Now    func() time.Time

	mu    sync.Mutex
	cache map[string]*cached // by channel
}

type cached struct {
	at      time.Time
	etag    string
	release *Release // nil: no release published yet
}

// NewChecker returns a checker for the Ervisio repository.
func NewChecker() *Checker {
	return &Checker{API: "https://api.github.com", Repo: brand.GitHubRepo,
		Client: &http.Client{Timeout: apiTimeout}}
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// ErrNoRelease means the repository has no (matching) release yet.
var ErrNoRelease = errors.New("no release has been published yet")

// Latest returns the newest release of the channel: the "latest" release
// for stable (GitHub excludes drafts and pre-releases), the highest
// semantic version among recent non-draft releases for prerelease. force
// skips the 1-hour cache (an ETag revalidation still applies).
func (c *Checker) Latest(ctx context.Context, channel string, force bool) (*Release, time.Time, error) {
	if channel != ChannelPrerelease {
		channel = ChannelStable
	}
	c.mu.Lock()
	if c.cache == nil {
		c.cache = map[string]*cached{}
	}
	ent := c.cache[channel]
	c.mu.Unlock()
	if ent != nil && !force && c.now().Sub(ent.at) < cacheTTL {
		if ent.release == nil {
			return nil, ent.at, ErrNoRelease
		}
		return ent.release, ent.at, nil
	}
	path := "/releases/latest"
	if channel == ChannelPrerelease {
		path = "/releases?per_page=30"
	}
	u := strings.TrimRight(c.API, "/") + "/repos/" + c.Repo + path
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", userAgentBase+brand.Version)
	if ent != nil && ent.etag != "" {
		req.Header.Set("If-None-Match", ent.etag)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("cannot reach GitHub: %v", cleanNetErr(err))
	}
	defer resp.Body.Close()
	store := func(rel *Release, etag string) {
		c.mu.Lock()
		c.cache[channel] = &cached{at: c.now(), etag: etag, release: rel}
		c.mu.Unlock()
	}
	switch {
	case resp.StatusCode == http.StatusNotModified && ent != nil:
		store(ent.release, ent.etag)
		if ent.release == nil {
			return nil, c.now(), ErrNoRelease
		}
		return ent.release, c.now(), nil
	case resp.StatusCode == http.StatusNotFound:
		store(nil, resp.Header.Get("ETag"))
		return nil, c.now(), ErrNoRelease
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, time.Time{}, errors.New("GitHub refused the request (API rate limit reached); try again later")
	case resp.StatusCode != http.StatusOK:
		return nil, time.Time{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody+1))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read GitHub answer: %v", err)
	}
	if len(body) > maxAPIBody {
		return nil, time.Time{}, errors.New("GitHub answer too large")
	}
	var rel *Release
	if channel == ChannelStable {
		var r Release
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, time.Time{}, fmt.Errorf("unexpected GitHub answer: %v", err)
		}
		if r.Version() != "" && !r.Draft {
			rel = &r
		}
	} else {
		var list []Release
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, time.Time{}, fmt.Errorf("unexpected GitHub answer: %v", err)
		}
		rel = newest(list)
	}
	if rel != nil && len(rel.Body) > maxNotes {
		rel.Body = rel.Body[:maxNotes] + "\n\n…"
	}
	store(rel, resp.Header.Get("ETag"))
	if rel == nil {
		return nil, c.now(), ErrNoRelease
	}
	return rel, c.now(), nil
}

// newest returns the highest-versioned non-draft release.
func newest(list []Release) *Release {
	var best *Release
	var bestV Version
	for i := range list {
		r := &list[i]
		if r.Draft {
			continue
		}
		v, err := ParseVersion(r.Tag)
		if err != nil {
			continue
		}
		if best == nil || v.Compare(bestV) > 0 {
			best, bestV = r, v
		}
	}
	return best
}

// cleanNetErr drops the URL from *url.Error messages.
func cleanNetErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
