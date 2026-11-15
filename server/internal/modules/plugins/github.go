package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// The marketplace is the public repositories of one GitHub organisation
// (plugins.github_org). A repository is a plugin when its name starts with
// "plugin-" or it has a plugin.ervisio file at its root. The package is the
// "<id>-<version>.tar.gz" attached to the latest release, or, for a
// repository without releases, the plugin folder of its branch.
//
// Only the root daemon talks to GitHub (SyncGitHub): it writes what it found
// to GitHubCatalogPath, which the bridges read. Nothing is signed: whoever can
// push to the organisation decides what runs here.

// GitHubCatalogPath is the marketplace as the root daemon last saw it.
var GitHubCatalogPath = brand.StateDir + "/plugins-github.json"

// GitHub addresses (tests replace them).
var (
	githubAPI      = "https://api.github.com"
	githubRaw      = "https://raw.githubusercontent.com"
	githubWeb      = "https://github.com"
	githubCodeload = "https://codeload.github.com"
)

// githubInterval is how often the organisation is read. The anonymous GitHub
// API allows 60 requests an hour per address and a pass costs one per 100
// repositories; everything else goes to hosts without that limit.
const githubInterval = 15 * time.Minute

const rawLag = 10 * time.Minute

const (
	repoPrefix   = "plugin-"
	repoSpecFile = "plugin.ervisio"
	maxSpecBytes = 16 << 10
	// defaultPluginDir is where the plugin lives in a repository that does
	// not say; the repository root is tried after it.
	defaultPluginDir = "plugin"
)

var (
	repoNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	gitRefRe   = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
)

// defaultCategories are the Browse categories of the marketplace.
var defaultCategories = []CatalogCategory{
	{ID: "Containers", Name: "Containers", Icon: "server", Color: "file"},
	{ID: "Security", Name: "Security", Icon: "shield", Color: "svc"},
	{ID: "Networking", Name: "Networking", Icon: "globe", Color: "term"},
	{ID: "Storage", Name: "Storage", Icon: "disk", Color: "sw"},
	{ID: "Databases", Name: "Databases", Icon: "database", Color: "file"},
	{ID: "Monitoring", Name: "Monitoring", Icon: "zap", Color: "ov"},
	{ID: "Web servers", Name: "Web servers", Icon: "globe", Color: "log"},
	{ID: "Games", Name: "Games", Icon: "play", Color: "term"},
}

// repoSpec is plugin.ervisio: JSON, every field optional, an empty file is valid.
type repoSpec struct {
	// Path is the plugin folder inside the repository ("." = its root).
	Path string `json:"path"`
	// Source is "release" (never offer the branch) or "branch" (ignore
	// releases); empty = the latest release when there is one, else the branch.
	Source string `json:"source"`
	// Branch replaces the default branch.
	Branch   string `json:"branch"`
	Category string `json:"category"`
	Featured bool   `json:"featured"`
}

func parseRepoSpec(data []byte) (*repoSpec, error) {
	var s repoSpec
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("%s: %v", repoSpecFile, err)
		}
	}
	switch s.Source {
	case "", "release", "branch":
	default:
		return nil, fmt.Errorf(`%s: source must be "release" or "branch"`, repoSpecFile)
	}
	if s.Path != "" && s.Path != "." && !cleanRef(s.Path) {
		return nil, fmt.Errorf("%s: path must be a folder inside the repository", repoSpecFile)
	}
	if s.Branch != "" && !cleanRef(s.Branch) {
		return nil, fmt.Errorf("%s: %q is not a branch name", repoSpecFile, s.Branch)
	}
	return &s, nil
}

// cleanRef accepts a branch, tag or folder that is safe inside a URL path.
func cleanRef(s string) bool {
	return gitRefRe.MatchString(s) && path.Clean(s) == s && !path.IsAbs(s) && s != ".." && !strings.HasPrefix(s, "../")
}

type githubRepo struct {
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	PushedAt      string `json:"pushed_at"`
}

