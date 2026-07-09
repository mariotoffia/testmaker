package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/adapters/native/llm/fileprompts"
	"github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/app/ingest"
	llmapp "github.com/mariotoffia/testmaker/app/llm"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// fakeLoader seeds the catalogue for a sourcing harness without touching disk.
type fakeLoader struct{ snaps []source.Snapshot }

func (l fakeLoader) Load(context.Context) ([]source.Snapshot, error) { return l.snaps, nil }

// canNormalizer is a fake normalizer that produces one valid item spec regardless
// of input, so the ingest endpoint has a survivor to save without a real parser.
func canNormalizer(_ source.Snapshot, _ []ports.RawItem) ([]item.ItemSpec, error) {
	return []item.ItemSpec{{
		ID:           "fake-item-1",
		Provenance:   item.Provenance{SourceID: "fake-src", Origin: item.OriginFetched, Redistributable: shared.RedistYes},
		TestType:     "A1",
		Stimulus:     []item.StimulusPart{{Text: "stem"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options:      []item.Option{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"}},
		AnswerKey:    item.AnswerKey{OptionID: "a"},
		Difficulty:   item.Difficulty{Band: 1},
	}}, nil
}

// srcSnap builds a minimal catalogue snapshot for the sourcing endpoints.
func srcSnap(id string, gen bool, redist shared.Redistributable, tt ...shared.TestTypeCode) source.Snapshot {
	return source.Snapshot{
		ID:         source.SourceID(id),
		Name:       id,
		Generator:  gen,
		TestTypes:  tt,
		Families:   source.DeriveFamilies(tt),
		License:    source.License{Redistributable: redist},
		Extraction: source.Extraction{Method: "direct-download"},
	}
}

func srcIDs(ss []source.Snapshot) []source.SourceID {
	out := make([]source.SourceID, len(ss))
	for i, s := range ss {
		out[i] = s.ID
	}
	return out
}

// sourcingSetup configures a sourcing harness.
type sourcingSetup struct {
	sources     []source.Snapshot
	fetchers    []ports.Fetcher
	normalizers map[source.SourceID]ingest.Normalizer
	llm         *llmapp.Service
	llmModel    string
}

// newSourcingHarness wires the full delivery surface (including catalogue + ingest)
// over in-memory stores with a seeded catalogue, and returns the running server and
// the backing TestDb so a test can read the bank directly.
func newSourcingHarness(t *testing.T, s sourcingSetup) (*httptest.Server, testDB) {
	t.Helper()
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	blobs, err := openBlobStore("memory")
	if err != nil {
		t.Fatalf("openBlobStore: %v", err)
	}
	cat := catalog.NewService(memorycatalog.NewStore(), fakeLoader{snaps: s.sources})
	if _, err := cat.Sync(context.Background()); err != nil {
		t.Fatalf("catalog sync: %v", err)
	}
	fetchers := s.fetchers
	if fetchers == nil {
		fetchers = []ports.Fetcher{stubfetcher.NewFetcher()}
	}
	ing := ingest.NewService(db.items, fetchers...)
	for id, n := range s.normalizers {
		ing.Register(id, n)
	}
	ts := httptest.NewServer(newServer(serverDeps{
		db: db, blobs: blobs, catalog: cat, ingest: ing, llm: s.llm, llmModel: s.llmModel,
	}).routes())
	t.Cleanup(ts.Close)
	return ts, db
}

func get(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// TestSourcesEndpointListsAndFilters proves GET /sources returns the catalogue and
// honours the generators / testType / redistributable query filters.
func TestSourcesEndpointListsAndFilters(t *testing.T) {
	ts, _ := newSourcingHarness(t, sourcingSetup{sources: []source.Snapshot{
		srcSnap("gen-src", true, shared.RedistYes, "A1"),
		srcSnap("cond-src", false, shared.RedistConditional, "B1"),
		srcSnap("closed-src", false, shared.RedistNo, "C3"),
	}})

	var all []source.Snapshot
	resp := get(t, ts, "/sources")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sources status = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &all)
	if len(all) != 3 {
		t.Fatalf("GET /sources returned %d, want 3", len(all))
	}

	var gens []source.Snapshot
	decode(t, get(t, ts, "/sources?generators=true"), &gens)
	if len(gens) != 1 || gens[0].ID != "gen-src" {
		t.Fatalf("generators filter = %v, want [gen-src]", srcIDs(gens))
	}

	var byType []source.Snapshot
	decode(t, get(t, ts, "/sources?testType=B1"), &byType)
	if len(byType) != 1 || byType[0].ID != "cond-src" {
		t.Fatalf("testType filter = %v, want [cond-src]", srcIDs(byType))
	}

	var byFamily []source.Snapshot
	decode(t, get(t, ts, "/sources?family=logical"), &byFamily)
	if len(byFamily) != 1 || byFamily[0].ID != "gen-src" {
		t.Fatalf("family filter = %v, want [gen-src] (A1 -> logical)", srcIDs(byFamily))
	}

	var reusable []source.Snapshot
	decode(t, get(t, ts, "/sources?redistributable=yes"), &reusable)
	if len(reusable) != 1 || reusable[0].ID != "gen-src" {
		t.Fatalf("redistributable filter = %v, want [gen-src]", srcIDs(reusable))
	}
}

// TestGetSourceEndpoint proves GET /sources/{id} returns one source and 404s an
// unknown id.
func TestGetSourceEndpoint(t *testing.T) {
	ts, _ := newSourcingHarness(t, sourcingSetup{sources: []source.Snapshot{
		srcSnap("s1", false, shared.RedistYes, "A1"),
	}})
	var s source.Snapshot
	resp := get(t, ts, "/sources/s1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sources/s1 = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &s)
	if s.ID != "s1" {
		t.Fatalf("got source %q, want s1", s.ID)
	}
	resp = get(t, ts, "/sources/nope")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown source = %d, want 404", resp.StatusCode)
	}
}

// TestCatalogSyncEndpoint proves POST /catalog/sync reloads the catalogue and
// reports the synced count.
func TestCatalogSyncEndpoint(t *testing.T) {
	ts, _ := newSourcingHarness(t, sourcingSetup{sources: []source.Snapshot{
		srcSnap("s1", false, shared.RedistYes, "A1"),
		srcSnap("s2", true, shared.RedistYes, "A2"),
	}})
	resp := post(t, ts, "/catalog/sync", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /catalog/sync = %d, want 200", resp.StatusCode)
	}
	var out struct{ Synced int }
	decode(t, resp, &out)
	if out.Synced != 2 {
		t.Fatalf("synced = %d, want 2", out.Synced)
	}
}

// TestItemsEndpointListsAndGets proves GET /items queries the bank (filtered) and
// GET /items/{id} returns one item, 404ing an unknown id.
func TestItemsEndpointListsAndGets(t *testing.T) {
	ts, _ := newSourcingHarness(t, sourcingSetup{})
	// Seed two distinct test types so a filter that silently returns everything
	// would be caught (the A2-only bank of the earlier version could not).
	for _, tt := range []string{"A2", "A3"} {
		resp := post(t, ts, "/items/generate", generateReq{TestType: tt, Difficulty: 2, Count: 3, Seed: 1})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("generate %s = %d, want 200", tt, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	var all []item.ItemSnapshot
	decode(t, get(t, ts, "/items"), &all)

	var items []item.ItemSnapshot
	resp := get(t, ts, "/items?testType=A2")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /items = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &items)
	if len(items) == 0 {
		t.Fatalf("GET /items?testType=A2 returned 0 items")
	}
	if len(items) >= len(all) {
		t.Fatalf("testType filter did not narrow: filtered %d vs total %d", len(items), len(all))
	}
	for _, it := range items {
		if it.TestType != "A2" {
			t.Fatalf("testType=A2 filter leaked a %q item (%s)", it.TestType, it.ID)
		}
	}

	var one item.ItemSnapshot
	resp = get(t, ts, "/items/"+string(items[0].ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /items/{id} = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &one)
	if one.ID != items[0].ID {
		t.Fatalf("got item %q, want %q", one.ID, items[0].ID)
	}

	resp = get(t, ts, "/items/does-not-exist")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown item = %d, want 404", resp.StatusCode)
	}
}

// TestIngestEndpointSavesItems proves POST /sources/{id}/ingest runs the
// fetch->normalize->validate->store pipeline and reports what it saved.
func TestIngestEndpointSavesItems(t *testing.T) {
	ts, db := newSourcingHarness(t, sourcingSetup{
		sources:     []source.Snapshot{srcSnap("fake-src", false, shared.RedistYes, "A1")},
		fetchers:    []ports.Fetcher{llmPayloadFetcher{text: "payload"}},
		normalizers: map[source.SourceID]ingest.Normalizer{"fake-src": canNormalizer},
	})
	resp := post(t, ts, "/sources/fake-src/ingest", ingestReq{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST ingest = %d, want 200", resp.StatusCode)
	}
	var rep ingest.Report
	decode(t, resp, &rep)
	if rep.Saved != 1 {
		t.Fatalf("ingest saved %d, want 1 (report %+v)", rep.Saved, rep)
	}
	got, err := db.items.ListItems(context.Background(), item.ItemFilter{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("bank holds %d items, want 1", len(got))
	}
}

// TestIngestEndpointUnknownSource proves an ingest against an uncatalogued id 404s.
func TestIngestEndpointUnknownSource(t *testing.T) {
	ts, _ := newSourcingHarness(t, sourcingSetup{})
	resp := post(t, ts, "/sources/nope/ingest", ingestReq{})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("ingest unknown source = %d, want 404", resp.StatusCode)
	}
}

// TestIngestEndpointNoNormalizer proves a source with a fetcher but no registered
// normalizer surfaces as 501 (unsupported), not a silent success.
func TestIngestEndpointNoNormalizer(t *testing.T) {
	ts, _ := newSourcingHarness(t, sourcingSetup{
		sources:  []source.Snapshot{srcSnap("fake-src", false, shared.RedistYes, "A1")},
		fetchers: []ports.Fetcher{llmPayloadFetcher{text: "payload"}},
	})
	resp := post(t, ts, "/sources/fake-src/ingest", ingestReq{})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("ingest without normalizer = %d, want 501", resp.StatusCode)
	}
}

// TestIngestLLMEndpointNotConfigured proves the LLM ingest endpoint returns 503
// when the deployment has no LLM backend wired.
func TestIngestLLMEndpointNotConfigured(t *testing.T) {
	ts, _ := newSourcingHarness(t, sourcingSetup{sources: []source.Snapshot{
		srcSnap("s1", false, shared.RedistYes, "C3"),
	}})
	resp := post(t, ts, "/sources/s1/ingest-llm", ingestLLMReq{})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ingest-llm without llm = %d, want 503", resp.StatusCode)
	}
}

// TestIngestLLMEndpointExtracts proves the wired LLM ingest endpoint lifts a
// source's payload into validated bank items against a canned backend.
func TestIngestLLMEndpointExtracts(t *testing.T) {
	itemsJSON := `{"items":[{"stem":"What is 2+2?","options":["3","4","5","6"],"answer_index":1,"explanation":"2+2=4","difficulty":2}]}`
	body, err := json.Marshal(map[string]any{
		"model":   "canned",
		"choices": []any{map[string]any{"message": map[string]any{"content": itemsJSON}}},
	})
	if err != nil {
		t.Fatalf("marshal canned response: %v", err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer backend.Close()

	client, err := openaicompat.New(openaicompat.Config{BaseURL: backend.URL})
	if err != nil {
		t.Fatalf("openaicompat.New: %v", err)
	}
	prompts, err := fileprompts.Open("../../data/prompts")
	if err != nil {
		t.Fatalf("open prompts: %v", err)
	}

	// The source is catalogued C3, but the request overrides the tag to B2 — the
	// extracted arithmetic item should carry B2, proving the override flows through.
	ts, db := newSourcingHarness(t, sourcingSetup{
		sources:  []source.Snapshot{srcSnap("llm-src", false, shared.RedistYes, "C3")},
		fetchers: []ports.Fetcher{llmPayloadFetcher{text: "some payload"}},
		llm:      llmapp.NewService(client, prompts),
		llmModel: "canned",
	})
	resp := post(t, ts, "/sources/llm-src/ingest-llm", ingestLLMReq{TestType: "B2"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ingest-llm = %d, want 200", resp.StatusCode)
	}
	var rep ingest.Report
	decode(t, resp, &rep)
	if rep.Saved != 1 {
		t.Fatalf("ingest-llm saved %d, want 1 (report %+v)", rep.Saved, rep)
	}
	got, err := db.items.ListItems(context.Background(), item.ItemFilter{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("bank holds %d items, want 1", len(got))
	}
	if got[0].Provenance.Origin != item.OriginGenerated {
		t.Errorf("origin = %q, want generated (LLM output)", got[0].Provenance.Origin)
	}
	if got[0].TestType != "B2" {
		t.Errorf("test type = %q, want B2 (from the request override, not the source's C3)", got[0].TestType)
	}
}

// TestSourcingEndpointInputValidation covers the custom validation branches: an
// optional (bodyless) ingest body is accepted, malformed JSON and a non-integer
// difficulty are 400, and a normalizer that rejects every spec is a 400
// (ErrAllRejected) rather than a silent empty success.
func TestSourcingEndpointInputValidation(t *testing.T) {
	rejectAll := func(_ source.Snapshot, _ []ports.RawItem) ([]item.ItemSpec, error) {
		// A multiple-choice spec with too few options: item.NewItem rejects it.
		return []item.ItemSpec{{
			ID:           "bad",
			Provenance:   item.Provenance{SourceID: "reject-src", Origin: item.OriginFetched, Redistributable: shared.RedistYes},
			TestType:     "A1",
			Stimulus:     []item.StimulusPart{{Text: "stem"}},
			AnswerFormat: item.FormatMultipleChoice,
			Options:      []item.Option{{ID: "a", Text: "A"}},
			AnswerKey:    item.AnswerKey{OptionID: "a"},
			Difficulty:   item.Difficulty{Band: 1},
		}}, nil
	}
	ts, _ := newSourcingHarness(t, sourcingSetup{
		sources: []source.Snapshot{
			srcSnap("good-src", false, shared.RedistYes, "A1"),
			srcSnap("reject-src", false, shared.RedistYes, "A1"),
		},
		fetchers: []ports.Fetcher{llmPayloadFetcher{text: "payload"}},
		normalizers: map[source.SourceID]ingest.Normalizer{
			"good-src":   canNormalizer,
			"reject-src": rejectAll,
		},
	})

	// A bodyless ingest is valid (the body is optional).
	resp := post(t, ts, "/sources/good-src/ingest", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bodyless ingest = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Malformed JSON body -> 400.
	r, err := http.Post(ts.URL+"/sources/good-src/ingest", "application/json", strings.NewReader("{bad"))
	if err != nil {
		t.Fatalf("post malformed: %v", err)
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed ingest body = %d, want 400", r.StatusCode)
	}

	// A non-integer difficulty query param -> 400.
	resp = get(t, ts, "/items?minDifficulty=abc")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad minDifficulty = %d, want 400", resp.StatusCode)
	}

	// A normalizer that rejects every spec -> 400, not a silent success.
	resp = post(t, ts, "/sources/reject-src/ingest", ingestReq{})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reject-all ingest = %d, want 400 (ErrAllRejected)", resp.StatusCode)
	}
}

// TestServerRealCatalogWiring proves the production sourcing composition (wireSourcing
// over the shipped catalogue file) loads and serves the real catalogue through the
// HTTP surface — the path the faked harness cannot cover.
func TestServerRealCatalogWiring(t *testing.T) {
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	blobs, err := openBlobStore("memory")
	if err != nil {
		t.Fatalf("openBlobStore: %v", err)
	}
	cat, ing, llmSvc, llmModel, err := wireSourcing(db.items, "../../data/catalog/sources.json", "../../data/prompts", LLMConfig{})
	if err != nil {
		t.Fatalf("wireSourcing: %v", err)
	}
	ts := httptest.NewServer(newServer(serverDeps{
		db: db, blobs: blobs, catalog: cat, ingest: ing, llm: llmSvc, llmModel: llmModel,
	}).routes())
	t.Cleanup(ts.Close)

	var sources []source.Snapshot
	resp := get(t, ts, "/sources")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sources = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &sources)
	if len(sources) < 50 {
		t.Fatalf("real catalogue served %d sources, want the full seed set (>=50)", len(sources))
	}
}
