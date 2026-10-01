package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a parsed semantic version (https://semver.org), with an
// optional leading "v" accepted and build metadata ignored for ordering.
type Version struct {
	Major, Minor, Patch int
	Pre                 []string // dot-separated pre-release identifiers ("rc", "1")
	Build               string
}

var semverRe = regexp.MustCompile(`^v?(0|[1-9]\d{0,8})\.(0|[1-9]\d{0,8})\.(0|[1-9]\d{0,8})` +
	`(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

// ParseVersion parses "1.2.3", "v1.2.3-rc.1", "1.2.3+build".
func ParseVersion(s string) (Version, error) {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil || len(s) > 128 {
		return Version{}, fmt.Errorf("%q is not a semantic version", s)
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	if m[4] != "" {
		v.Pre = strings.Split(m[4], ".")
		for _, id := range v.Pre {
			if len(id) > 1 && id[0] == '0' && isNum(id) {
				return Version{}, fmt.Errorf("%q: numeric pre-release identifier with a leading zero", s)
			}
		}
	}
	v.Build = m[5]
	return v, nil
}

// String renders the version without a leading "v".
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// Prerelease reports whether the version has a pre-release suffix (-rc.1, -beta).
func (v Version) Prerelease() bool { return len(v.Pre) > 0 }

// Compare returns -1, 0 or 1 following semver precedence (build ignored).
func (v Version) Compare(o Version) int {
	for _, d := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if d[0] != d[1] {
			return cmpInt(d[0], d[1])
		}
	}
	// A version without pre-release ranks higher than one with.
	switch {
	case len(v.Pre) == 0 && len(o.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(o.Pre) == 0:
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(o.Pre); i++ {
		a, b := v.Pre[i], o.Pre[i]
		an, bn := isNum(a), isNum(b)
		switch {
		case an && bn:
			ai, _ := strconv.Atoi(a)
			bi, _ := strconv.Atoi(b)
			if ai != bi {
				return cmpInt(ai, bi)
			}
		case an:
			return -1 // numeric identifiers rank lower than alphanumeric
		case bn:
			return 1
		default:
			if c := strings.Compare(a, b); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(v.Pre), len(o.Pre))
}

// Newer reports whether candidate is a higher version than current. A
// current version that does not parse (a dev build such as "0647974-dirty")
// is treated as older than any release.
func Newer(candidate, current string) bool {
	c, err := ParseVersion(candidate)
	if err != nil {
		return false
	}
	cur, err := ParseVersion(current)
	if err != nil {
		return true
	}
	return c.Compare(cur) > 0
}

func isNum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// dirNameRe is what a folder in versions/ may be called.
var dirNameRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)

// ValidDirName reports whether s can name a version folder.
func ValidDirName(s string) bool { return dirNameRe.MatchString(s) && !strings.Contains(s, "..") }

// DirName turns a version string into a folder name: the semver without
// "v" when it parses, otherwise the string with unsafe characters replaced.
func DirName(version string) string {
	if v, err := ParseVersion(version); err == nil {
		return v.String()
	}
	var b strings.Builder
	for _, r := range strings.TrimSpace(version) {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '_', r == '+', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), ".-_+")
	s = strings.ReplaceAll(s, "..", ".")
	if len(s) > 64 {
		s = s[:64]
	}
	if !ValidDirName(s) {
		return "unknown"
	}
	return s
}
