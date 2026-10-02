package plugins

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CatalogEntry is one plugin offered by Browse.
type CatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Color       string `json:"color"`
	Category    string `json:"category"`
	Verified    bool   `json:"verified"`
	Installs    int    `json:"installs"`
	Featured    bool   `json:"featured,omitempty"`
	Notes       string `json:"notes,omitempty"`
	// MinCore and Requires (as in the manifest) say which Ervisio the
	// plugin needs; Browse shows it and the installer enforces it.
	MinCore  string    `json:"minCore,omitempty"`
	Requires *Requires `json:"requires,omitempty"`
	// Incompatible is set by the daemon when this core is too old.
	Incompatible string `json:"incompatible,omitempty"`
	// Source is the https URL of the .tar.gz; SHA256 its checksum (optional).
	Source string `json:"source,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	// Capabilities and Contributes are what the install dialog lists. The
	// installer refuses the package when its manifest asks for more.
	Capabilities   Capabilities `json:"capabilities"`
	Contributes    Contributes  `json:"contributes"`
	VisibleTo      VisibleTo    `json:"visibleTo"`
	NewPermissions bool         `json:"-"`
}

// CatalogCategory groups entries.
type CatalogCategory struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// Catalog is the catalog file format.
type Catalog struct {
	Categories []CatalogCategory `json:"categories"`
	Plugins    []CatalogEntry    `json:"plugins"`
}

func (c *Catalog) find(id string) *CatalogEntry {
	if c == nil {
		return nil
	}
	for i := range c.Plugins {
		if c.Plugins[i].ID == id {
			return &c.Plugins[i]
		}
	}
	return nil
}

// CatalogFiles are searched for catalog.json (first existing wins).
func catalogFiles(p policy) []string {
	var out []string
	for _, d := range devDirs(p) {
		out = append(out, filepath.Join(d, "catalog.json"))
	}
	return append(out, filepath.Join(InstalledDir, "catalog.json"), filepath.Join(SystemDir, "catalog.json"))
}

func parseCatalog(data []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("catalog: %v", err)
	}
	seen := map[string]bool{}
	out := c.Plugins[:0]
	for i := range c.Plugins {
		e := c.Plugins[i]
		if !idRe.MatchString(e.ID) || !semverRe.MatchString(e.Version) || seen[e.ID] {
			continue // ignore malformed entries instead of failing the whole catalog
		}
		seen[e.ID] = true
		e.Capabilities = emptyIfNil(e.Capabilities)
		if e.Contributes.Pages == nil || e.Contributes.Widgets == nil || e.Contributes.Snippets == nil {
			m := Manifest{Contributes: e.Contributes}
			m.normalise()
			e.Contributes = m.Contributes
		}
		if e.VisibleTo.Groups == nil {
			e.VisibleTo.Groups = []string{}
		}
		out = append(out, e)
	}
	c.Plugins = out
	if c.Categories == nil {
		c.Categories = []CatalogCategory{}
	}
	return &c, nil
}

func emptyIfNil(c Capabilities) Capabilities {
	m := Manifest{Capabilities: c}
	m.normalise()
	return m.Capabilities
}

// loadLocalCatalog reads the catalog file shipped with the install. A
// missing file is an empty catalog.
func loadLocalCatalog() (*Catalog, error) {
	p := readPolicy()
	for _, f := range catalogFiles(p) {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		return parseCatalog(b)
	}
	return &Catalog{Categories: []CatalogCategory{}, Plugins: []CatalogEntry{}}, nil
}

// catalogSigPrefix is prepended to the canonical catalog before signing
// (catalog.sig), so a catalog signature can never pass for a plugin
// signature or the other way round.
const catalogSigPrefix = "ervisio-catalog-v1\n"

// Remote catalog limits.
const (
	maxCatalogBytes = 2 << 20
	maxCatalogSig   = 1 << 10
	catalogTTL      = 5 * time.Minute
)

// CatalogMessage is the byte string a catalog signature covers.
func CatalogMessage(catalog []byte) ([]byte, error) {
	c, err := Canonical(catalog)
	if err != nil {
		return nil, err
	}
	return append([]byte(catalogSigPrefix), c...), nil
}

// SignCatalog returns the catalog.sig content (base64 and a newline).
func SignCatalog(catalog []byte, priv ed25519.PrivateKey) ([]byte, error) {
	msg, err := CatalogMessage(catalog)
	if err != nil {
		return nil, fmt.Errorf("catalog.json: %v", err)
	}
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg)) + "\n"), nil
}

// VerifyCatalog checks a catalog.sig against the keys.
func VerifyCatalog(catalog, sig []byte, keys []ed25519.PublicKey) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New("catalog.sig is not a base64 ed25519 signature")
	}
	msg, err := CatalogMessage(catalog)
	if err != nil {
		return fmt.Errorf("catalog.json: %v", err)
	}
	for _, k := range keys {
		if ed25519.Verify(k, msg, raw) {
			return nil
		}
	}
	return errors.New("the catalog signature does not match any trusted key")
}

// catalogSigURL is where the signature of a catalog lives: the same
// address with a final ".json" replaced by ".sig" (".sig" appended when the
// path does not end in .json).
func catalogSigURL(u *url.URL) string {
	s := *u
	s.RawQuery, s.Fragment, s.RawPath = "", "", ""
	if strings.HasSuffix(s.Path, ".json") {
		s.Path = strings.TrimSuffix(s.Path, ".json") + ".sig"
	} else {
		s.Path += ".sig"
	}
	return s.String()
}

// remoteSource says which remote catalog to read: the https URL on the
// first line of CatalogURLFile when that file exists (an override kept from
// earlier versions), else plugins.catalog_url. "" = no remote catalog.
func remoteSource(p policy) (string, error) {
	if noRemoteCatalog {
		return "", nil
	}
	raw := p.CatalogURL
	if b, err := os.ReadFile(CatalogURLFile); err == nil {
		if line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0]); line != "" {
			raw = line
		}
	}
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("the plugin catalog address %q is not an https URL", raw)
	}
	return u.String(), nil
}

// catalogKeys are the keys trusted for catalog signatures: the team keys
// plus plugins.catalog_key.
func catalogKeys(p policy) []ed25519.PublicKey {
	keys := append([]ed25519.PublicKey(nil), Keys()...)
	if p.CatalogKey != "" {
		if k, err := base64.StdEncoding.DecodeString(p.CatalogKey); err == nil && len(k) == ed25519.PublicKeySize {
			keys = append(keys, ed25519.PublicKey(k))
		}
	}
	return keys
}

type remoteResult struct {
	url string
	cat *Catalog
	err error
	at  time.Time
}

// noRemoteCatalog turns the remote catalog off (tests that do not want the
// network).
var noRemoteCatalog bool

var (
	remoteMu   sync.Mutex
	remoteLast *remoteResult
	remoteBusy bool
	// catalogClient fetches the remote catalog (tests replace it).
	catalogClient = func() *http.Client { c := httpClient(); c.Timeout = 10 * time.Second; return c }
)

// loadRemoteCatalog returns the signed remote catalog (nil, nil when none
// is configured). Results, failures included, are cached for 5 minutes. A
// catalog without a valid signature by a trusted key is an error: it is
// ignored and its entries are never offered.
func loadRemoteCatalog(ctx context.Context) (*Catalog, error) {
	p := readPolicy()
	src, err := remoteSource(p)
	if err != nil || src == "" {
		return nil, err
	}
	remoteMu.Lock()
	defer remoteMu.Unlock()
	if r := remoteLast; r != nil && r.url == src && time.Since(r.at) < catalogTTL {
		return r.cat, r.err
	}
	c, err := fetchSignedCatalog(ctx, src, catalogKeys(p))
	remoteLast = &remoteResult{url: src, cat: c, err: err, at: time.Now()}
	return c, err
}

// cachedRemoteCatalog returns the cached remote catalog without waiting for
// the network. When the cache is stale it starts a refresh in the
// background, so the next call sees it.
func cachedRemoteCatalog() *Catalog {
	p := readPolicy()
	src, err := remoteSource(p)
	if err != nil || src == "" {
		return nil
	}
	remoteMu.Lock()
	r := remoteLast
	stale := r == nil || r.url != src || time.Since(r.at) >= catalogTTL
	start := stale && !remoteBusy
	if start {
		remoteBusy = true
	}
	remoteMu.Unlock()
	if start {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = loadRemoteCatalog(ctx)
			remoteMu.Lock()
			remoteBusy = false
			remoteMu.Unlock()
		}()
	}
	if r == nil || r.url != src {
		return nil
	}
	return r.cat
}

func fetchSignedCatalog(ctx context.Context, src string, keys []ed25519.PublicKey) (*Catalog, error) {
	u, _ := url.Parse(src)
	data, err := fetchSmall(ctx, src, maxCatalogBytes)
	if err != nil {
		return nil, fmt.Errorf("could not fetch the plugin catalog: %v", err)
	}
	sig, err := fetchSmall(ctx, catalogSigURL(u), maxCatalogSig)
	if err != nil {
		return nil, fmt.Errorf("the plugin catalog at %s was ignored: its signature could not be fetched (%v)", u.Host, err)
	}
	if err := VerifyCatalog(data, sig, keys); err != nil {
		return nil, fmt.Errorf("the plugin catalog at %s was ignored: %v", u.Host, err)
	}
	return parseCatalog(data)
}

func fetchSmall(ctx context.Context, src string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	resp, err := catalogClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server answered %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("larger than %d KiB", limit>>10)
	}
	return data, nil
}

// httpClient only follows redirects to https.
func httpClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) > 4 {
				return fmt.Errorf("refusing to follow a redirect to %s", req.URL.Redacted())
			}
			return nil
		},
	}
}

// CatalogView is the result of plugins.catalog.
type CatalogView struct {
	Categories []CatalogCategory `json:"categories"`
	Plugins    []CatalogItem     `json:"plugins"`
	// Warning is set when the remote catalog could not be read or was not
	// signed by a trusted key (the local one is still shown).
	Warning string `json:"warning,omitempty"`
	// Moved lists plugins that used to ship with Ervisio, are offered by
	// the catalog and are not installed on this machine although it looks
	// like they are wanted (moved.go). The UI shows an "Install" card.
	Moved []MovedNotice `json:"moved"`
}

// CatalogItem is an entry plus its install state.
type CatalogItem struct {
	CatalogEntry
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion,omitempty"`
}

