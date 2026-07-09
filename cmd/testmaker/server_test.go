package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/scoring"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

// newHarness wires the delivery surface over the dependency-free in-memory
// TestDb and blob store and returns a running httptest server.
func newHarness(t *testing.T) *httptest.Server {
	t.Helper()
	ts, _, _ := newHarnessWithStores(t)
	return ts
}

// newHarnessWithStores is newHarness that also hands back the backing TestDb and
// blob store, so a white-box test can read an item's offloaded media ref and
// resolve it through the /media endpoint.
func newHarnessWithStores(t *testing.T) (*httptest.Server, testDB, ports.BlobStore) {
	t.Helper()
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
	return ts, db, blobs
}

func post(t *testing.T, ts *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode %s body: %v", path, err)
		}
	}
	resp, err := http.Post(ts.URL+path, "application/json", &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// seedAndCompose generates a small logical batch into the bank and composes a
// fixed-increasing test from it, returning the composed test id and its total
// item count.
func seedAndCompose(t *testing.T, ts *httptest.Server, id string) (string, int) {
	t.Helper()
	resp := post(t, ts, "/api/items/generate", generateReq{TestType: "A2", Difficulty: 2, Count: 5, Seed: 1})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generate status = %d, want 200", resp.StatusCode)
	}
	var rep struct{ Generated, Saved int }
	decode(t, resp, &rep)
	if rep.Saved == 0 {
		t.Fatalf("generate saved 0 items")
	}

	resp = post(t, ts, "/api/tests", composeReq{
		ID:             id,
		Title:          "Server Flow",
		Policy:         "fixed-increasing",
		TotalSeconds:   600,
		PerItemSeconds: 60,
		Sections:       []sectionReq{{Title: "Logical", Family: "logical", TotalSeconds: 300, PerItemSeconds: 60}},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("compose status = %d, want 201", resp.StatusCode)
	}
	var test testset.TestSnapshot
	decode(t, resp, &test)
	items := 0
	for _, sec := range test.Sections {
		items += len(sec.Items)
	}
	if items == 0 {
		t.Fatalf("composed test %q has no items", id)
	}
	return string(test.ID), items
}

// TestDeliverySurfaceFullFlow drives the whole author -> take -> score path
// through the HTTP surface and asserts each step succeeds and the session ends
// completed and scorable.
func TestDeliverySurfaceFullFlow(t *testing.T) {
	ts := newHarness(t)
	testID, _ := seedAndCompose(t, ts, "srv-fixed")

	// Start the session; the opening delivery presents the first item.
	resp := post(t, ts, "/api/tests/"+testID+"/sessions", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start status = %d, want 201", resp.StatusCode)
	}
	var d ports.Delivery
	decode(t, resp, &d)
	sessID := string(d.Session.ID)
	if sessID == "" {
		t.Fatalf("start returned no session id")
	}

	// Answer each presented item until the plan is exhausted.
	for step := 0; d.Session.Presented.ItemID != ""; step++ {
		if step > 100 {
			t.Fatalf("answer loop did not terminate")
		}
		itemID := d.Session.Presented.ItemID
		resp = post(t, ts, "/api/sessions/"+sessID+"/answers", answerReq{ItemID: itemID, OptionID: "a"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("answer status = %d, want 200", resp.StatusCode)
		}
		d = ports.Delivery{}
		decode(t, resp, &d)
	}

	// Complete the session.
	resp = post(t, ts, "/api/sessions/"+sessID+"/complete", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete status = %d, want 200", resp.StatusCode)
	}
	var final session.SessionSnapshot
	decode(t, resp, &final)
	if final.State != session.StateCompleted {
		t.Fatalf("final state = %q, want completed", final.State)
	}

	// Score it.
	resp, err := http.Get(ts.URL + "/api/sessions/" + sessID + "/score")
	if err != nil {
		t.Fatalf("GET score: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("score status = %d, want 200", resp.StatusCode)
	}
	var score scoring.Score
	decode(t, resp, &score)
	if score.Max == 0 {
		t.Fatalf("score has no scored responses (Max=0)")
	}
}

// TestDeliverySurfaceConcurrentAnswersRecordOnce fires many concurrent answers
// at the same presented item and asserts the end-to-end invariant: exactly one
// answer is recorded (one 2xx); the rest are client errors (409 conflict, or 400
// once the session has advanced past the item), never a second success or a 5xx.
// Note this proves the *surface* is safe, not the CAS mechanism specifically:
// the load->save window here is so tight that a loser is usually rejected by the
// executor (its item is no longer presented -> 400) before the store's CAS even
// sees a same-version write. The deterministic, contended proof of the CAS
// itself lives at the store contract (ports/testdbtest
// ConcurrentSaveAtSameVersionRecordsOnce, run for both adapters under -race).
func TestDeliverySurfaceConcurrentAnswersRecordOnce(t *testing.T) {
	ts := newHarness(t)
	testID, _ := seedAndCompose(t, ts, "srv-conc")

	resp := post(t, ts, "/api/tests/"+testID+"/sessions", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start status = %d, want 201", resp.StatusCode)
	}
	var d ports.Delivery
	decode(t, resp, &d)
	sessID := string(d.Session.ID)
	itemID := d.Session.Presented.ItemID
	if itemID == "" {
		t.Fatalf("start presented no item to answer")
	}

	const n = 8
	statuses := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var buf bytes.Buffer
			_ = json.NewEncoder(&buf).Encode(answerReq{ItemID: itemID, OptionID: "a"})
			r, err := http.Post(ts.URL+"/api/sessions/"+sessID+"/answers", "application/json", &buf)
			if err != nil {
				statuses[i] = -1
				return
			}
			_ = r.Body.Close()
			statuses[i] = r.StatusCode
		}(i)
	}
	wg.Wait()

	success := 0
	for _, s := range statuses {
		switch {
		case s >= 200 && s < 300:
			success++
		case s >= 400 && s < 500:
			// expected loser: conflict, or the item is no longer presented
		default:
			t.Errorf("unexpected status %d (want one 2xx, rest 4xx)", s)
		}
	}
	if success != 1 {
		t.Fatalf("recorded %d successful answers, want exactly 1 under optimistic concurrency", success)
	}
}

// TestRootIndex proves GET / returns a 200 API index (so hitting the server root
// confirms it is up and lists the endpoints) while an unknown path still 404s —
// i.e. the SPA catch-all serves the JSON index at "/" but does not swallow every
// unmatched request into a 200 (ADR-0005: no UI build in tests, so handleSPA
// degrades to the index at "/" and JSON 404 elsewhere).
func TestRootIndex(t *testing.T) {
	ts := newHarness(t)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", resp.StatusCode)
	}
	var idx struct {
		Service   string
		Endpoints []string
	}
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if idx.Service != "testmaker" || len(idx.Endpoints) == 0 {
		t.Fatalf("index = %+v, want a service name and a non-empty endpoint list", idx)
	}

	// An unknown non-/api path must still be a 404: the GET / catch-all delegates
	// to handleSPA, which (with no UI build) returns the index only for "/" and a
	// JSON 404 for anything else — it must not turn every unmatched GET into a 200.
	other, err := http.Get(ts.URL + "/does-not-exist")
	if err != nil {
		t.Fatalf("GET unknown: %v", err)
	}
	_ = other.Body.Close()
	if other.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /does-not-exist = %d, want 404 (root must not catch-all)", other.StatusCode)
	}
}

