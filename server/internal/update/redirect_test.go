package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// After the repository moved from Fonlogen/LinuxAdmin to ervisio/ervisio,
// the GitHub API answers the old path with 301 to /repositories/<id>/….
// Consoles still running LinuxAdmin ask the old path with this same
// Checker code (plain http.Client), so the redirect must be followed for
// the release, with the request headers kept.
func TestCheckerFollowsMovedRepository(t *testing.T) {
	var hdr http.Header
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Fonlogen/LinuxAdmin/releases/latest":
			http.Redirect(w, r, "/repositories/123456/releases/latest", http.StatusMovedPermanently)
		case "/repositories/123456/releases/latest":
			hdr = r.Header.Clone()
			w.Header().Set("ETag", `"e1"`)
			json.NewEncoder(w).Encode(Release{Tag: "v0.3.0", Assets: []Asset{{Name: "linuxadmin-0.3.0-linux-amd64.tar.gz", Size: 1,
				URL: "https://github.com/ervisio/ervisio/releases/download/v0.3.0/linuxadmin-0.3.0-linux-amd64.tar.gz"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Checker{API: srv.URL, Repo: "Fonlogen/LinuxAdmin", Client: srv.Client()}
	rel, _, err := c.Latest(context.Background(), ChannelStable, true)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version() != "0.3.0" || rel.Asset("linuxadmin-0.3.0-linux-amd64.tar.gz") == nil {
		t.Fatalf("release %+v", rel)
	}
	if hdr.Get("Accept") != "application/vnd.github+json" || hdr.Get("X-GitHub-Api-Version") == "" {
		t.Errorf("headers lost on the redirect: %v", hdr)
	}
	// The download URL of the moved repository is still a GitHub one.
	u := rel.Asset("linuxadmin-0.3.0-linux-amd64.tar.gz").URL
	if !DefaultAllowURL(mustURL(t, u)) {
		t.Errorf("download URL %s refused", u)
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
