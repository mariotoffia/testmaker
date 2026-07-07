// Package testset is the "test authoring" bounded context — a composed Test:
// ordered sections, per-section and global timing, delivery policy (fixed
// increasing-difficulty vs adaptive) and the item references that make up a
// runnable assessment.
//
// SCAFFOLD: only the identifiers and DTO shells referenced by ports are present
// so the workspace compiles. The Test aggregate, Section value object, timing
// and delivery-policy modeling land in the "Test Authoring" block.
package testset

// TestID uniquely identifies a composed test.
type TestID string

// TestSnapshot is a placeholder persistence/transport DTO for a composed test.
type TestSnapshot struct {
	ID    TestID
	Title string
}

// TestFilter is a placeholder query object for the test repository.
type TestFilter struct{}
