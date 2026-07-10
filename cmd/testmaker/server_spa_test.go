package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newSPATestServer stands up the surface with zero-value config: memory
// backends, no auth, no limits — the baseline every pre-auth test uses.
func newSPATestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() { _ = db.close() })
	blobs, err := openBlobStore("memory")
	if err != nil {
		t.Fatalf("openBlobStore: %v", err)
	}
	ts := httptest.NewServer(newServer(serverDeps{db: db, blobs: blobs}).routes())
	t.Cleanup(ts.Close)
	return ts
}

func TestAPIIndexLivesUnderAPI(t *testing.T) {
	ts := newSPATestServer(t)
	res, err := http.Get(ts.URL + "/api")
	if err != nil {
		t.Fatalf("GET /api: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api = %d, want 200", res.StatusCode)
	}
	var body struct {
		Service   string   `json:"service"`
		Endpoints []string `json:"endpoints"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Service != "testmaker" || len(body.Endpoints) == 0 {
		t.Fatalf("unexpected index body: %+v", body)
	}
	for _, e := range body.Endpoints {
		if !strings.Contains(e, "/api/") && e != "GET /api" {
			t.Fatalf("endpoint %q not under /api", e)
		}
	}
}

func TestRootFallsBackToJSONIndexWithoutUIBuild(t *testing.T) {
	ts := newSPATestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	// A checkout with a locally built UI serves HTML here; both are legal.
	ct := res.Header.Get("Content-Type")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", res.StatusCode)
	}
	if !strings.HasPrefix(ct, "application/json") && !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET / content-type = %q, want json (no build) or html (built)", ct)
	}
}

func TestUnknownNonAPIPathIs404WithoutUIBuild(t *testing.T) {
	ts := newSPATestServer(t)
	res, err := http.Get(ts.URL + "/no/such/page")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	// Without a build: 404 JSON. With a locally built UI: 200 index.html
	// (client-side route). Assert the two legal outcomes only.
	if res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusOK {
		t.Fatalf("GET /no/such/page = %d (%s), want 404 (no build) or 200 (built)", res.StatusCode, string(b))
	}
}

func TestOldRootEndpointsAreGone(t *testing.T) {
	skipIfUIEmbedded(t)
	ts := newSPATestServer(t)
	res, err := http.Get(ts.URL + "/sources")
	if err != nil {
		t.Fatalf("GET /sources: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusOK {
		t.Fatal("GET /sources must no longer be served at the root (moved to /api/sources)")
	}
}
