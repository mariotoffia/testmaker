// Package shared is the shared kernel of the Testmaker domain: types and
// errors that every bounded context may use. It depends only on the standard
// library (it is the innermost ring).
package shared

import (
	"errors"
	"fmt"
)

// ErrorClass groups errors by how a caller should react.
type ErrorClass string

const (
	// ClassInvalid signals a violated invariant / bad input (permanent).
	ClassInvalid ErrorClass = "invalid"
	// ClassNotFound signals a missing entity.
	ClassNotFound ErrorClass = "not_found"
	// ClassConflict signals a uniqueness / state conflict.
	ClassConflict ErrorClass = "conflict"
	// ClassUnavailable signals a transient failure worth retrying.
	ClassUnavailable ErrorClass = "unavailable"
	// ClassUnsupported signals a capability that is not implemented.
	ClassUnsupported ErrorClass = "unsupported"
)

// ErrorCode is a stable, machine-matchable identifier for an error.
type ErrorCode string

// TestmakerError is the single structured error type of the domain. Matching is
// by Code (see Is); Message/Context/Cause add human and diagnostic detail. The
// fluent builders copy-on-write so package-level sentinels stay immutable.
type TestmakerError struct {
	Code    ErrorCode
	Class   ErrorClass
	Message string
	Cause   error
	Context map[string]any
}

// Error implements the error interface.
func (e *TestmakerError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped cause.
func (e *TestmakerError) Unwrap() error { return e.Cause }

// Is matches two TestmakerErrors on Code, enabling errors.Is against sentinels.
func (e *TestmakerError) Is(target error) bool {
	var t *TestmakerError
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// WithMessage returns a copy with a replaced message.
func (e *TestmakerError) WithMessage(msg string) *TestmakerError {
	c := e.clone()
	c.Message = msg
	return c
}

// WithMessagef returns a copy with a formatted message.
func (e *TestmakerError) WithMessagef(format string, args ...any) *TestmakerError {
	return e.WithMessage(fmt.Sprintf(format, args...))
}

// Wrap returns a copy carrying the given cause.
func (e *TestmakerError) Wrap(cause error) *TestmakerError {
	c := e.clone()
	c.Cause = cause
	return c
}

// With returns a copy with an additional context key/value.
func (e *TestmakerError) With(key string, value any) *TestmakerError {
	c := e.clone()
	if c.Context == nil {
		c.Context = map[string]any{}
	}
	c.Context[key] = value
	return c
}

func (e *TestmakerError) clone() *TestmakerError {
	ctx := make(map[string]any, len(e.Context))
	for k, v := range e.Context {
		ctx[k] = v
	}
	return &TestmakerError{Code: e.Code, Class: e.Class, Message: e.Message, Cause: e.Cause, Context: ctx}
}

// newSentinel is a helper for declaring package-level sentinels.
func newSentinel(code ErrorCode, class ErrorClass, msg string) *TestmakerError {
	return &TestmakerError{Code: code, Class: class, Message: msg}
}

// Shared sentinels. Bounded contexts declare their own next to their model.
var (
	// ErrInvalid is a generic invariant violation.
	ErrInvalid = newSentinel("shared.invalid", ClassInvalid, "invalid value")
	// ErrNotFound is a generic missing-entity error.
	ErrNotFound = newSentinel("shared.not_found", ClassNotFound, "not found")
	// ErrConflict is a generic conflict error.
	ErrConflict = newSentinel("shared.conflict", ClassConflict, "conflict")
	// ErrUnsupported marks a not-yet-implemented capability.
	ErrUnsupported = newSentinel("shared.unsupported", ClassUnsupported, "unsupported")
)
