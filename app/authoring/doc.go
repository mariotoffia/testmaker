// Package authoring is the application service (use-case layer) for two jobs:
// filling the item bank and composing tests from it.
//
// Items enter the bank two ways: procedurally, by asking a ports.Generator for a
// batch and persisting it, and manually, by validating a hand-written item spec
// and persisting it. Both paths converge on ports.ItemRepository, so a generated
// item and an authored one are indistinguishable once stored.
//
// TestService then composes those bank items into a runnable test: it queries
// the bank per section, orders the matches by difficulty, and builds the test
// through the invariant gate (testset.NewTest) before persisting it via
// ports.TestRepository.
//
// It orchestrates driven ports only (Generator, ItemRepository, TestRepository)
// and holds no rule-engine or storage knowledge of its own; the composition root
// injects the concrete adapters.
package authoring
