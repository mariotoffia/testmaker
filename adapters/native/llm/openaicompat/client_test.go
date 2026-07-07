package openaicompat_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/testutil/ollamalocal"
)

var _ ports.LLM = (*openaicompat.Client)(nil)

// TestMain tears down any Docker LLM backend that ollamalocal started for the
// integration test. Shutdown is a no-op when no container was started (unit-only
// or -short runs), so this stays cheap on every run.
func TestMain(m *testing.M) {
	code := m.Run()
	ollamalocal.Shutdown()
	os.Exit(code)
}

const okResponse = `{"model":"gpt-4o-mini",` +
	`"choices":[{"message":{"role":"assistant","content":"hello world"}}],` +
	`"usage":{"prompt_tokens":11,"completion_tokens":7}}`

type recordedRequest struct {
	method     string
	path       string
	query      string
	requestURI string
	auth       string
	apiKey     string
	ctype      string
	body       []byte
}

// fakeBackend starts an httptest server that records the request it receives and
// replies with status/respBody. The recorded request travels on a buffered
// channel, giving the test goroutine a happens-before edge (race-safe read).
func fakeBackend(t *testing.T, baseSuffix string, status int, respBody string) (*openaicompat.Client, <-chan recordedRequest) {
	t.Helper()
	rec := make(chan recordedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec <- recordedRequest{
			method:     r.Method,
			path:       r.URL.Path,
			query:      r.URL.RawQuery,
			requestURI: r.RequestURI,
			auth:       r.Header.Get("Authorization"),
			apiKey:     r.Header.Get("api-key"),
			ctype:      r.Header.Get("Content-Type"),
			body:       b,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)

	client, err := openaicompat.New(openaicompat.Config{
		BaseURL:    srv.URL + baseSuffix,
		APIKey:     "test-key",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, rec
}

func TestGenerateRequestWireMapping(t *testing.T) {
	t.Run("full request maps every field", func(t *testing.T) {
		client, rec := fakeBackend(t, "/v1", http.StatusOK, okResponse)
		schema := `{"type":"object","properties":{"x":{"type":"string"}}}`

		_, err := client.Generate(context.Background(), ports.LLMRequest{
			Model: "gpt-4o-mini",
			Messages: []ports.LLMMessage{
				{Role: ports.LLMRoleSystem, Content: "be terse"},
				{Role: ports.LLMRoleUser, Content: "hi"},
			},
			MaxTokens:     256,
			ContextLength: 4096, // has no wire field: must be dropped silently
			Temperature:   0.7,
			Effort:        ports.LLMEffortHigh,
			JSONSchema:    schema,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}

		got := <-rec
		if got.method != http.MethodPost {
			t.Errorf("method = %q, want POST", got.method)
		}
		if got.path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", got.path)
		}
		if got.ctype != "application/json" {
			t.Errorf("content-type = %q", got.ctype)
		}
		if got.auth != "Bearer test-key" {
			t.Errorf("authorization = %q", got.auth)
		}

		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			MaxTokens       int     `json:"max_tokens"`
			Temperature     float64 `json:"temperature"`
			ReasoningEffort string  `json:"reasoning_effort"`
			ResponseFormat  *struct {
				Type       string `json:"type"`
				JSONSchema *struct {
					Name   string          `json:"name"`
					Schema json.RawMessage `json:"schema"`
					Strict bool            `json:"strict"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.Unmarshal(got.body, &body); err != nil {
			t.Fatalf("decode wire body: %v (%s)", err, got.body)
		}
		if body.Model != "gpt-4o-mini" || body.MaxTokens != 256 || body.Temperature != 0.7 {
			t.Errorf("scalar fields wrong: %+v", body)
		}
		if body.ReasoningEffort != "high" {
			t.Errorf("reasoning_effort = %q, want high", body.ReasoningEffort)
		}
		if len(body.Messages) != 2 ||
			body.Messages[0].Role != "system" || body.Messages[0].Content != "be terse" ||
			body.Messages[1].Role != "user" || body.Messages[1].Content != "hi" {
			t.Errorf("messages not mapped as-is: %+v", body.Messages)
		}
		if body.ResponseFormat == nil || body.ResponseFormat.Type != "json_schema" ||
			body.ResponseFormat.JSONSchema == nil || body.ResponseFormat.JSONSchema.Name != "response" ||
			!body.ResponseFormat.JSONSchema.Strict {
			t.Fatalf("response_format not shaped: %+v", body.ResponseFormat)
		}
		if !jsonEqual(t, []byte(schema), body.ResponseFormat.JSONSchema.Schema) {
			t.Errorf("schema embedded as %s, want %s", body.ResponseFormat.JSONSchema.Schema, schema)
		}
		if _, ok := rawKeys(t, got.body)["context_length"]; ok {
			t.Error("ContextLength leaked onto the wire")
		}
	})

	t.Run("zero-valued knobs are omitted", func(t *testing.T) {
		client, rec := fakeBackend(t, "/v1", http.StatusOK, okResponse)

		_, err := client.Generate(context.Background(), ports.LLMRequest{
			Model:    "llama3.1:8b",
			Messages: []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: "hi"}},
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}

		keys := rawKeys(t, (<-rec).body)
		for _, want := range []string{"model", "messages"} {
			if _, ok := keys[want]; !ok {
				t.Errorf("missing required key %q", want)
			}
		}
		for _, absent := range []string{"max_tokens", "temperature", "reasoning_effort", "response_format"} {
			if _, ok := keys[absent]; ok {
				t.Errorf("zero-valued key %q should have been omitted", absent)
			}
		}
	})
}

func TestGenerateBaseURLTrailingSlashNormalised(t *testing.T) {
	client, rec := fakeBackend(t, "/v1/", http.StatusOK, okResponse)

	if _, err := client.Generate(context.Background(), ports.LLMRequest{
		Model:    "m",
		Messages: []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := (<-rec).path; got != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions (no double slash)", got)
	}
}

func TestGenerateBaseURLQueryPreserved(t *testing.T) {
	// Azure's OpenAI-compatible surface requires ?api-version=...; the adapter
	// must append the chat path without clobbering the query.
	client, rec := fakeBackend(t, "/v1?api-version=preview", http.StatusOK, okResponse)

	if _, err := client.Generate(context.Background(), ports.LLMRequest{
		Model:    "m",
		Messages: []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	got := <-rec
	if got.path != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", got.path)
	}
	if got.query != "api-version=preview" {
		t.Errorf("query = %q, want api-version=preview", got.query)
	}
}

func TestGenerateBaseURLEscapedPathPreserved(t *testing.T) {
	// A percent-encoded path segment (%2F) must survive path joining; decoding
	// it to a real "/" would route to a different resource.
	client, rec := fakeBackend(t, "/tenant%2Facme/v1", http.StatusOK, okResponse)

	if _, err := client.Generate(context.Background(), ports.LLMRequest{
		Model:    "m",
		Messages: []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := (<-rec).requestURI; got != "/tenant%2Facme/v1/chat/completions" {
		t.Errorf("request-uri = %q, want /tenant%%2Facme/v1/chat/completions", got)
	}
}

func TestGenerateResponseMapping(t *testing.T) {
	t.Run("usage and model reported", func(t *testing.T) {
		client, _ := fakeBackend(t, "/v1", http.StatusOK, okResponse)
		resp, err := client.Generate(context.Background(), ports.LLMRequest{Model: "req-model"})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if resp.Content != "hello world" || resp.Model != "gpt-4o-mini" ||
			resp.TokensIn != 11 || resp.TokensOut != 7 {
			t.Fatalf("response mapped wrong: %+v", resp)
		}
	})

	t.Run("absent usage yields zero tokens", func(t *testing.T) {
		body := `{"model":"m","choices":[{"message":{"content":"x"}}]}`
		client, _ := fakeBackend(t, "/v1", http.StatusOK, body)
		resp, err := client.Generate(context.Background(), ports.LLMRequest{})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if resp.TokensIn != 0 || resp.TokensOut != 0 {
			t.Fatalf("tokens should be 0 when usage omitted: %+v", resp)
		}
	})

	t.Run("absent model reported as empty, not fabricated", func(t *testing.T) {
		body := `{"choices":[{"message":{"content":"x"}}]}`
		client, _ := fakeBackend(t, "/v1", http.StatusOK, body)
		resp, err := client.Generate(context.Background(), ports.LLMRequest{Model: "req-model"})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if resp.Model != "" {
			t.Fatalf("model = %q, want empty (backend must not be impersonated by the request)", resp.Model)
		}
	})

	t.Run("explicit empty content is a valid completion", func(t *testing.T) {
		body := `{"model":"m","choices":[{"message":{"content":""}}]}`
		client, _ := fakeBackend(t, "/v1", http.StatusOK, body)
		resp, err := client.Generate(context.Background(), ports.LLMRequest{})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if resp.Content != "" {
			t.Fatalf("content = %q, want empty string", resp.Content)
		}
	})
}

func TestGenerateErrorPaths(t *testing.T) {
	t.Run("non-2xx wraps ErrBackend with status context", func(t *testing.T) {
		client, _ := fakeBackend(t, "/v1", http.StatusInternalServerError, "boom")
		resp, err := client.Generate(context.Background(), ports.LLMRequest{})
		if !errors.Is(err, openaicompat.ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
		if resp != (ports.LLMResponse{}) {
			t.Errorf("response should be zero on error: %+v", resp)
		}
		var te *shared.TestmakerError
		if !errors.As(err, &te) || te.Class != shared.ClassUnavailable {
			t.Fatalf("want Unavailable TestmakerError, got %v", err)
		}
		if te.Context["status"] != 500 {
			t.Errorf("status context = %v, want 500", te.Context["status"])
		}
	})

	t.Run("malformed body wraps ErrInvalidResponse", func(t *testing.T) {
		client, _ := fakeBackend(t, "/v1", http.StatusOK, "{not json")
		_, err := client.Generate(context.Background(), ports.LLMRequest{})
		if !errors.Is(err, openaicompat.ErrInvalidResponse) {
			t.Fatalf("err = %v, want ErrInvalidResponse", err)
		}
	})

	t.Run("empty choices wraps ErrInvalidResponse", func(t *testing.T) {
		client, _ := fakeBackend(t, "/v1", http.StatusOK, `{"model":"m","choices":[]}`)
		_, err := client.Generate(context.Background(), ports.LLMRequest{})
		if !errors.Is(err, openaicompat.ErrInvalidResponse) {
			t.Fatalf("err = %v, want ErrInvalidResponse", err)
		}
	})

	t.Run("null content wraps ErrInvalidResponse", func(t *testing.T) {
		client, _ := fakeBackend(t, "/v1", http.StatusOK, `{"choices":[{"message":{"content":null}}]}`)
		_, err := client.Generate(context.Background(), ports.LLMRequest{})
		if !errors.Is(err, openaicompat.ErrInvalidResponse) {
			t.Fatalf("err = %v, want ErrInvalidResponse", err)
		}
	})

	t.Run("choice without a message wraps ErrInvalidResponse", func(t *testing.T) {
		client, _ := fakeBackend(t, "/v1", http.StatusOK, `{"choices":[{}]}`)
		_, err := client.Generate(context.Background(), ports.LLMRequest{})
		if !errors.Is(err, openaicompat.ErrInvalidResponse) {
			t.Fatalf("err = %v, want ErrInvalidResponse", err)
		}
	})

	t.Run("malformed JSONSchema is rejected before any call", func(t *testing.T) {
		client, err := openaicompat.New(openaicompat.Config{BaseURL: "http://127.0.0.1:0/v1"})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		// Not valid JSON, and valid JSON that is not a schema object: both are
		// caller errors, rejected as ErrInvalidRequest without an HTTP call.
		for _, bad := range []string{"{not valid", "true", "123", "[]", `"a string"`, "null", "   "} {
			_, err := client.Generate(context.Background(), ports.LLMRequest{JSONSchema: bad})
			if !errors.Is(err, openaicompat.ErrInvalidRequest) {
				t.Errorf("JSONSchema %q: err = %v, want ErrInvalidRequest", bad, err)
			}
		}
	})
}

func TestGenerateHonoursContextCancellation(t *testing.T) {
	client, _ := fakeBackend(t, "/v1", http.StatusOK, okResponse)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Generate(ctx, ports.LLMRequest{
		Messages: []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: "hi"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled reachable", err)
	}
	if !errors.Is(err, openaicompat.ErrBackend) {
		t.Fatalf("err = %v, want ErrBackend wrapper", err)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	cases := map[string]struct {
		cfg     openaicompat.Config
		wantErr bool
	}{
		"empty base url":        {openaicompat.Config{BaseURL: ""}, true},
		"whitespace base url":   {openaicompat.Config{BaseURL: "   "}, true},
		"non-http scheme":       {openaicompat.Config{BaseURL: "ftp://example.com"}, true},
		"missing host":          {openaicompat.Config{BaseURL: "http://"}, true},
		"unparseable":           {openaicompat.Config{BaseURL: "http://[::1"}, true},
		"valid cloud":           {openaicompat.Config{BaseURL: "https://api.openai.com/v1", APIKey: "k"}, false},
		"valid local no key":    {openaicompat.Config{BaseURL: "http://localhost:11434/v1"}, false},
		"valid trailing slash":  {openaicompat.Config{BaseURL: "https://api.openai.com/v1/"}, false},
		"valid api-key scheme":  {openaicompat.Config{BaseURL: "https://x.openai.azure.com/openai/deployments/gpt/", APIKey: "k", AuthScheme: openaicompat.AuthSchemeAPIKey}, false},
		"api-key scheme no key": {openaicompat.Config{BaseURL: "https://api.openai.com/v1", AuthScheme: openaicompat.AuthSchemeAPIKey}, false},
		"unknown auth scheme":   {openaicompat.Config{BaseURL: "https://api.openai.com/v1", APIKey: "k", AuthScheme: "basic"}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, err := openaicompat.New(tc.cfg)
			switch {
			case tc.wantErr && !errors.Is(err, openaicompat.ErrInvalidConfig):
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			case !tc.wantErr && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case !tc.wantErr && client == nil:
				t.Fatal("client is nil on success")
			}
		})
	}
}

// TestGenerateAuthScheme checks that the auth header follows Config.AuthScheme:
// Bearer by default, Azure's api-key header when selected, and none without a key.
func TestGenerateAuthScheme(t *testing.T) {
	newClientTo := func(t *testing.T, url string, cfg openaicompat.Config) *openaicompat.Client {
		t.Helper()
		cfg.BaseURL = url
		cfg.HTTPClient = &http.Client{}
		c, err := openaicompat.New(cfg)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	}

	cases := map[string]struct {
		cfg        openaicompat.Config
		wantAuth   string
		wantAPIKey string
	}{
		"bearer default": {openaicompat.Config{APIKey: "sk-123"}, "Bearer sk-123", ""},
		"api-key scheme": {openaicompat.Config{APIKey: "az-123", AuthScheme: openaicompat.AuthSchemeAPIKey}, "", "az-123"},
		"no key no auth": {openaicompat.Config{}, "", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := make(chan recordedRequest, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec <- recordedRequest{auth: r.Header.Get("Authorization"), apiKey: r.Header.Get("api-key")}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, okResponse)
			}))
			t.Cleanup(srv.Close)

			client := newClientTo(t, srv.URL+"/v1", tc.cfg)
			if _, err := client.Generate(context.Background(), ports.LLMRequest{
				Messages: []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: "hi"}},
			}); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			got := <-rec
			if got.auth != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", got.auth, tc.wantAuth)
			}
			if got.apiKey != tc.wantAPIKey {
				t.Errorf("api-key = %q, want %q", got.apiKey, tc.wantAPIKey)
			}
		})
	}
}

// TestGenerateIntegrationRealBackend runs against a real OpenAI-compatible
// backend provisioned by testutil/ollamalocal (Docker Ollama + a tiny model),
// or the one named by TESTMAKER_LLM_BASE_URL. It is skipped under -short or when
// Docker is unavailable. It asserts shape (non-empty content, model reported),
// never content, because a live model is nondeterministic.
func TestGenerateIntegrationRealBackend(t *testing.T) {
	baseURL := ollamalocal.Endpoint(t) // skips: -short, no Docker, or start failure
	model := ollamalocal.Model(t)

	client, err := openaicompat.New(openaicompat.Config{
		BaseURL:    baseURL,
		APIKey:     os.Getenv("TESTMAKER_LLM_API_KEY"),
		AuthScheme: openaicompat.AuthScheme(os.Getenv("TESTMAKER_LLM_AUTH_SCHEME")),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := client.Generate(ollamalocal.Context(t), ports.LLMRequest{
		Model:     model,
		Messages:  []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: "Reply with the single word: ok"}},
		MaxTokens: 16,
	})
	if errors.Is(err, context.DeadlineExceeded) {
		t.Skipf("backend did not respond within the test budget: %v", err)
	}
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty content from a real backend")
	}
	if resp.Model == "" {
		t.Error("expected the backend to report a model")
	}
}

// --- test helpers ----------------------------------------------------------

func rawKeys(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode wire keys: %v (%s)", err, body)
	}
	return m
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	return reflect.DeepEqual(av, bv)
}
