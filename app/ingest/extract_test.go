package ingest_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/app/llm"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/prompt"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// fakeLLM is an in-process ports.LLM backend: it returns a canned completion and
// captures the last request so a test can assert what the extraction step sent.
type fakeLLM struct {
	content string
	err     error
	last    ports.LLMRequest
}

func (f *fakeLLM) Generate(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	f.last = req
	if f.err != nil {
		return ports.LLMResponse{}, f.err
	}
	return ports.LLMResponse{Content: f.content, Model: req.Model}, nil
}

// fakePrompts is a one-prompt ports.PromptRepository serving the extraction
// prompt for ByPurpose; the other methods are unused by the extraction step.
type fakePrompts struct {
	snap prompt.Snapshot
	err  error
}

func (f *fakePrompts) ByPurpose(context.Context, prompt.Purpose) (prompt.Snapshot, error) {
	return f.snap, f.err
}
func (f *fakePrompts) Get(context.Context, prompt.PromptID) (prompt.Snapshot, error) {
	return prompt.Snapshot{}, prompt.ErrUnknownPrompt
}
func (f *fakePrompts) List(context.Context) ([]prompt.Snapshot, error) { return nil, nil }
func (f *fakePrompts) Put(context.Context, prompt.Snapshot) error      { return nil }
func (f *fakePrompts) Delete(context.Context, prompt.PromptID) error   { return nil }

var (
	_ ports.LLM              = (*fakeLLM)(nil)
	_ ports.PromptRepository = (*fakePrompts)(nil)
)

// extractionPrompt returns a valid extraction-purpose prompt snapshot.
func extractionPrompt() prompt.Snapshot {
	return prompt.MustPrompt(prompt.Spec{
		ID: "extract-items", Version: 1, Purpose: prompt.PurposeExtraction,
		Template: "extract from {{.source}} for {{.testType}}",
		Params:   []string{"source", "testType"},
	}).Snapshot()
}

// llmSnap returns a snapshot whose single fetched artifact carries text.
func llmSnap() source.Snapshot {
	return source.Snapshot{
		ID:      "src-1",
		Name:    "Test Source",
		License: source.License{Redistributable: shared.RedistYes},
	}
}

// payloadFetcher supports every source and returns one text artifact.
type payloadFetcher struct{ note string }

func (payloadFetcher) Supports(source.Snapshot) bool { return true }
func (f payloadFetcher) Fetch(context.Context, ports.FetchRequest) (ports.FetchResult, error) {
	return ports.FetchResult{
		SourceID: "src-1",
		Items:    []ports.RawItem{{Stem: "some unstructured payload with an item"}},
		Note:     f.note,
	}, nil
}

func llmService(t *testing.T, backend *fakeLLM) *llm.Service {
	t.Helper()
	return llm.NewService(backend, &fakePrompts{snap: extractionPrompt()})
}

const twoGoodItems = `{"items":[
  {"stem":"1+1?","options":["1","2","3","4"],"answer_index":1,"explanation":"two","difficulty":2},
  {"stem":"2+2?","options":["3","4","5","6"],"answer_index":1,"explanation":"four","difficulty":9}
]}`

func TestIngestLLMLiftsAndValidates(t *testing.T) {
	backend := &fakeLLM{content: twoGoodItems}
	bank := &fakeBank{}
	svc := ingest.NewService(bank, payloadFetcher{})

	rep, err := svc.IngestLLM(context.Background(), ingest.LLMExtractRequest{
		Source:   llmSnap(),
		LLM:      llmService(t, backend),
		TestType: "C3",
		Model:    "test-model",
	})
	if err != nil {
		t.Fatalf("IngestLLM: %v", err)
	}
	if rep.Fetched != 1 || rep.Normalized != 2 || rep.Saved != 2 || rep.Skipped != 0 {
		t.Errorf("report = %+v, want fetched=1 normalized=2 saved=2 skipped=0", rep)
	}
	if len(bank.saved) != 2 {
		t.Fatalf("bank stored %d items, want 2", len(bank.saved))
	}
	// Extracted items are provenance-tagged generated, inheriting the source's
	// redistributability, and difficulty out of range (9) is clamped to the band.
	got := bank.saved[0]
	if got.Provenance.Origin != item.OriginGenerated {
		t.Errorf("origin = %q, want generated", got.Provenance.Origin)
	}
	if got.Provenance.SourceID != "src-1" || got.Provenance.Redistributable != shared.RedistYes {
		t.Errorf("provenance = %+v", got.Provenance)
	}
	if bank.saved[1].Difficulty.Band != 3 {
		t.Errorf("out-of-range difficulty band = %d, want clamped to 3", bank.saved[1].Difficulty.Band)
	}
	// The fetched payload must reach the backend as a user message under the schema.
	if backend.last.JSONSchema == "" {
		t.Error("extraction request carried no JSON schema")
	}
	if !userMessageContains(backend.last, "unstructured payload") {
		t.Errorf("payload not sent as user message: %+v", backend.last.Messages)
	}
	if !strings.Contains(rep.Note, "model=test-model") || !strings.Contains(rep.Note, "prompt=extract-items") {
		t.Errorf("note lacks provenance: %q", rep.Note)
	}
}

