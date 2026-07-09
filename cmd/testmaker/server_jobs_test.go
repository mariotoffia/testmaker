package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/clock"
	"github.com/mariotoffia/testmaker/domain/item"
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

// TestAsyncIngestRecordsFailure drives the failed-run branch: an async ingest of
// a catalogued source with no normalizer still 202s (the error is not known
// until the background run), then lands terminal in "failed" with an error
// message — proving runIngestJob records failures on the job, not by crashing.
func TestAsyncIngestRecordsFailure(t *testing.T) {
	ts, _ := newSourcingHarness(t, sourcingSetup{
		sources:  []source.Snapshot{srcSnap("fail-src", false, shared.RedistYes, "A1")},
		fetchers: []ports.Fetcher{llmPayloadFetcher{text: "payload"}},
		// no normalizer registered ⇒ ingest.Ingest returns ErrNoNormalizer
		jobs:          newJobRegistry(clock.System(), 100, nil),
		ingestTimeout: 30 * time.Second,
	})
	res := post(t, ts, "/api/sources/fail-src/ingest", map[string]any{"async": true})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("async ingest → %d, want 202", res.StatusCode)
	}
	var j struct{ ID string }
	decodeBody(t, res, &j)

	var final struct{ State, Error string }
	for i := 0; i < 100; i++ {
		decodeBody(t, get(t, ts, "/api/jobs/"+j.ID), &final)
		if final.State == "done" || final.State == "failed" {
			break
		}
	}
	if final.State != "failed" || final.Error == "" {
		t.Fatalf("want a failed job carrying an error, got %+v", final)
	}
}

// panicNormalizer models a pipeline stage that blows up on bad input, to prove a
// panic in a background async run is contained (net/http already shields the sync
// path, so the async path must not be the weaker sibling and crash the process).
func panicNormalizer(source.Snapshot, []ports.RawItem) ([]item.ItemSpec, error) {
	panic("normalizer boom")
}

// TestAsyncIngestRecoversPanic proves a panicking pipeline turns into a failed
// job, not a dead server process. Without a recover in the runner this test
// crashes the whole test binary — that is exactly the asymmetry it guards.
func TestAsyncIngestRecoversPanic(t *testing.T) {
	ts, _ := newSourcingHarness(t, sourcingSetup{
		sources:       []source.Snapshot{srcSnap("boom-src", false, shared.RedistYes, "A1")},
		fetchers:      []ports.Fetcher{llmPayloadFetcher{text: "payload"}},
		normalizers:   map[source.SourceID]ingest.Normalizer{"boom-src": panicNormalizer},
		jobs:          newJobRegistry(clock.System(), 100, nil),
		ingestTimeout: 30 * time.Second,
	})
	res := post(t, ts, "/api/sources/boom-src/ingest", map[string]any{"async": true})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("async ingest → %d, want 202", res.StatusCode)
	}
	var j struct{ ID string }
	decodeBody(t, res, &j)

	var final struct{ State, Error string }
	for i := 0; i < 100; i++ {
		decodeBody(t, get(t, ts, "/api/jobs/"+j.ID), &final)
		if final.State == "done" || final.State == "failed" {
			break
		}
	}
	if final.State != "failed" || final.Error == "" {
		t.Fatalf("panicking run should land failed with an error, got %+v", final)
	}
}

// TestGetJobUnknownReturns404 proves an id the registry never held 404s with the
// documented code, not a 200 with an empty job.
func TestGetJobUnknownReturns404(t *testing.T) {
	ts, _ := asyncIngestServer(t)
	res := get(t, ts, "/api/jobs/does-not-exist")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown job → %d, want 404", res.StatusCode)
	}
	var body struct{ Code string }
	decodeBody(t, res, &body)
	if body.Code != "server.job_not_found" {
		t.Fatalf("404 body code = %q, want server.job_not_found", body.Code)
	}
}

// TestSyncIngestUnaffectedByJobsRegistry guards against an async-downgrade
// regression: with a jobs registry wired but "async" omitted, the ingest stays
// synchronous (200 + the Report), never a 202.
func TestSyncIngestUnaffectedByJobsRegistry(t *testing.T) {
	ts, sourceID := asyncIngestServer(t)
	res := post(t, ts, "/api/sources/"+sourceID+"/ingest", ingestReq{})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("sync ingest → %d, want 200", res.StatusCode)
	}
	var rep ingest.Report
	decodeBody(t, res, &rep)
	if rep.Saved != 1 {
		t.Fatalf("sync ingest saved %d, want 1", rep.Saved)
	}
}
