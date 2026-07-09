package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/source"
)

// validCatalogBody is one catalogue record that passes source.NewSource: real
// snake_case wire schema, every closed-set enum a valid value.
const validCatalogBody = `{"sources":[{"id":"s1","name":"S1","urls":["https://x"],"access_class":["dataset-repo"],` +
	`"license":{"category":"public-domain","redistributable":"yes"},"test_types":["A2"],` +
	`"answer_keys":"yes","norms_difficulty":"no","priority":"high","ip_risk":"low","category":"open-data"}]}`

// serverWithCatalogPath wires a zero-auth delivery surface whose catalogue loads
// from catPath (a real filecatalog loader) and whose catalogPath is set, so the
// upload handler can validate → write → sync end to end. The catalogue is NOT
// synced at setup (catPath may not exist yet); the first upload populates it.
func serverWithCatalogPath(t *testing.T, catPath string) *httptest.Server {
	t.Helper()
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	blobs, err := openBlobStore("memory")
	if err != nil {
		t.Fatalf("openBlobStore: %v", err)
	}
	cat := catalog.NewService(memorycatalog.NewStore(), filecatalog.NewLoader(catPath))
	ing := ingest.NewService(db.items, stubfetcher.NewFetcher())
	ts := httptest.NewServer(newServer(serverDeps{
		db: db, blobs: blobs, catalog: cat, ingest: ing, catalogPath: catPath,
	}).routes())
	t.Cleanup(ts.Close)
	return ts
}

func postRaw(t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// sourcesTotal decodes GET /api/sources and returns the envelope Total.
func sourcesTotal(t *testing.T, ts *httptest.Server) int {
	t.Helper()
	var page pageEnvelope[source.Snapshot]
	decode(t, get(t, ts, "/api/sources"), &page)
	return page.Total
}

// TestUploadCatalogValidatesWritesAndSyncs proves POST /api/catalog validates the
// body, writes it atomically to the configured path, and reloads it — and that an
// invalid body is a 400 that never touches the served catalogue.
func TestUploadCatalogValidatesWritesAndSyncs(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "sources.json")
	ts := serverWithCatalogPath(t, catPath)

	res := postRaw(t, ts.URL+"/api/catalog", []byte(validCatalogBody))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("upload valid = %d, want 200", res.StatusCode)
	}
	var out struct{ Synced int }
	decode(t, res, &out)
	if out.Synced != 1 {
		t.Fatalf("synced = %d, want 1", out.Synced)
	}
	// The file was written…
	if _, err := os.Stat(catPath); err != nil {
		t.Fatalf("catalogue not written: %v", err)
	}
	// …and the catalogue is now queryable through the surface.
	if got := sourcesTotal(t, ts); got != 1 {
		t.Fatalf("served total = %d, want 1", got)
	}

	// An invalid body is a 400 and does NOT overwrite the served catalogue.
	bad := postRaw(t, ts.URL+"/api/catalog", []byte(`{"sources":[{"name":"no id"}]}`))
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("upload invalid = %d, want 400", bad.StatusCode)
	}
	_ = bad.Body.Close()
	if got := sourcesTotal(t, ts); got != 1 {
		t.Fatalf("invalid upload changed the served catalogue (total = %d, want 1)", got)
	}
}

// TestUploadCatalogNoPathConfigured proves the endpoint is a clean error (not a
// panic or a nil-path write) when no catalogue path is configured.
func TestUploadCatalogNoPathConfigured(t *testing.T) {
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	blobs, err := openBlobStore("memory")
	if err != nil {
		t.Fatalf("openBlobStore: %v", err)
	}
	cat := catalog.NewService(memorycatalog.NewStore(), fakeLoader{})
	if _, err := cat.Sync(context.Background()); err != nil {
		t.Fatalf("catalog sync: %v", err)
	}
	ing := ingest.NewService(db.items, stubfetcher.NewFetcher())
	ts := httptest.NewServer(newServer(serverDeps{db: db, blobs: blobs, catalog: cat, ingest: ing}).routes())
	t.Cleanup(ts.Close)

	res := postRaw(t, ts.URL+"/api/catalog", []byte(validCatalogBody))
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotImplemented {
		t.Fatalf("upload with no catalogPath = %d, want 501 (unsupported)", res.StatusCode)
	}
}
