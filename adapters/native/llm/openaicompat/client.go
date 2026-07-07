package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// Adapter error sentinels. Callers match by Code with errors.Is; the wrapped
// cause (transport error, decode error) stays reachable through Unwrap, so a
// cancelled request still satisfies errors.Is(err, context.Canceled).
var (
	// ErrInvalidConfig marks a Config that New rejected (bad/empty BaseURL).
	ErrInvalidConfig = &shared.TestmakerError{
		Code: "openaicompat.invalid_config", Class: shared.ClassInvalid, Message: "invalid openai-compatible config",
	}
	// ErrInvalidRequest marks an LLMRequest the adapter cannot turn into a wire
	// request (e.g. JSONSchema is not valid JSON).
	ErrInvalidRequest = &shared.TestmakerError{
		Code: "openaicompat.invalid_request", Class: shared.ClassInvalid, Message: "invalid llm request",
	}
	// ErrBackend marks a failed round-trip: the request could not be sent, was
	// cancelled, or the backend answered with a non-2xx status.
	ErrBackend = &shared.TestmakerError{
		Code: "openaicompat.backend", Class: shared.ClassUnavailable, Message: "llm backend call failed",
	}
	// ErrInvalidResponse marks a 2xx body the adapter could not parse into a
	// completion (malformed JSON or no choices).
	ErrInvalidResponse = &shared.TestmakerError{
		Code: "openaicompat.invalid_response", Class: shared.ClassInvalid, Message: "invalid llm response",
	}
)

// defaultTimeout bounds a single completion round-trip when the caller does not
// supply their own HTTP client. A per-call context deadline still takes
// precedence when it is shorter.
const defaultTimeout = 120 * time.Second

// maxResponseBytes caps how much of a response body the adapter reads, guarding
// against a hostile or runaway backend.
// ponytail: fixed 8 MiB ceiling; a chat completion never approaches it. Raise
// or stream if a future backend returns larger payloads.
const maxResponseBytes = 8 << 20

// AuthScheme selects how APIKey is presented to the backend.
type AuthScheme string

const (
	// AuthSchemeBearer sends "Authorization: Bearer <key>" — the OpenAI
	// convention, also used by Ollama/vLLM/LM Studio/llama.cpp. It is the zero
	// value, so an unset AuthScheme means Bearer.
	AuthSchemeBearer AuthScheme = ""
	// AuthSchemeAPIKey sends "api-key: <key>" — the Azure OpenAI convention.
	// (Azure additionally wants ?api-version=... on BaseURL, which JoinPath
	// preserves.)
	AuthSchemeAPIKey AuthScheme = "api-key"
)

// Config selects and tunes the backend. Only BaseURL is required; local servers
// (Ollama, vLLM, LM Studio, llama.cpp) accept an empty APIKey.
type Config struct {
	// BaseURL is the API root, e.g. "https://api.openai.com/v1" or
	// "http://localhost:11434/v1". "/chat/completions" is appended to it.
	BaseURL string
	// APIKey, when set, authenticates the request per AuthScheme.
	APIKey string
	// AuthScheme picks the auth header for APIKey: the zero value
	// (AuthSchemeBearer) sends "Authorization: Bearer <key>", AuthSchemeAPIKey
	// sends "api-key: <key>" (Azure OpenAI). Ignored when APIKey is empty.
	AuthScheme AuthScheme
	// HTTPClient overrides the transport; nil uses a client with a sane timeout.
	HTTPClient *http.Client
}

// Client is a ports.LLM backed by an OpenAI-compatible chat API.
type Client struct {
	endpoint   string
	authHeader string
	authValue  string
	http       *http.Client
}

// New validates cfg and returns a ready client; it performs no I/O.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, ErrInvalidConfig.WithMessage("BaseURL is required")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, ErrInvalidConfig.WithMessagef("BaseURL is not a valid URL: %q", cfg.BaseURL).Wrap(err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, ErrInvalidConfig.WithMessagef("BaseURL needs an http(s) scheme and host: %q", cfg.BaseURL)
	}

	authHeader, authValue, err := authFor(cfg)
	if err != nil {
		return nil, err
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	// JoinPath appends the chat path while preserving path escaping (e.g. an
	// encoded %2F segment) and any query the caller put on BaseURL (e.g. Azure's
	// required ?api-version=...); plain concatenation would corrupt both.
	endpoint := u.JoinPath("chat", "completions")
	return &Client{
		endpoint:   endpoint.String(),
		authHeader: authHeader,
		authValue:  authValue,
		http:       httpClient,
	}, nil
}

