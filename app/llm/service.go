// Package llm is the LLM application service: the one "library" every
// LLM-consuming step (extraction, translation, derivation, generation)
// receives. It wraps a ports.LLM backend and a ports.PromptRepository, so a
// stored prompt is applied automatically per Purpose and registered hooks run
// around every generation. Hooks are registered only by the composition root
// via functional options.
//
// Call order of GenerateFor: prompt lookup + render + prepend as system
// message, then BeforeGenerate hooks (they see the final request), the
// backend, then AfterGenerate hooks (they see the Result including prompt
// provenance). Any hook error aborts the call.
package llm

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/prompt"
	"github.com/mariotoffia/testmaker/ports"
)

// BeforeGenerate runs before the backend call and may mutate the request —
// e.g. per-purpose model defaults, token caps, redaction.
type BeforeGenerate func(ctx context.Context, req *ports.LLMRequest) error

// AfterGenerate runs after the backend call and may inspect/mutate the
// result — e.g. provenance recording, JSON validation, usage metering.
type AfterGenerate func(ctx context.Context, req ports.LLMRequest, res *Result) error

// Result is an LLMResponse plus the provenance of the prompt that shaped it
// (zero values when Generate was called without a stored prompt).
type Result struct {
	ports.LLMResponse
	PromptID      prompt.PromptID
	PromptVersion int
}

// Option configures the service at construction.
type Option func(*Service)

// WithBeforeGenerate appends a hook that runs before every backend call, in
// registration order.
func WithBeforeGenerate(h BeforeGenerate) Option {
	return func(s *Service) { s.before = append(s.before, h) }
}

// WithAfterGenerate appends a hook that runs after every backend call, in
// registration order.
func WithAfterGenerate(h AfterGenerate) Option {
	return func(s *Service) { s.after = append(s.after, h) }
}

// Service is the hook-applying, prompt-applying LLM service. It satisfies
// ports.LLM itself, so consumers written against the port get the full
// behaviour transparently.
type Service struct {
	backend ports.LLM
	prompts ports.PromptRepository
	before  []BeforeGenerate
	after   []AfterGenerate
}

// NewService wires a backend and a prompt store into the service.
func NewService(backend ports.LLM, prompts ports.PromptRepository, opts ...Option) *Service {
	s := &Service{backend: backend, prompts: prompts}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Generate runs the hook chain around one backend completion.
func (s *Service) Generate(ctx context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	res, err := s.generate(ctx, req, Result{})
	return res.LLMResponse, err
}

// GenerateFor looks up the stored prompt for the purpose, renders it with the
// given values and prepends it as the system message, then generates. Callers
// should not supply their own system message in req.
func (s *Service) GenerateFor(
	ctx context.Context, purpose prompt.Purpose, values map[string]string, req ports.LLMRequest,
) (Result, error) {
	snap, err := s.prompts.ByPurpose(ctx, purpose)
	if err != nil {
		return Result{}, err
	}
	system, rerr := prompt.RehydrateFromSnapshot(snap).Render(values)
	if rerr != nil {
		return Result{}, rerr
	}
	req.Messages = append(
		[]ports.LLMMessage{{Role: ports.LLMRoleSystem, Content: system}}, req.Messages...,
	)
	return s.generate(ctx, req, Result{PromptID: snap.ID, PromptVersion: snap.Version})
}

func (s *Service) generate(ctx context.Context, req ports.LLMRequest, seed Result) (Result, error) {
	for _, h := range s.before {
		if err := h(ctx, &req); err != nil {
			return Result{}, err
		}
	}
	resp, err := s.backend.Generate(ctx, req)
	if err != nil {
		return Result{}, err
	}
	seed.LLMResponse = resp
	for _, h := range s.after {
		if err := h(ctx, req, &seed); err != nil {
			return Result{}, err
		}
	}
	return seed, nil
}