func TestIngestLLMMalformedOutputIsParseError(t *testing.T) {
	backend := &fakeLLM{content: "not json at all"}
	svc := ingest.NewService(&fakeBank{}, payloadFetcher{})

	_, err := svc.IngestLLM(context.Background(), ingest.LLMExtractRequest{
		Source: llmSnap(), LLM: llmService(t, backend), TestType: "C3",
	})
	if !errors.Is(err, ingest.ErrExtractParse) {
		t.Fatalf("err = %v, want ErrExtractParse", err)
	}
}

func TestIngestLLMAllRejected(t *testing.T) {
	// Every candidate has too few options, so item.NewItem rejects all of them.
	backend := &fakeLLM{content: `{"items":[{"stem":"q","options":["a","b"],"answer_index":0}]}`}
	svc := ingest.NewService(&fakeBank{}, payloadFetcher{})

	_, err := svc.IngestLLM(context.Background(), ingest.LLMExtractRequest{
		Source: llmSnap(), LLM: llmService(t, backend), TestType: "C3",
	})
	if !errors.Is(err, ingest.ErrAllRejected) {
		t.Fatalf("err = %v, want ErrAllRejected", err)
	}
}

func TestIngestLLMNoPayloadSkipsBackend(t *testing.T) {
	backend := &fakeLLM{content: twoGoodItems}
	// Fetcher returns an artifact with no text, so there is nothing to extract.
	empty := emptyFetcher{}
	svc := ingest.NewService(&fakeBank{}, empty)

	_, err := svc.IngestLLM(context.Background(), ingest.LLMExtractRequest{
		Source: llmSnap(), LLM: llmService(t, backend), TestType: "C3",
	})
	if !errors.Is(err, ingest.ErrNoPayload) {
		t.Fatalf("err = %v, want ErrNoPayload", err)
	}
	if backend.last.Model != "" {
		t.Error("backend should not be called when there is no payload")
	}
}

func TestIngestLLMRequiresService(t *testing.T) {
	svc := ingest.NewService(&fakeBank{}, payloadFetcher{})
	_, err := svc.IngestLLM(context.Background(), ingest.LLMExtractRequest{Source: llmSnap()})
	if !errors.Is(err, ingest.ErrNoLLM) {
		t.Fatalf("err = %v, want ErrNoLLM", err)
	}
}

// emptyFetcher supports every source but returns an artifact with no text.
type emptyFetcher struct{}

func (emptyFetcher) Supports(source.Snapshot) bool { return true }
func (emptyFetcher) Fetch(context.Context, ports.FetchRequest) (ports.FetchResult, error) {
	return ports.FetchResult{SourceID: "src-1", Items: []ports.RawItem{{ExternalID: "no-text"}}}, nil
}

// manyFetcher returns a fixed set of artifacts, used to exercise payload joining
// across multiple RawItems (as multi-file zip sources produce).
type manyFetcher struct{ items []ports.RawItem }

func (f manyFetcher) Supports(source.Snapshot) bool { return true }
func (f manyFetcher) Fetch(context.Context, ports.FetchRequest) (ports.FetchResult, error) {
	return ports.FetchResult{SourceID: "src-1", Items: f.items}, nil
}

