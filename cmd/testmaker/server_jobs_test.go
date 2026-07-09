package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/clock"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// asyncIngestServer wires a sourcing harness whose "fake-src" ingests
// deterministically (in-process fetcher + always-valid normalizer, as
// server_sourcing_test.go's sync ingest test uses), plus a jobs registry and a
// real timeout so the async 202 path has somewhere to record its outcome.
func asyncIngestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	ts, _ := newSourcingHarness(t, sourcingSetup{
		sources:       []source.Snapshot{srcSnap("fake-src", false, shared.RedistYes, "A1")},
		fetchers:      []ports.Fetcher{llmPayloadFetcher{text: "payload"}},
		normalizers:   map[source.SourceID]ingest.Normalizer{"fake-src": canNormalizer},
		jobs:          newJobRegistry(clock.System(), 100, nil),
		ingestTimeout: 30 * time.Second,
	})
	return ts, "fake-src"
}

// TestAsyncIngestReturns202ThenCompletes drives the async ingest envelope:
// "async": true → 202 + a queued/running job, then GET /api/jobs/{id} polled to
// terminal, then GET /api/jobs lists it. The fetcher is synchronous, so the
// bounded poll loop converges without sleeping.
func TestAsyncIngestReturns202ThenCompletes(t *testing.T) {
	ts, sourceID := asyncIngestServer(t)

	res := post(t, ts, "/api/sources/"+sourceID+"/ingest", map[string]any{"async": true})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("async ingest → %d, want 202", res.StatusCode)
	}
	var j struct {
		ID    string
		State string
	}
	decodeBody(t, res, &j)
	if j.ID == "" || (j.State != "queued" && j.State != "running") {
		t.Fatalf("202 job = %+v", j)
	}

	// Poll GET /api/jobs/{id} until terminal (bounded loop; the fake fetcher is
	// synchronous so this converges immediately — no sleep, just retries).
	var final struct {
		State  string
		Report *struct{ Saved int }
	}
	for i := 0; i < 100; i++ {
		decodeBody(t, get(t, ts, "/api/jobs/"+j.ID), &final)
		if final.State == "done" || final.State == "failed" {
			break
		}
	}
	if final.State != "done" {
		t.Fatalf("job did not complete: state=%s", final.State)
	}
	if final.Report == nil || final.Report.Saved != 1 {
		t.Fatalf("done job report = %+v, want Saved=1", final.Report)
	}

	// GET /api/jobs lists it.
	var page struct{ Total int }
	decodeBody(t, get(t, ts, "/api/jobs"), &page)
	if page.Total < 1 {
		t.Fatal("job list empty after a run")
	}
}
