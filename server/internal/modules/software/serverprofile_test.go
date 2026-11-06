package software

import (
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/sys"
)

func TestServerProfileRequiresPositiveEditionOrImageMarkers(t *testing.T) {
	for _, test := range []struct {
		release string
		markers map[string]string
		want    bool
	}{
		{"ID=ubuntu\nNAME=Ubuntu", nil, false},
		{"ID=ubuntu", map[string]string{"/etc/cloud/build.info": "build_name: desktop\n"}, false},
		{"ID=ubuntu", map[string]string{"/etc/cloud/build.info": "build_name: server\n"}, true},
		{"ID=ubuntu", map[string]string{"/var/lib/dpkg/info/ubuntu-server-minimal.list": "/.\n"}, true},
		{"ID=fedora\nVARIANT_ID=server", nil, true},
		{"ID=fedora\nVARIANT_ID=workstation", nil, false},
		{"ID=debian", nil, false},
	} {
		got := serverProfile(sys.ParseOSRelease(strings.NewReader(test.release)), func(path string) []byte { return []byte(test.markers[path]) })
		if got != test.want {
			t.Fatalf("%s %v: got %v", test.release, test.markers, got)
		}
	}
}

func TestCLIClassificationIsPositiveAndDoesNotMutateCachedResults(t *testing.T) {
	items := []Result{{Name: "git", Kind: KindRepo}, {Name: "firefox", Kind: KindRepo}, {Name: "unknown-package", Kind: KindRepo}, {Name: "git", Kind: KindFlatpak}, {Name: "postgresql-client", Kind: KindRepo}}
	out := classifyCLI(items)
	for i, want := range []bool{true, false, false, false, true} {
		if out[i].CLI != want || items[i].CLI {
			t.Fatalf("bad classification at %d", i)
		}
	}
}
