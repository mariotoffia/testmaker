package main

import (
	"context"
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

func TestLLMClampCapsTokens(t *testing.T) {
	hook := llmClampHook(4096, nil)
	req := ports.LLMRequest{MaxTokens: 100000, Model: "anything"}
	if err := hook(context.Background(), &req); err != nil {
		t.Fatalf("hook errored: %v", err)
	}
	if req.MaxTokens != 4096 {
		t.Fatalf("MaxTokens = %d, want clamped to 4096", req.MaxTokens)
	}
	// Zero (backend default) is also clamped to the cap — a huge default is bounded.
	req2 := ports.LLMRequest{MaxTokens: 0, Model: "x"}
	_ = hook(context.Background(), &req2)
	if req2.MaxTokens != 4096 {
		t.Fatalf("zero MaxTokens should clamp to 4096, got %d", req2.MaxTokens)
	}
}

func TestLLMClampGatesModel(t *testing.T) {
	hook := llmClampHook(4096, []string{"gpt-ok", "local-ok"})
	if err := hook(context.Background(), &ports.LLMRequest{Model: "gpt-ok"}); err != nil {
		t.Fatalf("allowed model rejected: %v", err)
	}
	err := hook(context.Background(), &ports.LLMRequest{Model: "forbidden"})
	if !errors.Is(err, shared.ErrInvalid) {
		t.Fatalf("forbidden model err = %v, want ErrInvalid", err)
	}
}

func TestLLMClampEmptyAllowListPermitsAny(t *testing.T) {
	hook := llmClampHook(0, nil) // no cap, no allow-list
	if err := hook(context.Background(), &ports.LLMRequest{Model: "whatever", MaxTokens: 50}); err != nil {
		t.Fatalf("unconfigured clamp must permit: %v", err)
	}
}
