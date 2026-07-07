package shared_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mariotoffia/testmaker/domain/shared"
)

func TestSentinelsStayImmutable(t *testing.T) {
	derived := shared.ErrInvalid.
		WithMessage("custom").
		Wrap(errors.New("cause")).
		With("key", "value")

	if shared.ErrInvalid.Message != "invalid value" || shared.ErrInvalid.Cause != nil || len(shared.ErrInvalid.Context) != 0 {
		t.Fatalf("sentinel mutated: %+v", shared.ErrInvalid)
	}
	if derived.Message != "custom" || derived.Cause == nil || derived.Context["key"] != "value" {
		t.Fatalf("builder chain lost state: %+v", derived)
	}
}

func TestMatchByCodeThroughWrapping(t *testing.T) {
	err := shared.ErrNotFound.WithMessage("gone").With("id", "x")
	wrapped := fmt.Errorf("outer: %w", err)

	if !errors.Is(wrapped, shared.ErrNotFound) {
		t.Fatal("errors.Is must match by Code through wrapping")
	}
	if errors.Is(wrapped, shared.ErrConflict) {
		t.Fatal("different Code must not match")
	}

	var te *shared.TestmakerError
	if !errors.As(wrapped, &te) || te.Class != shared.ClassNotFound {
		t.Fatalf("errors.As failed: %+v", te)
	}
}

func TestErrorStringIncludesCause(t *testing.T) {
	err := shared.ErrInvalid.Wrap(errors.New("boom"))
	if err.Unwrap() == nil {
		t.Fatal("Unwrap must expose the cause")
	}
	if err.Error() == "" || shared.ErrInvalid.Error() == err.Error() {
		t.Fatal("Error() must include the cause")
	}
}
