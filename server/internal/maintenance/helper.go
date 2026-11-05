package maintenance

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"regexp"
	"time"
)

const HelperFlag = "--factory-reset"
const DefaultBackupDir = "/var/backups/ervisio/factory-resets"

var validJobID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func planFresh(createdAt int64, now time.Time) bool {
	created := time.Unix(createdAt, 0)
	return !created.After(now.Add(time.Minute)) && now.Sub(created) <= 10*time.Minute
}

type JobStatus struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Phase      string `json:"phase"`
	Backup     string `json:"backup,omitempty"`
	Error      string `json:"error,omitempty"`
	NextURL    string `json:"nextURL,omitempty"`
	TokenHash  string `json:"tokenHash,omitempty"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func VerifyStatusToken(status *JobStatus, token string) bool {
	if status == nil || len(token) != 64 || len(status.TokenHash) != 64 {
		return false
	}
	expected := HashToken(token)
	return subtle.ConstantTimeCompare([]byte(status.TokenHash), []byte(expected)) == 1
}

type helperPlan struct {
	Plan      *Plan  `json:"plan"`
	TokenHash string `json:"tokenHash"`
}

// PublicStatus excludes backup locations and diagnostic errors, even for a bearer status token.
func PublicStatus(status *JobStatus) *JobStatus {
	if status == nil {
		return nil
	}
	out := *status
	out.Backup = ""
	out.Error = ""
	out.TokenHash = ""
	return &out
}