// mergeCatalogs adds the remote entries to the local ones (a remote entry
// replaces a local one with the same id). The remote categories win when
// it has any.
func mergeCatalogs(local, remote *Catalog) *Catalog {
	out := &Catalog{Categories: local.Categories, Plugins: append([]CatalogEntry(nil), local.Plugins...)}
	if remote == nil {
		return out
	}
	byID := map[string]int{}
	for i, e := range out.Plugins {
		byID[e.ID] = i
	}
	for _, e := range remote.Plugins {
		if i, ok := byID[e.ID]; ok {
			out.Plugins[i] = e
		} else {
			byID[e.ID] = len(out.Plugins)
			out.Plugins = append(out.Plugins, e)
		}
	}
	if len(remote.Categories) > 0 {
		out.Categories = remote.Categories
	}
	return out
}

func catalogView(ctx context.Context) (*CatalogView, error) {
	local, err := loadLocalCatalog()
	if err != nil {
		return nil, err
	}
	remote, rerr := loadRemoteCatalog(ctx)
	merged := mergeCatalogs(local, remote)
	view := &CatalogView{Categories: merged.Categories, Plugins: []CatalogItem{}, Moved: []MovedNotice{}}
	if rerr != nil {
		view.Warning = rerr.Error()
	}
	installed := map[string]string{}
	for _, f := range scan(readPolicy()) {
		if f.M != nil {
			installed[f.M.ID] = f.M.Version
		}
	}
	for _, e := range merged.Plugins {
		if need, ok := highestCoreReq(e.MinCore, e.Requires.get()); ok {
			e.Incompatible = coreProblem(e.Name, need)
		}
		it := CatalogItem{CatalogEntry: e}
		it.InstalledVersion, it.Installed = installed[e.ID]
		view.Plugins = append(view.Plugins, it)
	}
	view.Moved = movedNotices(merged, installed)
	return view, nil
}