// authFor validates cfg.AuthScheme and returns the header name/value to send.
// Both are empty when APIKey is unset (local servers need no auth).
func authFor(cfg Config) (header, value string, err error) {
	switch cfg.AuthScheme {
	case AuthSchemeBearer, AuthSchemeAPIKey:
		// valid
	default:
		return "", "", ErrInvalidConfig.WithMessagef(
			"unknown AuthScheme %q (use \"\" for Bearer or \"api-key\")", cfg.AuthScheme)
	}
	if cfg.APIKey == "" {
		return "", "", nil
	}
	if cfg.AuthScheme == AuthSchemeAPIKey {
		return "api-key", cfg.APIKey, nil
	}
	return "Authorization", "Bearer " + cfg.APIKey, nil
}

// --- wire types ------------------------------------------------------------

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type jsonSchemaFormat struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict,omitempty"`
}

type responseFormat struct {
	Type       string            `json:"type"`
	JSONSchema *jsonSchemaFormat `json:"json_schema,omitempty"`
}

type chatRequest struct {
	Model           string          `json:"model,omitempty"`
	Messages        []chatMessage   `json:"messages"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	Temperature     float64         `json:"temperature,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	ResponseFormat  *responseFormat `json:"response_format,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Generate maps req onto a POST {BaseURL}/chat/completions call and maps the
// first choice back into an LLMResponse. Hints the wire has no field for
// (ContextLength) are dropped silently; zero-valued knobs are omitted so the
// backend applies its own defaults.
func (c *Client) Generate(ctx context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	body, err := buildRequestBody(req)
	if err != nil {
		return ports.LLMResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return ports.LLMResponse{}, ErrBackend.WithMessage("build http request").Wrap(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.authValue != "" {
		httpReq.Header.Set(c.authHeader, c.authValue)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return ports.LLMResponse{}, ErrBackend.WithMessage("send request").Wrap(err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return ports.LLMResponse{}, ErrBackend.WithMessage("read response body").Wrap(err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ports.LLMResponse{}, ErrBackend.
			WithMessagef("backend returned status %d", resp.StatusCode).
			With("status", resp.StatusCode).
			With("body", snippet(payload))
	}

	var decoded chatResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return ports.LLMResponse{}, ErrInvalidResponse.WithMessage("decode response body").Wrap(err)
	}
	if len(decoded.Choices) == 0 {
		return ports.LLMResponse{}, ErrInvalidResponse.WithMessage("response contained no choices")
	}
	// A missing/null content field is a malformed completion, distinct from an
	// explicit empty string (which is a valid, if degenerate, answer).
	content := decoded.Choices[0].Message.Content
	if content == nil {
		return ports.LLMResponse{}, ErrInvalidResponse.WithMessage("response choice had no message content")
	}

	return ports.LLMResponse{
		Content:   *content,
		Model:     decoded.Model, // the model the backend reports as having served the request
		TokensIn:  decoded.Usage.PromptTokens,
		TokensOut: decoded.Usage.CompletionTokens,
	}, nil
}

func buildRequestBody(req ports.LLMRequest) ([]byte, error) {
	wire := chatRequest{
		Model:           req.Model,
		Messages:        make([]chatMessage, len(req.Messages)),
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		ReasoningEffort: string(req.Effort),
	}
	for i, m := range req.Messages {
		wire.Messages[i] = chatMessage{Role: string(m.Role), Content: m.Content}
	}
	if req.JSONSchema != "" {
		// OpenAI structured output requires the schema to be a JSON object; a
		// valid-JSON non-object ("true", "123", "[]") is a caller error, not a
		// transient backend failure, so reject it before the HTTP call.
		if trimmed := bytes.TrimSpace([]byte(req.JSONSchema)); len(trimmed) == 0 ||
			trimmed[0] != '{' || !json.Valid(trimmed) {
			return nil, ErrInvalidRequest.WithMessage("JSONSchema must be a JSON object")
		}
		wire.ResponseFormat = &responseFormat{
			Type: "json_schema",
			JSONSchema: &jsonSchemaFormat{
				Name:   "response",
				Schema: json.RawMessage(req.JSONSchema),
				Strict: true,
			},
		}
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return nil, ErrInvalidRequest.WithMessage("marshal request").Wrap(err)
	}
	return body, nil
}

// snippet trims a body to a short, log-safe excerpt for error context.
func snippet(b []byte) string {
	const max = 512
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
