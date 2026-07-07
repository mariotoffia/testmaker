// Package ollamalocal provides shared test infrastructure for a real, small
// OpenAI-compatible LLM backend running in Docker (Ollama serving a tiny model
// on its /v1 endpoint). It lets the openaicompat adapter's integration test run
// against a genuine model without a hand-provisioned server.
//
// It shells out to the docker CLI and uses the standard library alone; it
// imports no testmaker package, so any adapter's test can depend on it without
// pulling a vendor SDK into the core.
//
// Usage in a test:
//
//	func TestAgainstRealBackend(t *testing.T) {
//	    base := ollamalocal.Endpoint(t) // "http://127.0.0.1:<port>/v1"
//	    model := ollamalocal.Model(t)   // e.g. "qwen2.5:0.5b"
//	    // ... build the client with base, request model ...
//	    resp, err := client.Generate(ollamalocal.Context(t), req)
//	}
//
// Context bounds backend calls by the test's own deadline (reserving time for
// teardown), so a slow model degrades into a skippable context.DeadlineExceeded
// instead of a hard test-timeout panic.
//
// The container starts on first Endpoint call and is shared across the test
// binary; pulled models are cached in a named Docker volume, so repeat runs are
// fast. The test is skipped when -short is set, Docker is unavailable, or the
// backend cannot be started — so `go test ./...` stays green on any machine.
//
// Setting TESTMAKER_LLM_BASE_URL points the tests at an existing backend and
// starts no container (mirror it with TESTMAKER_LLM_MODEL to pick the model).
package ollamalocal
