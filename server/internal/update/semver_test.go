package update

import "testing"

func TestCompareOrder(t *testing.T) {
	// Ascending, from the semver spec plus a few of our own.
	order := []string{
		"0.9.9", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "v1.0.1", "1.1.0-rc.1", "1.1.0", "1.10.0", "2.0.0",
	}
	for i := range order {
		for j := range order {
			a, err := ParseVersion(order[i])
			if err != nil {
				t.Fatal(err)
			}
			b, _ := ParseVersion(order[j])
			want := cmpInt(i, j)
			if got := a.Compare(b); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	a, _ := ParseVersion("1.2.3+build.5")
	b, _ := ParseVersion("1.2.3")
	if a.Compare(b) != 0 {
		t.Error("build metadata must not affect precedence")
	}
}

func TestParseVersion(t *testing.T) {
	for _, s := range []string{"", "1", "1.2", "1.2.3.4", "01.2.3", "1.2.3-", "1.2.3-01", "1.2.3-a..b", "v", "1.2.3 ", "x1.2.3", "1.2.3-ü"} {
		if _, err := ParseVersion(s); err == nil && s != "1.2.3 " {
			t.Errorf("ParseVersion(%q) accepted", s)
		}
	}
	v, err := ParseVersion("v2.3.4-rc.1+abc")
	if err != nil || v.Major != 2 || v.Minor != 3 || v.Patch != 4 || !v.Prerelease() || v.String() != "2.3.4-rc.1+abc" {
		t.Fatalf("got %+v %v", v, err)
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		cand, cur string
		want      bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "v1.0.0", false},
		{"0.9.0", "1.0.0", false},
		{"1.0.0", "1.0.0-rc.2", true},
		{"1.0.0-rc.2", "1.0.0", false},
		{"1.0.0", "0647974-dirty", true}, // dev builds are older than any release
		{"1.0.0", "0.1.0-dev", true},
		{"garbage", "1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.cand, c.cur); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cand, c.cur, got, c.want)
		}
	}
}

func TestDirName(t *testing.T) {
	cases := map[string]string{
		"v1.2.3":          "1.2.3",
		"1.2.3-rc.1":      "1.2.3-rc.1",
		"0647974-dirty":   "0647974-dirty",
		"v0.1.0-3-gab/..": "v0.1.0-3-gab",
		"../../etc":       "etc",
		"":                "unknown",
		"//":              "unknown",
	}
	for in, want := range cases {
		got := DirName(in)
		if got != want {
			t.Errorf("DirName(%q) = %q, want %q", in, got, want)
		}
		if got != "unknown" && !ValidDirName(got) {
			t.Errorf("DirName(%q) = %q is not a valid folder name", in, got)
		}
	}
	for _, bad := range []string{"", ".", "..", ".hidden", "a/b", "a..b", "-x"} {
		if ValidDirName(bad) {
			t.Errorf("ValidDirName(%q) = true", bad)
		}
	}
}
