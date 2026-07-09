package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// blockingFetcher parks the FIRST fetch until release is closed (signalling
// entered as it parks); every later fetch returns immediately. That lets a test
// hold the single ingest slot with one in-flight request and observe that a
// second concurrent ingest is refused. Without the semaphore the second fetch
// would instead run to completion (200), so the 429 assertion has real teeth.
type blockingFetcher struct {
	entered chan struct{}
	release chan struct{}
	first   sync.Once
}

func (f *blockingFetcher) Supports(source.Snapshot) bool { return true }

func (f *blockingFetcher) Fetch(context.Context, ports.FetchRequest) (ports.FetchResult, error) {
	blocked := false
	f.first.Do(func() { blocked = true })
	if blocked {
		close(f.entered)
		<-f.release
	}
	return ports.FetchResult{Items: []ports.RawItem{{Stem: "payload"}}}, nil
}

// TestIngestSemaphoreRejectsConcurrent proves maxIngest:1 lets one ingest run at
// a time: while the first is parked in its fetch, a second is refused 429
// limit.ingest, and once the first completes the freed slot admits a third.
func TestIngestSemaphoreRejectsConcurrent(t *testing.T) {
	bf := &blockingFetcher{entered: make(chan struct{}), release: make(chan struct{})}
	ts, _ := newSourcingHarness(t, sourcingSetup{
		sources:     []source.Snapshot{srcSnap("fake-src", false, shared.RedistYes, "A1")},
		fetchers:    []ports.Fetcher{bf},
		normalizers: map[source.SourceID]ingest.Normalizer{"fake-src": canNormalizer},
		maxIngest:   1,
	})
	// Always free the parked fetch, even if an assertion fails first: otherwise
	// the in-flight request wedges httptest.Server.Close() in t.Cleanup forever.
	release := sync.OnceFunc(func() { close(bf.release) })
	defer release()

	// First ingest acquires the only slot and parks inside the fetcher. Inline
	// http.Post (not the t-taking helper) keeps *testing.T off this goroutine.
	first := make(chan int, 1)
	go func() {
		r, err := http.Post(ts.URL+"/api/sources/fake-src/ingest", "application/json", strings.NewReader("{}"))
		if err != nil {
			first <- -1
			return
		}
		_ = r.Body.Close()
		first <- r.StatusCode
	}()
	<-bf.entered // the slot is now held by the first ingest

	// A second ingest while the slot is held must be refused, not queued.
	resp := post(t, ts, "/api/sources/fake-src/ingest", ingestReq{})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("concurrent ingest = %d, want 429", resp.StatusCode)
	}
	var body struct{ Code string }
	decode(t, resp, &body)
	if body.Code != "limit.ingest" {
		t.Fatalf("concurrent ingest code = %q, want limit.ingest", body.Code)
	}

	// Release the first; it completes 200 and returns the slot.
	release()
	if code := <-first; code != http.StatusOK {
		t.Fatalf("first ingest = %d, want 200", code)
	}

	// A third ingest now succeeds — the slot was released.
	resp = post(t, ts, "/api/sources/fake-src/ingest", ingestReq{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post-release ingest = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
