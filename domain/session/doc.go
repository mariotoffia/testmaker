// Package session is the "test execution" bounded context — a live or completed
// attempt at a Test: navigation state, per-item timing, captured responses and
// the adaptive path taken.
//
// SCAFFOLD: only the identifiers and DTO shells referenced by ports are present
// so the workspace compiles. The Session aggregate, Response value object and
// the execution state machine land in the "Renderer / Executor" block.
package session

import "github.com/mariotoffia/testmaker/domain/shared"

// Session-context sentinels.
var (
	// ErrInvalidSession is returned when a session snapshot violates an invariant.
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

// SessionSnapshot is a placeholder persistence/transport DTO for a session.
type SessionSnapshot struct {
	ID     SessionID
	TestID string
}
