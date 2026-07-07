package llm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/app/llm"
	"github.com/mariotoffia/testmaker/domain/prompt"
	"github.com/mariotoffia/testmaker/ports"
)

// the service must itself satisfy the LLM port so consumers written against
// the port get hooks + prompts transparently
var _ ports.LLM = (*llm.Service)(nil)

type fakeBackend struct {
	lastReq ports.LLMRequest
	resp    ports.LLMResponse
	err     error
	calls   int
}

var _ ports.LLM = (*fakeBackend)(nil)

func (f *fakeBackend) Generate(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	f.calls++
	f.lastReq = req
	return f.resp, f.err
}

type fakePrompts struct {
	byPurpose map[prompt.Purpose]prompt.Snapshot
}

var _ ports.PromptRepository = (*fakePrompts)(nil)

func (f *fakePrompts) Get(_ context.Context, id prompt.PromptID) (prompt.Snapshot, error) {
	for _, s := range f.byPurpose {
		if s.ID == id {
			return s, nil
		}
	}
	return prompt.Snapshot{}, prompt.ErrUnknownPrompt
}

func (f *fakePrompts) ByPurpose(_ context.Context, p prompt.Purpose) (prompt.Snapshot, error) {
	s, ok := f.byPurpose[p]
	if !ok {
		return prompt.Snapshot{}, prompt.ErrUnknownPrompt
	}
	return s, nil
}

func (f *fakePrompts) List(_ context.Context) ([]prompt.Snapshot, error) { return nil, nil }

func (f *fakePrompts) Put(_ context.Context, s prompt.Snapshot) error {
	f.byPurpose[s.Purpose] = s
	return nil
}

func (f *fakePrompts) Delete(_ context.Context, _ prompt.PromptID) error { return nil }

func newFakePrompts() *fakePrompts {
	return &fakePrompts{byPurpose: map[prompt.Purpose]prompt.Snapshot{
		prompt.PurposeTranslation: prompt.MustPrompt(prompt.Spec{
			ID: "translate-item", Version: 3, Purpose: prompt.PurposeTranslation,
			Template: "Translate every item field to {{.language}}. Keep the answer key.",
			Params:   []string{"language"},
		}).Snapshot(),
	}}
}

func TestGenerateRunsHooksInOrder(t *testing.T) {
	backend := &fakeBackend{resp: ports.LLMResponse{Content: "hi"}}
	var order []string
	svc := llm.NewService(backend, newFakePrompts(),
		llm.WithBeforeGenerate(func(_ context.Context, req *ports.LLMRequest) error {
			order = append(order, "before-1")
			req.Model = "capped-model"
			return nil
		}),
		llm.WithBeforeGenerate(func(_ context.Context, req *ports.LLMRequest) error {
			order = append(order, "before-2:"+req.Model)
			return nil
		}),
		llm.WithAfterGenerate(func(_ context.Context, _ ports.LLMRequest, res *llm.Result) error {
			order = append(order, "after")
			res.Content += "!"
			return nil
		}),
	)

	res, err := svc.Generate(context.Background(), ports.LLMRequest{Model: "user-model"})
	if err != nil {
		t.Fatal(err)
	}
	if backend.lastReq.Model != "capped-model" {
		t.Fatalf("before hook mutation lost: %q", backend.lastReq.Model)
	}
	if res.Content != "hi!" {
		t.Fatalf("after hook mutation lost: %q", res.Content)
	}
	want := []string{"before-1", "before-2:capped-model", "after"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("hook order = %v, want %v", order, want)
	}
}

func TestBeforeHookErrorAbortsBeforeBackend(t *testing.T) {
	backend := &fakeBackend{}
	boom := errors.New("budget exceeded")
	svc := llm.NewService(backend, newFakePrompts(),
		llm.WithBeforeGenerate(func(context.Context, *ports.LLMRequest) error { return boom }),
	)

	if _, err := svc.Generate(context.Background(), ports.LLMRequest{}); !errors.Is(err, boom) {
		t.Fatalf("want hook error, got %v", err)
	}
	if backend.calls != 0 {
		t.Fatal("backend must not be called when a before hook fails")
	}
}

func TestGenerateForAppliesStoredPrompt(t *testing.T) {
	backend := &fakeBackend{resp: ports.LLMResponse{Content: "översatt"}}
	svc := llm.NewService(backend, newFakePrompts())

	res, err := svc.GenerateFor(context.Background(), prompt.PurposeTranslation,
		map[string]string{"language": "Swedish"},
		ports.LLMRequest{Messages: []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: "item json"}}},
	)
	if err != nil {
		t.Fatal(err)
	}

	msgs := backend.lastReq.Messages
	if len(msgs) != 2 || msgs[0].Role != ports.LLMRoleSystem {
		t.Fatalf("system prompt not prepended: %+v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "to Swedish") {
		t.Fatalf("template not rendered: %q", msgs[0].Content)
	}
	if res.PromptID != "translate-item" || res.PromptVersion != 3 {
		t.Fatalf("prompt provenance missing: %+v", res)
	}
}

func TestGenerateForUnknownPurpose(t *testing.T) {
	svc := llm.NewService(&fakeBackend{}, newFakePrompts())

	_, err := svc.GenerateFor(context.Background(), prompt.PurposeDerivation, nil, ports.LLMRequest{})
	if !errors.Is(err, prompt.ErrUnknownPrompt) {
		t.Fatalf("want ErrUnknownPrompt, got %v", err)
	}
}

func TestGenerateForMissingParamFails(t *testing.T) {
	backend := &fakeBackend{}
	svc := llm.NewService(backend, newFakePrompts())

	_, err := svc.GenerateFor(context.Background(), prompt.PurposeTranslation, nil, ports.LLMRequest{})
	if !errors.Is(err, prompt.ErrInvalidPrompt) {
		t.Fatalf("want ErrInvalidPrompt for missing placeholder, got %v", err)
	}
	if backend.calls != 0 {
		t.Fatal("backend must not be called when the prompt cannot render")
	}
}