// TestIngestLLMCapsOversizedPayload pins the payload cap: many artifacts whose
// combined text (plus separators) exceeds the 100k ceiling, ending in a
// multibyte run, must truncate to a valid UTF-8 string within the cap. The
// pre-fix join wrote the separator outside the budget, drove a negative slice
// bound, and panicked on exactly this shape.
func TestIngestLLMCapsOversizedPayload(t *testing.T) {
	long := strings.Repeat("a", 40_000)
	multibyte := strings.Repeat("é", 40_000) // 2 bytes per rune
	fetch := manyFetcher{items: []ports.RawItem{{Stem: long}, {Stem: long}, {Stem: multibyte}}}
	backend := &fakeLLM{content: twoGoodItems}
	svc := ingest.NewService(&fakeBank{}, fetch)

	if _, err := svc.IngestLLM(context.Background(), ingest.LLMExtractRequest{
		Source: llmSnap(), LLM: llmService(t, backend), TestType: "C3",
	}); err != nil {
		t.Fatalf("IngestLLM: %v", err)
	}
	sent := userMessage(backend.last)
	if len(sent) > 100_000 {
		t.Errorf("payload = %d bytes, want <= 100000", len(sent))
	}
	if !utf8.ValidString(sent) {
		t.Error("truncated payload split a multibyte rune")
	}
}

// TestIngestLLMContentAddressedIDs pins that item ids track content, not array
// position: distinct candidates get distinct ids, and the same candidate
// extracted again keeps its id (so a re-run rewrites its own item instead of
// clobbering an unrelated positional neighbour).
func TestIngestLLMContentAddressedIDs(t *testing.T) {
	bank := &fakeBank{}
	svc := ingest.NewService(bank, payloadFetcher{})
	req := ingest.LLMExtractRequest{
		Source: llmSnap(), LLM: llmService(t, &fakeLLM{content: twoGoodItems}), TestType: "C3",
	}

	if _, err := svc.IngestLLM(context.Background(), req); err != nil {
		t.Fatalf("IngestLLM #1: %v", err)
	}
	first0, first1 := bank.saved[0].ID, bank.saved[1].ID
	if first0 == first1 {
		t.Fatal("distinct candidates collided to a single id")
	}
	if first0 == "src-1-llm-1" {
		t.Errorf("id %q is positional, want content-addressed", first0)
	}

	// A second run over the same content must reproduce the same ids.
	req.LLM = llmService(t, &fakeLLM{content: twoGoodItems})
	if _, err := svc.IngestLLM(context.Background(), req); err != nil {
		t.Fatalf("IngestLLM #2: %v", err)
	}
	if bank.saved[2].ID != first0 || bank.saved[3].ID != first1 {
		t.Errorf("re-extraction changed ids: %v,%v then %v,%v",
			first0, first1, bank.saved[2].ID, bank.saved[3].ID)
	}
}

// TestIngestLLMDedupesIdenticalCandidates pins that two byte-identical
// candidates in one completion collapse to a single stored item, so the run's
// Saved count never exceeds the distinct items persisted.
func TestIngestLLMDedupesIdenticalCandidates(t *testing.T) {
	dup := `{"items":[
	  {"stem":"1+1?","options":["1","2","3","4"],"answer_index":1},
	  {"stem":"1+1?","options":["1","2","3","4"],"answer_index":1}
	]}`
	bank := &fakeBank{}
	svc := ingest.NewService(bank, payloadFetcher{})

	rep, err := svc.IngestLLM(context.Background(), ingest.LLMExtractRequest{
		Source: llmSnap(), LLM: llmService(t, &fakeLLM{content: dup}), TestType: "C3",
	})
	if err != nil {
		t.Fatalf("IngestLLM: %v", err)
	}
	if rep.Normalized != 1 || rep.Saved != 1 {
		t.Errorf("report = %+v, want normalized=1 saved=1 (duplicate collapsed)", rep)
	}
	if len(bank.saved) != 1 {
		t.Errorf("bank stored %d items, want 1", len(bank.saved))
	}
}

func userMessage(req ports.LLMRequest) string {
	for _, m := range req.Messages {
		if m.Role == ports.LLMRoleUser {
			return m.Content
		}
	}
	return ""
}

func userMessageContains(req ports.LLMRequest, sub string) bool {
	for _, m := range req.Messages {
		if m.Role == ports.LLMRoleUser && strings.Contains(m.Content, sub) {
			return true
		}
	}
	return false
}
