package plugins

import (
	"context"
	"encoding/json"
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

var (
	remoteMu    sync.Mutex
	remoteCache *Catalog
	remoteAt    time.Time
)

// loadRemoteCatalog fetches the catalog from the URL in CatalogURLFile
// (https only, 5 s, 2 MiB). It returns nil when no URL is configured.
func loadRemoteCatalog(ctx context.Context) (*Catalog, error) {
	b, err := os.ReadFile(CatalogURLFile)
	if err != nil {
		return nil, nil
	}
	raw := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("the catalog address in %s must be an https URL", CatalogURLFile)
	}
	remoteMu.Lock()
	defer remoteMu.Unlock()
	if remoteCache != nil && time.Since(remoteAt) < 5*time.Minute {
		return remoteCache, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch the plugin catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the plugin catalog server answered %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
		return nil, fmt.Errorf("the plugin catalog is unreadable or larger than 2 MiB")
	}
	c, err := parseCatalog(data)
	if err != nil {
		return nil, err
	}
	remoteCache, remoteAt = c, time.Now()
	return c, nil
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
	// Warning is set when the remote catalog could not be read (the local one is still shown).
	Warning string `json:"warning,omitempty"`
}

// CatalogItem is an entry plus its install state.
type CatalogItem struct {
	CatalogEntry
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion,omitempty"`
}

func catalogView(ctx context.Context) (*CatalogView, error) {
	local, err := loadLocalCatalog()
	if err != nil {
		return nil, err
	}
	view := &CatalogView{Categories: local.Categories, Plugins: []CatalogItem{}}
	entries := local.Plugins
	if remote, rerr := loadRemoteCatalog(ctx); rerr != nil {
		view.Warning = rerr.Error()
	} else if remote != nil {
		byID := map[string]int{}
		for i, e := range entries {
			byID[e.ID] = i
		}
		for _, e := range remote.Plugins {
			if i, ok := byID[e.ID]; ok {
				entries[i] = e
			} else {
				entries = append(entries, e)
			}
		}
		if len(remote.Categories) > 0 {
			view.Categories = remote.Categories
		}
	}
	installed := map[string]string{}
	for _, f := range scan(readPolicy()) {
		if f.M != nil {
			installed[f.M.ID] = f.M.Version
		}
	}
	for _, e := range entries {
		it := CatalogItem{CatalogEntry: e}
		it.InstalledVersion, it.Installed = installed[e.ID]
		view.Plugins = append(view.Plugins, it)
	}
	return view, nil
}
