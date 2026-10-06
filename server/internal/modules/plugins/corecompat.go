package plugins

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// A plugin may say which Ervisio it needs:
//
//	"minCore": "0.5.0"
//	"requires": {"ervisio": ">=0.5.0"}
//
// The two spell the same thing (a plugin may use either, or both: the
// higher version counts). Only ">=" (or a bare version) is understood.
// The core refuses to install or run a plugin that needs a newer core than
// it is, with a message that says which version it needs. The marketplace
// catalog entries carry the same fields (minCore, requires) so Browse can
// say it before anyone installs.

// Requires is the manifest's "requires" object.
type Requires struct {
	Ervisio string `json:"ervisio,omitempty"`
}

var coreReqRe = regexp.MustCompile(`^(?:>=\s*)?v?(\d+)\.(\d+)\.(\d+)$`)

// get is the ervisio constraint, "" for no requires object.
func (r *Requires) get() string {
	if r == nil {
		return ""
	}
	return r.Ervisio
}

// parseCoreReq turns "0.5.0", "v0.5.0" or ">=0.5.0" into a version triple.
func parseCoreReq(s string) ([3]int, error) {
	m := coreReqRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return [3]int{}, fmt.Errorf("%q is not a version Ervisio understands: write 0.5.0 or >=0.5.0", s)
	}
	var v [3]int
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil || n > 1<<20 {
			return v, fmt.Errorf("%q is not a valid version", s)
		}
		v[i] = n
	}
	return v, nil
}

// requiredCore is the lowest core version the manifest needs, ok false when
// it says nothing. The fields are validated by validateCoreReq.
func (m *Manifest) requiredCore() (string, bool) {
	return highestCoreReq(m.MinCore, m.Requires.get())
}

func highestCoreReq(reqs ...string) (string, bool) {
	var best [3]int
	found := false
	for _, r := range reqs {
		if r == "" {
			continue
		}
		v, err := parseCoreReq(r)
		if err != nil {
			continue
		}
		if !found || cmpVer3(v, best) > 0 {
			best, found = v, true
		}
	}
	if !found {
		return "", false
	}
	return fmt.Sprintf("%d.%d.%d", best[0], best[1], best[2]), true
}

func (m *Manifest) validateCoreReq() error {
	for _, f := range []struct{ name, v string }{{"minCore", m.MinCore}, {"requires.ervisio", m.Requires.get()}} {
		if f.v == "" {
			continue
		}
		if _, err := parseCoreReq(f.v); err != nil {
			return fmt.Errorf("%s: %v", f.name, err)
		}
	}
	return nil
}

func cmpVer3(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// coreVersion is this core's version: its numbers, and whether it is a
// development build (a git describe such as v0.4.0-33-gabc1234, or -dev).
func coreVersion() (v [3]int, dev bool) {
	s := strings.TrimPrefix(brand.Version, "v")
	core, pre, _ := strings.Cut(s, "-")
	for i, p := range strings.SplitN(core, ".", 3) {
		v[i], _ = strconv.Atoi(p)
	}
	return v, pre != ""
}

// coreProblem says why a plugin that needs core version `need` ("" = nothing)
// cannot run here, or "" when it can. A development build of the core (a
// daemon run with --dev from a checkout, whose version is the last tag plus
// a suffix) accepts every plugin, so work on the next release can be tried
// before it is tagged.
func coreProblem(name, need string) string {
	if need == "" {
		return ""
	}
	want, err := parseCoreReq(need)
	if err != nil {
		return ""
	}
	have, dev := coreVersion()
	if dev && DaemonDev {
		return ""
	}
	if cmpVer3(have, want) >= 0 {
		return ""
	}
	return fmt.Sprintf("%s needs Ervisio %d.%d.%d or newer; this server runs %d.%d.%d. Update Ervisio first.", name, want[0], want[1], want[2], have[0], have[1], have[2])
}

// CoreProblem says why the plugin cannot run here: another operating system
// (platform.go) or a core that is too old. "" = it can.
func (m *Manifest) CoreProblem() string {
	if msg := platformProblem(m.Name, m.Platforms); msg != "" {
		return msg
	}
	need, _ := m.requiredCore()
	return coreProblem(m.Name, need)
}
