package session_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
)

func TestSentinelsClassifyCorrectly(t *testing.T) {
	if session.ErrUnknownSession.Class != shared.ClassNotFound {
		t.Fatalf("ErrUnknownSession class = %q, want %q", session.ErrUnknownSession.Class, shared.ClassNotFound)
	}
	if session.ErrInvalidSession.Class != shared.ClassInvalid {
		t.Fatalf("ErrInvalidSession class = %q, want %q", session.ErrInvalidSession.Class, shared.ClassInvalid)
	}
}

func TestSentinelsMatchByCodeThroughWrapping(t *testing.T) {
	err := session.ErrUnknownSession.With("id", "x")
	wrapped := fmt.Errorf("get: %w", err)

	if !errors.Is(wrapped, session.ErrUnknownSession) {
		t.Fatal("errors.Is must match ErrUnknownSession by Code through wrapping")
	}
	if errors.Is(wrapped, session.ErrInvalidSession) {
		t.Fatal("distinct sentinels must not cross-match")
	}
}

func TestSentinelsStayImmutable(t *testing.T) {
	_ = session.ErrInvalidSession.WithMessage("custom").With("id", "x")

	if session.ErrInvalidSession.Message != "invalid session" || len(session.ErrInvalidSession.Context) != 0 {
		t.Fatalf("sentinel mutated by copy-on-write builder: %+v", session.ErrInvalidSession)
	}
}
