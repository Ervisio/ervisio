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

	"github.com/ervisio/ervisio/server/internal/modules/plugins"
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

type rate struct {
	burst  int
	refill time.Duration
}

var (
	hookRate = rate{hookBurst, hookRefill}
	missRate = rate{missBurst, missRefill}
)

// refilled returns the bucket of key brought up to now; fresh says it did
// not exist yet (and is not stored).
func refilled(m map[string]*bucket, key string, now time.Time, r rate) (b *bucket, fresh bool) {
	b = m[key]
	if b == nil {
		b, fresh = &bucket{tokens: float64(r.burst), at: now}, true
	}
	b.tokens += float64(now.Sub(b.at)) / float64(r.refill)
	if b.tokens > float64(r.burst) {
		b.tokens = float64(r.burst)
	}
	b.at = now
	return b, fresh
}

// peek says whether the bucket of key has a token, without using it.
func peek(m map[string]*bucket, key string, now time.Time, r rate) bool {
	b, _ := refilled(m, key, now, r)
	return b.tokens >= 1
}

// take removes one token from the bucket of key and says whether one was
// available.
func take(m map[string]*bucket, key string, now time.Time, r rate) bool {
	b, fresh := refilled(m, key, now, r)
	if fresh {
		m[key] = b
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
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
	allowed := peek(hl.byIP, ip, now, missRate)
	hl.mu.Unlock()
	if !allowed {
		return HookResult{Status: http.StatusTooManyRequests, RetryAfter: int(missRefill.Seconds())}
	}

	hash := HashToken(token)
	snap, hookID, found := m.findHook(plugin, hash)
	miss := func() HookResult {
		hl.mu.Lock()
		take(hl.byIP, ip, now, missRate)
		hl.mu.Unlock()
		return HookResult{Status: http.StatusNotFound}
	}
	if !found {
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
	ok := take(hl.byToken, hash, now, hookRate)
	hl.mu.Unlock()
	if !ok {
		return HookResult{Status: http.StatusTooManyRequests, RetryAfter: int(hookRefill.Seconds())}
	}

	over, valid := hookParams(job, body, query)
	if !valid {
		return HookResult{Status: http.StatusBadRequest}
	}
	// An instance that runs steps as root runs with the values an
	// administrator approved, and only those: a call that tries to set a
	// param is refused (400), whatever webhook.params says.
	if len(over) > 0 {
		a, err := m.env.Account(snap.Owner)
		if err != nil || a == nil || man.JobNeedsAdmin(job, a.IsRoot(), groupSet(a)) {
			return HookResult{Status: http.StatusBadRequest}
		}
	}
	return m.runHook(snap.ID, hookID, ip, now, over)
}

// findHook looks up the enabled instance owning the webhook with this hash
// and returns a copy of it.
func (m *Manager) findHook(plugin, hash string) (snap Instance, hookID string, found bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var in *Instance
	// Every stored hash is compared in constant time, none is skipped.
	for _, cand := range m.list {
		for i := range cand.Webhooks {
			if subtle.ConstantTimeCompare([]byte(cand.Webhooks[i].Hash), []byte(hash)) == 1 && cand.Plugin == plugin {
				in, hookID = cand, cand.Webhooks[i].ID
			}
		}
	}
	if in == nil || !in.Enabled {
		return Instance{}, "", false
	}
	return *in, hookID, true
}

// bodyParams reads the top-level string and number values of a JSON object
// body; anything else yields nothing.
func bodyParams(body []byte) map[string]string {
	given := map[string]string{}
	if t := strings.TrimSpace(string(body)); !strings.HasPrefix(t, "{") {
		return given
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return given
	}
	for k, v := range raw {
		var s string
		if json.Unmarshal(v, &s) == nil {
			given[k] = s
		} else if n := strings.TrimSpace(string(v)); n != "" && n[0] >= '0' && n[0] <= '9' {
			given[k] = n
		}
	}
	return given
}

// hookParams picks the params the job lists from the call (the query wins
// over the body). valid is false when one of them is not acceptable.
func hookParams(job *plugins.JobDef, body []byte, query url.Values) (over map[string]string, valid bool) {
	over = map[string]string{}
	if job.Webhook == nil || len(job.Webhook.Params) == 0 {
		return over, true
	}
	given := bodyParams(body)
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
			return nil, false
		}
		over[name] = v
	}
	return over, true
}

// runHook starts the instance for a webhook call and audits it.
func (m *Manager) runHook(id, hookID, ip string, now time.Time, over map[string]string) HookResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.findLocked(id)
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
