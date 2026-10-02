package jobs

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Webhooks: POST /hooks/<plugin>/<token>. The token is 32 random bytes
// (base64url, 43 characters); only its SHA-256 is stored. The endpoint is
// unauthenticated by design, so it leaks nothing: an unknown token, a
// disabled instance and a turned-off plugin all answer the same 404.

const (
	// A token may call 6 times in a burst, then once every 10 seconds.
	hookBurst  = 6
	hookRefill = 10 * time.Second
	// A client address may miss 20 times, then once every 3 seconds.
	missBurst  = 20
	missRefill = 3 * time.Second
	// MaxHookBody is how much of a webhook body is read.
	MaxHookBody = 4 << 10
)

type bucket struct {
	tokens float64
	at     time.Time
}

type hookLimits struct {
	mu      sync.Mutex
	byToken map[string]*bucket
	byIP    map[string]*bucket
}

func newHookLimits() hookLimits {
	return hookLimits{byToken: map[string]*bucket{}, byIP: map[string]*bucket{}}
}

// take removes one token from the bucket of key (when consume) and says
// whether one was available.
func take(m map[string]*bucket, key string, now time.Time, burst int, refill time.Duration, consume bool) bool {
	b := m[key]
	if b == nil {
		b = &bucket{tokens: float64(burst), at: now}
		if consume {
			m[key] = b
		}
	}
	b.tokens += float64(now.Sub(b.at)) / float64(refill)
	if b.tokens > float64(burst) {
		b.tokens = float64(burst)
	}
	b.at = now
	if b.tokens < 1 {
		return false
	}
	if consume {
		b.tokens--
	}
	return true
}

func (h *hookLimits) gc(now time.Time) {
	if len(h.byToken) > 5000 {
		h.byToken = map[string]*bucket{}
	}
	if len(h.byIP) > 5000 {
		h.byIP = map[string]*bucket{}
	}
}

// NewToken makes a webhook token and its stored hash.
func NewToken() (token, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

// HashToken is the SHA-256 of a token, as stored.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// HookResult is the answer to a webhook call.
type HookResult struct {
	// Status is 202, 400, 404 or 429.
	Status int
	// Run is the run id (202 only).
	Run string
	// RetryAfter is in seconds (429 only).
	RetryAfter int
}

// HandleHook serves POST /hooks/<plugin>/<token>. ip is the client
// address (for the miss limiter); body and query may set the params the
// job lists in webhook.params.
func (m *Manager) HandleHook(plugin, token, ip string, body []byte, query url.Values) HookResult {
	now := m.env.Now()
	hl := &m.hooks
	hl.mu.Lock()
	hl.gc(now)
	// A client that keeps missing is slowed down before any lookup.
	if !take(hl.byIP, ip, now, missBurst, missRefill, false) {
		hl.mu.Unlock()
		return HookResult{Status: http.StatusTooManyRequests, RetryAfter: int(missRefill.Seconds())}
	}
	hl.mu.Unlock()

	hash := HashToken(token)
	m.mu.Lock()
	var in *Instance
	hookID := ""
	// Every stored hash is compared in constant time, none is skipped.
	for _, cand := range m.list {
		for i := range cand.Webhooks {
			if subtle.ConstantTimeCompare([]byte(cand.Webhooks[i].Hash), []byte(hash)) == 1 && cand.Plugin == plugin {
				in, hookID = cand, cand.Webhooks[i].ID
			}
		}
	}
	var snap Instance
	if in != nil && in.Enabled {
		snap = *in
	} else {
		in = nil
	}
	m.mu.Unlock()

	miss := func() HookResult {
		hl.mu.Lock()
		take(hl.byIP, ip, now, missBurst, missRefill, true)
		hl.mu.Unlock()
		return HookResult{Status: http.StatusNotFound}
	}
	if in == nil {
		return miss()
	}
	man, err := m.env.Manifest(snap.Plugin)
	if err != nil {
		return miss()
	}
	job := man.Job(snap.Job)
	if job == nil {
		return miss()
	}
	hl.mu.Lock()
	ok := take(hl.byToken, hash, now, hookBurst, hookRefill, true)
	hl.mu.Unlock()
	if !ok {
		return HookResult{Status: http.StatusTooManyRequests, RetryAfter: int(hookRefill.Seconds())}
	}

	// Only the params the job lists may come from the call.
	over := map[string]string{}
	if job.Webhook != nil && len(job.Webhook.Params) > 0 {
		given := map[string]string{}
		if t := strings.TrimSpace(string(body)); strings.HasPrefix(t, "{") {
			var raw map[string]json.RawMessage
			if json.Unmarshal(body, &raw) == nil {
				for k, v := range raw {
					var s string
					if json.Unmarshal(v, &s) == nil {
						given[k] = s
					} else if n := strings.TrimSpace(string(v)); n != "" && n[0] >= '0' && n[0] <= '9' {
						given[k] = n
					}
				}
			}
		}
		for k, v := range query {
			if len(v) > 0 {
				given[k] = v[0]
			}
		}
		for _, name := range job.Webhook.Params {
			v, has := given[name]
			if !has {
				continue
			}
			if p := job.Param(name); p == nil || p.CheckValue(v) != nil {
				return HookResult{Status: http.StatusBadRequest}
			}
			over[name] = v
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.findLocked(snap.ID)
	if cur == nil || !cur.Enabled {
		return HookResult{Status: http.StatusNotFound}
	}
	for i := range cur.Webhooks {
		if cur.Webhooks[i].ID == hookID {
			cur.Webhooks[i].LastUsed = now.UnixMilli()
		}
	}
	runID, err := m.beginLocked(cur, Trigger{Kind: "webhook", Params: over})
	if err != nil {
		return HookResult{Status: http.StatusNotFound}
	}
	snapAudit := *cur
	defer m.auditWebhook(&snapAudit, hookID, ip, runID)
	return HookResult{Status: http.StatusAccepted, Run: runID}
}
