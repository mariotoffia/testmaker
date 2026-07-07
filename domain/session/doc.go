// Package session is the "test execution" bounded context — a live or completed
// attempt at a Test: navigation state, per-item timing, captured responses and
// the adaptive path taken.
//
// SCAFFOLD: only the identifiers and DTO shells referenced by ports are present
// so the workspace compiles. The Session aggregate, Response value object and
// the execution state machine land in the "Renderer / Executor" block.
package session

// SessionID uniquely identifies a test-taking session.
type SessionID string

// SessionSnapshot is a placeholder persistence/transport DTO for a session.
type SessionSnapshot struct {
	ID     SessionID
	TestID string
}