func orgRepos(ctx context.Context, org string) ([]githubRepo, error) {
	var out []githubRepo
	for page := 1; page <= 10; page++ {
		data, err := fetchSmall(ctx, fmt.Sprintf("%s/orgs/%s/repos?type=public&per_page=100&page=%d", githubAPI, org, page), maxCatalogBytes)
		if err != nil {
			return nil, err
		}
		var batch []githubRepo
		if err := json.Unmarshal(data, &batch); err != nil {
			return nil, err
		}
		for _, r := range batch {
			if !r.Archived && repoNameRe.MatchString(r.Name) && cleanRef(r.DefaultBranch) {
				out = append(out, r)
			}
		}
		if len(batch) < 100 {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// repoSeen is what the last pass learned about a repository, so one that
// did not change costs no request (one, to look for a new release).
type repoSeen struct {
	pushedAt string
	spec     *repoSpec // nil: not a plugin
	tag      string
	entry    *CatalogEntry
}

var (
	// syncMu serialises what the root daemon does with the marketplace: the
	// passes over the organisation and the installs they lead to.
	syncMu     sync.Mutex
	githubSeen = map[string]repoSeen{}
)

// repoEntry is the catalog entry of the repository full ("org/name"), nil
// when it is not a plugin or has nothing to offer yet.
func repoEntry(ctx context.Context, full string, r githubRepo) (*CatalogEntry, error) {
	seen, ok := githubSeen[full]
	if !ok || seen.pushedAt != r.PushedAt {
		spec, err := fetchRepoSpec(ctx, full, r)
		if err != nil {
			return nil, err
		}
		seen = repoSeen{spec: spec}
		// The raw host serves a push some minutes late: an answer read
		// right after one is not remembered.
		if at, err := time.Parse(time.RFC3339, r.PushedAt); err != nil || time.Since(at) > rawLag {
			seen.pushedAt = r.PushedAt
		}
		githubSeen[full] = seen
	}
	if seen.spec == nil {
		return nil, nil
	}
	tag := ""
	if seen.spec.Source != "branch" {
		var err error
		if tag, err = latestRelease(ctx, full); err != nil {
			return nil, err
		}
		if tag == "" && seen.spec.Source == "release" {
			return nil, nil
		}
	}
	if seen.entry != nil && seen.tag == tag {
		return seen.entry, nil
	}
	e, err := buildEntry(ctx, full, r, *seen.spec, tag)
	if err != nil || e == nil {
		return nil, err
	}
	seen.tag, seen.entry = tag, e
	githubSeen[full] = seen
	return e, nil
}

// fetchRepoSpec reads plugin.ervisio; nil when the repository is not a plugin.
func fetchRepoSpec(ctx context.Context, full string, r githubRepo) (*repoSpec, error) {
	data, err := fetchSmall(ctx, rawURL(full, r.DefaultBranch, repoSpecFile), maxSpecBytes)
	if errors.Is(err, errNotFound) {
		if !strings.HasPrefix(strings.ToLower(r.Name), repoPrefix) {
			return nil, nil
		}
		return &repoSpec{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseRepoSpec(data)
}

func rawURL(full, ref string, file ...string) string {
	return githubRaw + "/" + full + "/" + ref + "/" + path.Join(file...)
}

// latestRelease is the tag of the latest release, "" when there is none.
// It reads the redirect of the releases page, which is not the rate-limited API.
func latestRelease(ctx context.Context, full string) (string, error) {
	loc, err := redirectOf(ctx, githubWeb+"/"+full+"/releases/latest")
	if err != nil {
		return "", err
	}
	_, tag, ok := strings.Cut(loc, "/releases/tag/")
	if !ok {
		return "", nil
	}
	if tag, err = url.PathUnescape(tag); err != nil || !cleanRef(tag) {
		return "", fmt.Errorf("the release tag %q cannot be used", tag)
	}
	return tag, nil
}

// redirectOf is where src redirects to ("" when it does not).
func redirectOf(ctx context.Context, src string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", err
	}
	c := *catalogClient()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", errNotFound
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return resp.Header.Get("Location"), nil
	case resp.StatusCode == http.StatusOK:
		return "", nil
	}
	return "", fmt.Errorf("the server answered %s", resp.Status)
}

// buildEntry reads the plugin of a repository at its release tag ("" = the
// branch). nil: a repository that is a plugin only by name and has no manifest.
func buildEntry(ctx context.Context, full string, r githubRepo, spec repoSpec, tag string) (*CatalogEntry, error) {
	ref := tag
	if ref == "" {
		ref = spec.Branch
	}
	if ref == "" {
		ref = r.DefaultBranch
	}
	dir, data, err := fetchRepoManifest(ctx, full, ref, spec.Path)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	if tag != "" {
		data = withoutFiles(data)
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}
	e := CatalogEntry{
		ID: m.ID, Name: m.Name, Version: m.Version, Author: m.Author, Description: m.Description,
		Icon: m.Icon, Color: m.Color, Category: spec.Category, Featured: spec.Featured, Verified: true,
		MinCore: m.MinCore, Platforms: m.Platforms,
		Capabilities: m.declared, Contributes: m.Contributes, VisibleTo: m.VisibleTo,
		Repo: full, Logo: fetchRepoLogo(ctx, full, ref, dir),
	}
	if m.Requires.Ervisio != "" {
		e.Requires = &m.Requires
	}
	if tag == "" {
		e.Revision, e.Path = r.PushedAt, dir
		e.Source = githubCodeload + "/" + full + "/tar.gz/refs/heads/" + ref
		return &e, nil
	}
	e.Revision = tag
	e.Source = fmt.Sprintf("%s/%s/releases/download/%s/%s-%s.tar.gz", githubWeb, full, tag, m.ID, m.Version)
	// A release counts once its package is attached: the workflow that
	// builds it may still be running.
	if _, err := redirectOf(ctx, e.Source); err != nil {
		return nil, fmt.Errorf("release %s has no package %s yet (%v)", tag, path.Base(e.Source), err)
	}
	return &e, nil
}

// withoutFiles drops the file hashes of a source manifest: a repository that
// releases packages keeps placeholders there, the package has the real ones.
func withoutFiles(data []byte) []byte {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return data
	}
	delete(raw, "files")
	out, _ := json.Marshal(raw)
	return out
}

// fetchRepoManifest returns manifest.json and the folder it was found in.
// data is nil when the repository does not say where its plugin is and has
// none in the usual places.
func fetchRepoManifest(ctx context.Context, full, ref, dir string) (string, []byte, error) {
	dirs := []string{defaultPluginDir, ""}
	if dir != "" {
		dirs = []string{strings.TrimPrefix(dir, ".")}
	}
	for _, d := range dirs {
		data, err := fetchSmall(ctx, rawURL(full, ref, d, "manifest.json"), maxManifestBytes)
		if err == nil {
			return d, data, nil
		}
		if !errors.Is(err, errNotFound) {
			return "", nil, err
		}
	}
	if dir != "" {
		return "", nil, fmt.Errorf("there is no manifest.json in %s", dir)
	}
	return "", nil, nil
}

// fetchRepoLogo is the plugin's logo as a data: URL, "" when it has none.
func fetchRepoLogo(ctx context.Context, full, ref, dir string) string {
	for _, name := range LogoFiles {
		data, err := fetchSmall(ctx, rawURL(full, ref, dir, name), MaxLogoSize)
		if err != nil || len(data) == 0 {
			continue
		}
		mime := "image/png"
		if strings.HasSuffix(name, ".svg") {
			mime = "image/svg+xml"
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return ""
}

// discoverGitHub reads the organisation. A repository that fails keeps the
// entry it had in prev.
func discoverGitHub(ctx context.Context, org string, prev *Catalog, logf func(string, ...any)) (*Catalog, error) {
	repos, err := orgRepos(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("could not list the repositories of %s on GitHub: %v", org, err)
	}
	out := &Catalog{Categories: defaultCategories, Plugins: []CatalogEntry{}}
	owner := map[string]string{}
	for _, r := range repos {
		full := org + "/" + r.Name
		e, err := repoEntry(ctx, full, r)
		if err != nil {
			logf("plugin repository %s: %v", full, err)
			e = prev.byRepo(full)
		}
		if e == nil {
			continue
		}
		if other, taken := owner[e.ID]; taken {
			logf("plugin repository %s: the plugin id %q is already published by %s", full, e.ID, other)
			continue
		}
		owner[e.ID] = full
		out.Plugins = append(out.Plugins, *e)
	}
	return out, nil
}

func (c *Catalog) byRepo(full string) *CatalogEntry {
	if c == nil {
		return nil
	}
	for i := range c.Plugins {
		if c.Plugins[i].Repo == full {
			return &c.Plugins[i]
		}
	}
	return nil
}

func (c *Catalog) bySource(source string) *CatalogEntry {
	if c == nil {
		return nil
	}
	for i := range c.Plugins {
		if c.Plugins[i].Source == source {
			return &c.Plugins[i]
		}
	}
	return nil
}

// githubFile is GitHubCatalogPath.
type githubFile struct {
	Org string `json:"org"`
	Catalog
}

// readGitHubCatalog is the marketplace as last written by the root daemon,
// nil when it is off or was not read yet. It never touches the network.
func readGitHubCatalog(p policy) *Catalog {
	if p.GitHubOrg == "" {
		return nil
	}
	b, err := os.ReadFile(GitHubCatalogPath)
	if err != nil {
		return nil
	}
	var head struct {
		Org string `json:"org"`
	}
	if json.Unmarshal(b, &head) != nil || !strings.EqualFold(head.Org, p.GitHubOrg) {
		return nil
	}
	c, err := parseCatalog(b)
	if err != nil {
		return nil
	}
	return c
}

// refreshGitHubCatalog reads the organisation again and rewrites
// GitHubCatalogPath, unless the file is younger than maxAge. When GitHub
// cannot be read it returns the catalog it had, with the error. The caller
// holds syncMu and is root.
func refreshGitHubCatalog(ctx context.Context, p policy, maxAge time.Duration, logf func(string, ...any)) (*Catalog, error) {
	if p.GitHubOrg == "" {
		_ = os.Remove(GitHubCatalogPath)
		return nil, nil
	}
	prev := readGitHubCatalog(p)
	if fi, err := os.Stat(GitHubCatalogPath); err == nil && prev != nil && time.Since(fi.ModTime()) < maxAge {
		return prev, nil
	}
	cat, err := discoverGitHub(ctx, p.GitHubOrg, prev, logf)
	if err != nil {
		return prev, err
	}
	b, err := json.MarshalIndent(githubFile{Org: p.GitHubOrg, Catalog: *cat}, "", "  ")
	if err != nil {
		return prev, err
	}
	if err := os.MkdirAll(filepath.Dir(GitHubCatalogPath), 0o755); err != nil {
		return prev, err
	}
	if err := writeFileAtomic(GitHubCatalogPath, append(b, '\n'), 0o644); err != nil {
		return prev, err
	}
	return cat, nil
}

// isUpdate says whether the catalog entry is newer than the installed plugin.
// A plugin installed from the entry's repository follows its revision (a
// branch has no version to compare); anything else compares versions.
func isUpdate(e *CatalogEntry, m *Manifest, o origin) bool {
	if e.Repo != "" && e.Repo == o.Repo {
		return e.Revision != o.Revision
	}
	return compareSemver(e.Version, m.Version) > 0
}

// SyncGitHub keeps the marketplace catalog and the plugins installed from it
// up to date until ctx ends. It runs in the root daemon.
func SyncGitHub(ctx context.Context, logf func(string, ...any)) {
	said := map[string]bool{}
	once := func(format string, a ...any) {
		if msg := fmt.Sprintf(format, a...); !said[msg] {
			said[msg] = true
			logf("%s", msg)
		}
	}
	for {
		syncGitHubOnce(ctx, once)
		select {
		case <-ctx.Done():
			return
		case <-time.After(githubInterval):
		}
	}
}

func syncGitHubOnce(ctx context.Context, logf func(string, ...any)) {
	syncMu.Lock()
	defer syncMu.Unlock()
	p := readPolicy()
	cat, err := refreshGitHubCatalog(ctx, p, time.Minute, logf)
	if err != nil {
		logf("plugin marketplace: %v", err)
	}
	st := readState()
	for _, f := range scan(p) {
		if f.M == nil || f.Location != LocInstalled {
			continue
		}
		e := cat.find(f.M.ID)
		if e == nil || !isUpdate(e, f.M, st.Origins[f.M.ID]) || entryProblem(e) != "" {
			continue
		}
		in, err := install(ctx, installRequest{Source: e.Source})
		if err != nil {
			logf("plugin %s: updating from %s (%s) failed: %v", e.ID, e.Repo, e.Revision, err)
			continue
		}
		logf("plugin %s: updated to %s from %s (%s)", e.ID, in.Version, e.Repo, e.Revision)
	}
}
