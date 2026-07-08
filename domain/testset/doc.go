// Package testset is the "test authoring" bounded context — a composed Test:
// ordered sections, per-section and global timing, a delivery policy (fixed
// increasing-difficulty vs adaptive) and the item references that make up a
// runnable assessment.
//
// The aggregate root Test is built through NewTest (which validates its
// invariants) and crosses ports only as TestSnapshot. It references bank items
// by plain-string id (ItemRef.ItemID carries an item.ItemID): bounded contexts
// meet only through the shared kernel, never by importing each other.
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
