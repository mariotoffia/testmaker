// Package memoryprompts is an in-memory ports.PromptRepository: the tests +
// conformance baseline for the prompt store (mirroring memorycatalog). Prompts
// live in a map, are deep-copied on read/write so no internal slice is ever
// shared with a caller, and it is safe for concurrent use. ByPurpose resolves
// deterministically — highest Version wins, ties broken by smallest ID — so it
// is behaviourally identical to the file-backed default,
// adapters/native/llm/fileprompts.
package memoryprompts
