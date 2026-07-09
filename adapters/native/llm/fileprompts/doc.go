// Package fileprompts is the default, file-backed ports.PromptRepository: one
// YAML file per prompt under a directory (id, version, purpose, params,
// template, notes). Prompts are reviewable, diffable seed data. Reads parse and
// validate every file through prompt.NewPrompt, so a malformed prompt on disk
// surfaces as an error rather than a silently broken template; writes are atomic
// (temp file + rename) so a crash never leaves a torn prompt under its id.
// ByPurpose resolves deterministically — highest Version wins, ties by smallest
// ID — identical to adapters/native/llm/memoryprompts.
package fileprompts
