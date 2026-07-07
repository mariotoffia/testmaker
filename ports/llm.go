package ports

import "context"

// LLMRole identifies who authored a chat message.
type LLMRole string

const (
	LLMRoleSystem    LLMRole = "system"
	LLMRoleUser      LLMRole = "user"
	LLMRoleAssistant LLMRole = "assistant"
)

// LLMEffort is a backend-neutral reasoning-effort hint; adapters map it to
// whatever their backend supports (reasoning tokens, thinking budget) and
// ignore it otherwise.
type LLMEffort string

const (
	LLMEffortLow    LLMEffort = "low"
	LLMEffortMedium LLMEffort = "medium"
	LLMEffortHigh   LLMEffort = "high"
)

// LLMMessage is one turn of a chat prompt.
type LLMMessage struct {
	Role    LLMRole
	Content string
}

// LLMRequest asks an LLM backend for one completion. Zero values mean
// "backend default". Fields a backend cannot honour are ignored, never an
// error — the request is a hint set, not a contract on backend internals.
type LLMRequest struct {
	Model         string       // backend model id, e.g. "gpt-4o-mini", "llama3.1:8b"
	Messages      []LLMMessage // the prompt; first message is usually the system role
	MaxTokens     int          // max output tokens
	ContextLength int          // context-window hint (e.g. Ollama num_ctx); 0 = model default
	Temperature   float64      // sampling temperature; 0 = backend default
	Effort        LLMEffort    // reasoning-effort hint; "" = backend default
	JSONSchema    string       // non-empty = constrain output to this JSON schema (structured extraction)
}

// LLMResponse is the completion returned by an LLM backend.
type LLMResponse struct {
	Content   string // the generated text (JSON when JSONSchema was set)
	Model     string // model that actually served the request
	TokensIn  int    // prompt tokens, 0 if the backend does not report usage
	TokensOut int    // completion tokens, 0 if the backend does not report usage
}

// LLM is the single driven port through which any step — extraction,
// translation, item generation/derivation — talks to a language model.
// Backends (OpenAI-compatible HTTP, Ollama, Bedrock, ...) are adapters;
// consumers receive an LLM via constructor injection from the composition
// root and never know which backend serves them. LLM output is untrusted
// input: whatever comes back must pass the domain constructors
// (e.g. item.NewItem) before it reaches a bank or an examinee.
type LLM interface {
	// Generate returns one completion for the request.
	Generate(ctx context.Context, req LLMRequest) (LLMResponse, error)
}
