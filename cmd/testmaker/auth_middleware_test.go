package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// authTestServer builds a token-mode surface (memory backends) so role checks
// are live. Returns the server + the operator token for Authorization headers.
func authTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.close() })
	blobs, _ := openBlobStore("memory")
	cfg := AuthConfig{Mode: "token", OperatorToken: "op-tok", Secret: "sec", InviteTTLSeconds: 3600}
	ts := httptest.NewServer(newServer(serverDeps{
		db: db, blobs: blobs, authCfg: cfg, clock: clock.System(),
	}).routes())
	t.Cleanup(ts.Close)
	return ts, "op-tok"
}

// authGet issues a GET with an optional bearer token (full URL, not path — the
// existing get(t, ts, path) helper is server-relative and token-blind).
func authGet(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return res
}

// decodeBody decodes a JSON response body into dst and closes it.
func decodeBody(t *testing.T, res *http.Response, dst any) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

func TestOperatorEndpointRequiresToken(t *testing.T) {
	ts, op := authTestServer(t)
	if res := authGet(t, ts.URL+"/api/items", ""); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token → %d, want 401", res.StatusCode)
	}
	if res := authGet(t, ts.URL+"/api/items", "wrong"); res.StatusCode != http.StatusForbidden {
		t.Fatalf("bad token → %d, want 403", res.StatusCode)
	}
	if res := authGet(t, ts.URL+"/api/items", op); res.StatusCode != http.StatusOK {
		t.Fatalf("operator token → %d, want 200", res.StatusCode)
	}
}

func TestWhoami(t *testing.T) {
	ts, op := authTestServer(t)
	if res := authGet(t, ts.URL+"/api/auth/whoami", ""); res.StatusCode != http.StatusOK {
		t.Fatalf("whoami anon → %d, want 200", res.StatusCode)
	}
	res := authGet(t, ts.URL+"/api/auth/whoami", op)
	var body struct{ Role, Mode string }
	decodeBody(t, res, &body)
	if body.Role != "operator" || body.Mode != "token" {
		t.Fatalf("whoami(operator) = %+v", body)
	}
}

func TestPublicEndpointsNeedNoToken(t *testing.T) {
	ts, _ := authTestServer(t)
	if res := authGet(t, ts.URL+"/api", ""); res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api anon → %d, want 200 (public)", res.StatusCode)
	}
}

func TestNoneModeDisablesEnforcement(t *testing.T) {
	db, _ := openTestDB("memory")
	t.Cleanup(func() { _ = db.close() })
	blobs, _ := openBlobStore("memory")
	ts := httptest.NewServer(newServer(serverDeps{
		db: db, blobs: blobs, authCfg: AuthConfig{Mode: "none"},
	}).routes())
	t.Cleanup(ts.Close)
	if res := authGet(t, ts.URL+"/api/items", ""); res.StatusCode != http.StatusOK {
		t.Fatalf("none mode, no token → %d, want 200", res.StatusCode)
	}
}
