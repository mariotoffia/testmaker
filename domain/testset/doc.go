// Package testset is the "test authoring" bounded context — a composed Test:
// ordered sections, per-section and global timing, delivery policy (fixed
// increasing-difficulty vs adaptive) and the item references that make up a
// runnable assessment.
//
// SCAFFOLD: only the identifiers and DTO shells referenced by ports are present
// so the workspace compiles. The Test aggregate, Section value object, timing
// and delivery-policy modeling land in the "Test Authoring" block.
package testset

import "github.com/mariotoffia/testmaker/domain/shared"

// Test-context sentinels.
var (
	// ErrInvalidTest is returned when a test snapshot violates an invariant.
	ErrInvalidTest = &shared.TestmakerError{
		Code: "testset.invalid", Class: shared.ClassInvalid, Message: "invalid test",
	}
	// ErrUnknownTest is returned when a test id is not in the repository.
	ErrUnknownTest = &shared.TestmakerError{
		Code: "testset.unknown", Class: shared.ClassNotFound, Message: "unknown test",
	}
)

// TestID uniquely identifies a composed test.
type TestID string

// TestSnapshot is a placeholder persistence/transport DTO for a composed test.
type TestSnapshot struct {
	ID    TestID
	Title string
}

// TestFilter is a placeholder query object for the test repository.
type TestFilter struct{}
