package main

import (
	"context"

	llmapp "github.com/mariotoffia/testmaker/app/llm"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// llmClampHook is a BeforeGenerate hook that bounds caller-controlled LLM spend
// on the delivery surface (DESIGN.md §7.4): it caps MaxTokens to maxTokens (a
// zero request value means "backend default", which is also clamped so a large
// default can't slip through) and, when allowed is non-empty, rejects any model
// not on the list. maxTokens <= 0 disables the cap; an empty allow-list permits
// any model. This is the composition root's use of the hook point the LLM
// service was built around — steps never register their own (DESIGN.md §6).
func llmClampHook(maxTokens int, allowed []string) llmapp.BeforeGenerate {
	allowset := make(map[string]struct{}, len(allowed))
	for _, m := range allowed {
		allowset[m] = struct{}{}
	}
	return func(_ context.Context, req *ports.LLMRequest) error {
		if maxTokens > 0 && (req.MaxTokens <= 0 || req.MaxTokens > maxTokens) {
			req.MaxTokens = maxTokens
		}
		if len(allowset) > 0 {
			if _, ok := allowset[req.Model]; !ok {
				return shared.ErrInvalid.WithMessagef("model %q is not in the allowed list", req.Model)
			}
		}
		return nil
	}
}
