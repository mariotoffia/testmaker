package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/clock"
)

func TestRateLimiterRefills(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	l := newRateLimiter(1, 2, clk) // 1 rps, burst 2
	if !l.allow("ip") {
		t.Fatal("first request of a burst-2 bucket must be allowed")
	}
	if !l.allow("ip") {
		t.Fatal("second request of a burst-2 bucket must be allowed")
	}
	if l.allow("ip") {
		t.Fatal("third immediate request must be denied")
	}
	clk.Advance(time.Second) // refill one token
	if !l.allow("ip") {
		t.Fatal("after 1s a token should be available")
	}
	if !l.allow("ip2") { // a different IP has its own full bucket
		t.Fatal("a distinct IP must start with its own full bucket (buckets are per-key)")
	}
	if !l.allow("ip2-fresh") {
		t.Fatal("a fresh IP starts with a full bucket")
	}
}

func TestRateLimitMiddlewareOnlyGatesAPI(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	l := newRateLimiter(1, 1, clk)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := withRateLimit(ok, l)

	// First /api request from an IP passes; second is 429.
	req := func(path string) int {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "1.2.3.4:5555"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	if req("/api/items") != http.StatusOK {
		t.Fatal("first /api request should pass")
	}
	if req("/api/items") != http.StatusTooManyRequests {
		t.Fatal("second /api request should be 429")
	}
	// Non-/api (SPA asset) is never gated, even when the bucket is empty.
	if req("/assets/app.js") != http.StatusOK {
		t.Fatal("SPA asset must not be rate-limited")
	}
}

func TestWithRateLimitNilIsPassthrough(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := withRateLimit(ok, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("nil limiter must pass through, got %d", rec.Code)
	}
}