// TestDeliverySurfaceErrors covers the error->status translation: malformed
// input is a 400 and an unknown test is a 404.
func TestDeliverySurfaceErrors(t *testing.T) {
	ts := newHarness(t)

	resp, err := http.Post(ts.URL+"/api/tests", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatalf("POST malformed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d, want 400", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/api/tests/does-not-exist")
	if err != nil {
		t.Fatalf("GET unknown test: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown test status = %d, want 404", resp.StatusCode)
	}
}

// TestMediaEndpointRoundTrip proves the renderer resolves a figural item's media
// through the blob port: generating A2 (matrix) items offloads their inline SVG
// data URIs to the store as content refs, and GET /media/{ref} returns those
// bytes with the stored content type. An unknown ref is a 404.
func TestMediaEndpointRoundTrip(t *testing.T) {
	ts, db, _ := newHarnessWithStores(t)

	resp := post(t, ts, "/api/items/generate", generateReq{TestType: "A2", Difficulty: 2, Count: 3, Seed: 1})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generate status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	items, err := db.items.ListItems(context.Background(), item.ItemFilter{TestTypes: []shared.TestTypeCode{"A2"}})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	ref := firstMediaRef(items)
	if ref == "" {
		t.Fatal("no offloaded media ref found on generated A2 items")
	}
	if strings.HasPrefix(ref, "data:") {
		t.Fatalf("media ref was not offloaded to the blob store: %q", ref)
	}

	mresp, err := http.Get(ts.URL + "/api/media/" + ref)
	if err != nil {
		t.Fatalf("GET media: %v", err)
	}
	defer func() { _ = mresp.Body.Close() }()
	if mresp.StatusCode != http.StatusOK {
		t.Fatalf("media status = %d, want 200", mresp.StatusCode)
	}
	if ct := mresp.Header.Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("media content type = %q, want image/svg+xml", ct)
	}
	// SVG is script-executable, so the media endpoint must pin the type and
	// sandbox it — otherwise stored media is an XSS vector on the assessment origin.
	if got := mresp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := mresp.Header.Get("Content-Security-Policy"); got == "" {
		t.Fatal("media response has no Content-Security-Policy")
	}
	body, err := io.ReadAll(mresp.Body)
	if err != nil {
		t.Fatalf("read media: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("media body is empty")
	}

	uresp, err := http.Get(ts.URL + "/api/media/deadbeefunknownref")
	if err != nil {
		t.Fatalf("GET unknown media: %v", err)
	}
	_ = uresp.Body.Close()
	if uresp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown media status = %d, want 404", uresp.StatusCode)
	}
}

// firstMediaRef returns the first non-empty media ref across the items' stimulus
// and option parts, or "" when none carry media.
func firstMediaRef(items []item.ItemSnapshot) string {
	for _, it := range items {
		for _, p := range it.Stimulus {
			if p.MediaRef != "" {
				return p.MediaRef
			}
		}
		for _, o := range it.Options {
			if o.MediaRef != "" {
				return o.MediaRef
			}
		}
	}
	return ""
}
