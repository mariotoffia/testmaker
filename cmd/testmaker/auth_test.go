package main

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/clock"
)

func tokenAuth(t *testing.T, clk clock.Clock) *authenticator {
	t.Helper()
	return newAuthenticator(AuthConfig{
		Mode: "token", OperatorToken: "op-secret-token",
		Secret: "hmac-signing-secret", InviteTTLSeconds: 3600,
	}, clk)
}

func TestOperatorTokenVerify(t *testing.T) {
	a := tokenAuth(t, clock.System())
	if !a.verifyOperator("op-secret-token") {
		t.Fatal("correct operator token rejected")
	}
	if a.verifyOperator("wrong") || a.verifyOperator("") {
		t.Fatal("wrong/empty operator token accepted")
	}
}

func TestInviteRoundTripAndExpiry(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	a := tokenAuth(t, clk)
	tok, exp, err := a.mintInvite("test-1", 0) // 0 → config TTL (1h)
	if err != nil {
		t.Fatalf("mintInvite: %v", err)
	}
	if !exp.After(clk.Now()) {
		t.Fatal("expiry must be in the future")
	}
	tid, ok := a.verifyInvite(tok)
	if !ok || tid != "test-1" {
		t.Fatalf("verifyInvite = (%q, %v), want (test-1, true)", tid, ok)
	}
	clk.Advance(2 * time.Hour) // past the 1h TTL
	if _, ok := a.verifyInvite(tok); ok {
		t.Fatal("expired invite accepted")
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	a := tokenAuth(t, clock.System())
	tok, _, _ := a.mintInvite("test-1", time.Hour)
	bad := tok[:len(tok)-1] + "X" // flip the last sig char
	if _, ok := a.verifyInvite(bad); ok {
		t.Fatal("tampered signature accepted")
	}
	// A session token must not verify as an invite (prefix guard).
	stok, _ := a.mintSession("s-1")
	if _, ok := a.verifyInvite(stok); ok {
		t.Fatal("session token accepted as invite")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	a := tokenAuth(t, clock.System())
	tok, err := a.mintSession("sess-42")
	if err != nil {
		t.Fatal(err)
	}
	sid, ok := a.verifySession(tok)
	if !ok || sid != "sess-42" {
		t.Fatalf("verifySession = (%q, %v), want (sess-42, true)", sid, ok)
	}
}

func TestMintInviteRequiresSecret(t *testing.T) {
	a := newAuthenticator(AuthConfig{Mode: "none"}, clock.System())
	if a.enforced() {
		t.Fatal("none mode must not be enforced")
	}
	if _, _, err := a.mintInvite("t", time.Hour); err == nil {
		t.Fatal("mintInvite with no secret must error (invites need token mode)")
	}
}

func TestBearerExtraction(t *testing.T) {
	r := httptest.NewRequest("GET", "/api", nil)
	r.Header.Set("Authorization", "Bearer abc.def.ghi")
	if got := bearer(r); got != "abc.def.ghi" {
		t.Fatalf("bearer = %q", got)
	}
	r2 := httptest.NewRequest("GET", "/api", nil)
	r2.Header.Set("Authorization", "Basic xyz")
	if got := bearer(r2); got != "" {
		t.Fatalf("non-bearer scheme leaked: %q", got)
	}
}
