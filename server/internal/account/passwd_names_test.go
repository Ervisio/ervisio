package account

import "testing"

func TestSplitDomainAccount(t *testing.T) {
	cases := []struct {
		in, dom, user string
		dns, ok       bool
	}{
		{`CORP\alice`, "CORP", "alice", false, true},
		{"alice@corp.example", "corp.example", "alice", true, true},
		{"a@b@corp.example", "corp.example", "a@b", true, true},
		{"alice", "", "", false, false},
		{`CORP\`, "", "", false, false},
		{`\alice`, "", "", false, false},
		{"@corp", "", "", false, false},
	}
	for _, c := range cases {
		d, u, dns, ok := splitDomainAccount(c.in)
		if d != c.dom || u != c.user || dns != c.dns || ok != c.ok {
			t.Errorf("%q: got (%q,%q,%v,%v)", c.in, d, u, dns, ok)
		}
	}
}
