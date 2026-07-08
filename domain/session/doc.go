// Package session is the "test execution" bounded context — a live or completed
// attempt at a Test: navigation state, per-item timing, captured responses and
// the adaptive path taken.
//
// The aggregate is a clock-free state machine: the executor (app/execution)
// owns the clock and passes a `now time.Time` into every transition, so a taker
// session is deterministic under test. It cannot import the testset or item
// contexts (bounded contexts meet only through the shared kernel), so it carries
// its own plan value objects — plain-string item ids and time.Duration budgets —
// mirroring how testset.ItemRef carries a plain-string item id. Grading (which
// needs the item answer key) therefore happens in the executor, not here: the
// executor passes the graded `correct bool` into Record.
package session

import "github.com/mariotoffia/testmaker/domain/shared"

// Session-context sentinels.
var (
	// ErrInvalidSession is returned when a session snapshot violates an invariant
	// or a transition is attempted from an illegal state.
	ErrInvalidSession = &shared.TestmakerError{
		Code: "session.invalid", Class: shared.ClassInvalid, Message: "invalid session",
	}
	// ErrUnknownSession is returned when a session id is not in the repository.
	ErrUnknownSession = &shared.TestmakerError{
		Code: "session.unknown", Class: shared.ClassNotFound, Message: "unknown session",
	}
)

// SessionID uniquely identifies a test-taking session.
type SessionID string
