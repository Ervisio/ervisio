package maintenance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPlanFreshUsesUnixSecondsAndBoundsAgeAndClockSkew(t *testing.T) {
	now := time.Date(2026, time.November, 6, 0, 20, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		created int64
		want    bool
	}{
		{"now", now.Unix(), true},
		{"five minutes old", now.Add(-5 * time.Minute).Unix(), true},
		{"exact ten minute lifetime", now.Add(-10 * time.Minute).Unix(), true},
		{"expired", now.Add(-10*time.Minute - time.Second).Unix(), false},
		{"allowed one minute clock skew", now.Add(time.Minute).Unix(), true},
		{"future beyond allowed clock skew", now.Add(time.Minute + time.Second).Unix(), false},
		{"milliseconds are not accepted as seconds", now.UnixMilli(), false},
		{"missing creation time", 0, false},
		{"negative creation time", -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := planFresh(tc.created, now); got != tc.want {
				t.Fatalf("planFresh(%d, %s)=%v, want %v", tc.created, now, got, tc.want)
			}
		})
	}
}

func TestStatusCapabilityRejectsInvalidLengthsAndMismatchedTokens(t *testing.T) {
	token := strings.Repeat("a1", 32)
	status := &JobStatus{ID: strings.Repeat("b", 32), State: "running", TokenHash: HashToken(token)}
	if !VerifyStatusToken(status, token) {
		t.Fatal("valid read-only capability was refused")
	}
	for _, candidate := range []string{"", token[:63], token + "a", strings.Repeat("x", 1<<20), strings.Repeat("b2", 32), strings.ToUpper(token)} {
		if VerifyStatusToken(status, candidate) {
			t.Fatalf("invalid capability accepted (length %d)", len(candidate))
		}
	}
	if VerifyStatusToken(nil, token) {
		t.Fatal("missing status accepted")
	}
	for _, hash := range []string{"", strings.Repeat("x", 63), strings.Repeat("x", 65), strings.Repeat("x", 1<<20)} {
		if VerifyStatusToken(&JobStatus{TokenHash: hash}, token) {
			t.Fatalf("malformed stored hash accepted (length %d)", len(hash))
		}
	}
}

func TestPublicResetStatusRedactsSecretsWithoutChangingPrivateRecord(t *testing.T) {
	private := &JobStatus{ID: strings.Repeat("a", 32), State: "rolled-back", Phase: "failed", Backup: "/private/backup-with-api-keys.tar.gz", Error: "PRIVATE-CONFIG-DIAGNOSTIC", NextURL: "https://console.example:9090", TokenHash: HashToken(strings.Repeat("b", 64)), FinishedAt: 12345}
	public := PublicStatus(private)
	if public == nil || public == private {
		t.Fatal("public status must be a separate record")
	}
	if public.Backup != "" || public.Error != "" || public.TokenHash != "" {
		t.Fatal("sensitive private status fields were exposed")
	}
	if public.ID != private.ID || public.State != private.State || public.Phase != private.Phase || public.NextURL != private.NextURL || public.FinishedAt != private.FinishedAt {
		t.Fatal("non-secret progress information changed")
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{private.Backup, private.Error, private.TokenHash, "tokenHash", "backup", "error"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("public JSON exposed private field %q", secret)
		}
	}
	if private.Backup == "" || private.Error == "" || private.TokenHash == "" {
		t.Fatal("redaction mutated the privileged diagnostic record")
	}
	if PublicStatus(nil) != nil {
		t.Fatal("nil public status should remain nil")
	}
}

func TestResetJobIdentifierIsASingleBoundedHexComponent(t *testing.T) {
	if !validJobID.MatchString(strings.Repeat("a1", 16)) {
		t.Fatal("generated identifier refused")
	}
	for _, id := range []string{"", strings.Repeat("a", 31), strings.Repeat("a", 33), strings.Repeat("g", 32), strings.Repeat("A", 32), "../" + strings.Repeat("a", 29), strings.Repeat("a", 32) + "/status", strings.Repeat("x", 1<<20)} {
		if validJobID.MatchString(id) {
			t.Fatalf("unsafe identifier accepted (length %d)", len(id))
		}
	}
}
