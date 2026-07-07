// Package openaicompat is a ports.LLM adapter over the OpenAI-compatible chat
// completions HTTP API. One adapter covers cloud backends (OpenAI, Azure) and
// local servers (Ollama's /v1 endpoint, vLLM, LM Studio, llama.cpp server):
// they share the same wire contract and differ only by base URL, API key, and
// auth scheme (Config.AuthScheme selects Bearer or Azure's api-key header),
// all chosen in the composition root.
//
// It depends on the standard library alone (net/http, encoding/json) plus the
// domain shared kernel for its error sentinels, so it carries no vendor
// dependency. LLMRequest hints a backend cannot honour are dropped from the
// wire, never turned into an error; only transport failures, non-2xx statuses
// and unparseable bodies surface as errors (matched by Code via errors.Is).
package openaicompat
