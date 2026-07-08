// Package authoring is the application service (use-case layer) for putting
// items into the bank two ways: procedurally, by asking a ports.Generator for a
// batch and persisting it, and manually, by validating a hand-written item spec
// and persisting it. Both paths converge on ports.ItemRepository, so a generated
// item and an authored one are indistinguishable once stored.
//
// It orchestrates driven ports only (Generator, ItemRepository) and holds no
// rule-engine or storage knowledge of its own; the composition root injects the
// concrete adapters.
package authoring
