package main

import (
	"net/http"
	"strings"
	"sync"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// maxRateBuckets caps the per-IP bucket map. ponytail: a global lock + a lazy
// sweep of idle (refilled-to-full) buckets when the cap is hit — fine for a
// single node; a sharded map or an LRU is the upgrade if IP cardinality ever
// makes the lock hot.
const maxRateBuckets = 4096

type tokenBucket struct {
	tokens float64
	last   int64 // unix nanos of the last refill
}

// rateLimiter is a per-key token bucket. The clock is injected so refill is
// deterministic under test (the same discipline as the rest of the surface).
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64 // tokens per second
	burst   float64 // bucket capacity
	clk     clock.Clock
}

func newRateLimiter(rps float64, burst int, clk clock.Clock) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    rps,
		burst:   float64(burst),
		clk:     clk,
	}
}

// allow consumes one token for key, refilling by elapsed time first. It returns
// false when the bucket is empty (caller should 429).
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clk.Now().UnixNano()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= maxRateBuckets {
			l.sweepIdleLocked()
		}
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	elapsed := float64(now-b.last) / 1e9
	b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweepIdleLocked drops buckets that have refilled to full (nobody is using
// them), reclaiming space without evicting an active limiter. Caller holds mu.
func (l *rateLimiter) sweepIdleLocked() {
	for k, b := range l.buckets {
		if b.tokens >= l.burst {
			delete(l.buckets, k)
		}
	}
}

// withRateLimit gates /api* by client IP; a nil limiter (unconfigured, e.g.
// tests) is a pass-through. SPA assets and non-/api paths are never limited.
func withRateLimit(next http.Handler, l *rateLimiter) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") && !l.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			writeAuthError(w, http.StatusTooManyRequests, "limit.rate", "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
