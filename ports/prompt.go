package ports

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/prompt"
)

// PromptRepository stores versioned LLM prompt templates (driven port).
// Identity is the PromptID; Put replaces by id. ByPurpose selects the active
// prompt for a purpose: highest Version wins, ties broken by lexically
// smallest ID, so every adapter resolves identically.
type PromptRepository interface {
	// Get returns the prompt with the given id or ErrUnknownPrompt.
	Get(ctx context.Context, id prompt.PromptID) (prompt.Snapshot, error)
	// ByPurpose returns the active prompt for the purpose or ErrUnknownPrompt.
	ByPurpose(ctx context.Context, p prompt.Purpose) (prompt.Snapshot, error)
	// List returns all stored prompts.
	List(ctx context.Context) ([]prompt.Snapshot, error)
	// Put inserts or replaces a prompt by id.
	Put(ctx context.Context, snap prompt.Snapshot) error
	// Delete removes a prompt by id (no error if absent).
	Delete(ctx context.Context, id prompt.PromptID) error
}
