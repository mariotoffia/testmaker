// Package prompt is the "LLM prompts" context: stored, versioned prompt
// templates that the app/llm service looks up (by Purpose) and applies
// automatically to LLM calls.
//
// The aggregate root is Prompt; it is validated on construction (NewPrompt)
// and crosses ports as a Snapshot DTO. Templates are Go text/template;
// Render fills them with caller values and fails on missing placeholders,
// so a bad call site is an error, not a silently broken prompt.
//
// This package is pure (stdlib + the shared kernel only); wire formats are
// the concern of the prompt-store adapters.
package prompt

import "github.com/mariotoffia/testmaker/domain/shared"

// Prompt-context sentinels.
var (
	// ErrInvalidPrompt is returned when a Spec violates an invariant or a
	// template fails to parse/render.
	ErrInvalidPrompt = &shared.TestmakerError{
		Code: "prompt.invalid", Class: shared.ClassInvalid, Message: "invalid prompt",
	}
	// ErrUnknownPrompt is returned when a prompt id or purpose is not stored.
	ErrUnknownPrompt = &shared.TestmakerError{
		Code: "prompt.unknown", Class: shared.ClassNotFound, Message: "unknown prompt",
	}
)
