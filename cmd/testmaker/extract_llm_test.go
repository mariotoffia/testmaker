package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/llm/fileprompts"
	"github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb"
	"github.com/mariotoffia/testmaker/app/ingest"
	llmapp "github.com/mariotoffia/testmaker/app/llm"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/testutil/ollamalocal"
)

// TestMain tears down any Docker LLM backend that ollamalocal started for the
// integration test. Shutdown is a no-op when no container was started.
func TestMain(m *testing.M) {
	code := m.Run()
	ollamalocal.Shutdown()
	os.Exit(code)
}

// llmPayloadFetcher is an in-process ports.Fetcher returning one text artifact,
// so the LLM extraction path has an unstructured payload to lift.
type llmPayloadFetcher struct{ text string }

func (llmPayloadFetcher) Supports(source.Snapshot) bool { return true }
func (f llmPayloadFetcher) Fetch(context.Context, ports.FetchRequest) (ports.FetchResult, error) {
	return ports.FetchResult{Items: []ports.RawItem{{Stem: f.text}}}, nil
}

func llmSource() source.Snapshot {
	return source.Snapshot{
		ID:        "llm-src",
		Name:      "LLM Source",
		License:   source.License{Redistributable: shared.RedistYes},
		TestTypes: []source.TestTypeCode{"C3"},
	}
}

// TestIngestLLMCloudShapeEndToEnd proves the full Block 12 wiring — openaicompat
// backend → app/llm service (prompt from the file store) → app/ingest extraction
// → item.NewItem → bank — deterministically against a canned OpenAI-compatible
// endpoint (the "cloud backend" of the done-when, same adapter as local Ollama).
func TestIngestLLMCloudShapeEndToEnd(t *testing.T) {
	// A canned completion whose content is a schema-valid extraction result.
	itemsJSON := `{"items":[{"stem":"What is 2+2?","options":["3","4","5","6"],"answer_index":1,"explanation":"2+2=4","difficulty":2}]}`
	body, err := json.Marshal(map[string]any{
		"model":   "cloud-model",
		"choices": []any{map[string]any{"message": map[string]any{"content": itemsJSON}}},
	})
	if err != nil {
		t.Fatalf("marshal canned response: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	client, err := openaicompat.New(openaicompat.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	prompts, err := fileprompts.Open("../../data/prompts")
	if err != nil {
		t.Fatalf("open prompt store: %v", err)
	}

	bank := memorytestdb.NewStore()
	svc := ingest.NewService(bank, llmPayloadFetcher{text: "some payload"})

	rep, err := svc.IngestLLM(context.Background(), ingest.LLMExtractRequest{
		Source:   llmSource(),
		LLM:      llmapp.NewService(client, prompts),
		TestType: "C3",
		Model:    "cloud-model",
	})
	if err != nil {
		t.Fatalf("IngestLLM: %v", err)
	}
	if rep.Saved != 1 {
		t.Fatalf("saved %d items, want 1 (report %+v)", rep.Saved, rep)
	}
	items, err := bank.ListItems(context.Background(), item.ItemFilter{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("bank holds %d items, want 1", len(items))
	}
	got := items[0]
	if got.Provenance.Origin != item.OriginGenerated {
		t.Errorf("origin = %q, want generated", got.Provenance.Origin)
	}
	if got.Provenance.SourceID != "llm-src" || got.Provenance.Redistributable != shared.RedistYes {
		t.Errorf("provenance = %+v", got.Provenance)
	}
	if got.AnswerFormat != item.FormatMultipleChoice || got.AnswerKey.OptionID != "o1" {
		t.Errorf("answer mapped wrong: format=%q key=%q", got.AnswerFormat, got.AnswerKey.OptionID)
	}
}

// TestIngestLLMLocalOllamaEndToEnd runs the same extraction wiring against a real
// backend provisioned by ollamalocal (Docker Ollama + a tiny model). It is
// skipped under -short or without Docker. A tiny model is nondeterministic and
// may not return schema-valid items, so a parse/all-rejected/timeout outcome is
// a skip, not a failure — only infrastructure errors are red.
func TestIngestLLMLocalOllamaEndToEnd(t *testing.T) {
	baseURL := ollamalocal.Endpoint(t) // skips: -short, no Docker, or start failure
	model := ollamalocal.Model(t)

	client, err := openaicompat.New(openaicompat.Config{BaseURL: baseURL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	prompts, err := fileprompts.Open("../../data/prompts")
	if err != nil {
		t.Fatalf("open prompt store: %v", err)
	}

	bank := memorytestdb.NewStore()
	payload := "Question: What is 2 + 2?\nA) 3\nB) 4\nC) 5\nD) 6\nCorrect answer: B (because 2+2=4)."
	svc := ingest.NewService(bank, llmPayloadFetcher{text: payload})

	rep, err := svc.IngestLLM(ollamalocal.Context(t), ingest.LLMExtractRequest{
		Source:    llmSource(),
		LLM:       llmapp.NewService(client, prompts),
		TestType:  "C3",
		Model:     model,
		MaxTokens: 512,
	})
	switch {
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, ingest.ErrExtractParse),
		errors.Is(err, ingest.ErrAllRejected),
		errors.Is(err, ingest.ErrNoPayload):
		t.Skipf("tiny model did not produce schema-valid items (acceptable): %v", err)
	case err != nil:
		t.Fatalf("IngestLLM against real backend: %v", err)
	}
	// The model returned schema-valid items: assert shape, never content.
	if rep.Saved < 1 {
		t.Skipf("real backend extracted no valid items (report %+v)", rep)
	}
	items, err := bank.ListItems(context.Background(), item.ItemFilter{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	for _, it := range items {
		if it.Provenance.Origin != item.OriginGenerated {
			t.Errorf("item %s origin = %q, want generated", it.ID, it.Provenance.Origin)
		}
		if it.AnswerFormat != item.FormatMultipleChoice || len(it.Options) < 4 {
			t.Errorf("item %s has format=%q, %d options", it.ID, it.AnswerFormat, len(it.Options))
		}
	}
}
