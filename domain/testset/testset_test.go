package testset_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
)

func TestSentinelsClassifyCorrectly(t *testing.T) {
	if testset.ErrUnknownTest.Class != shared.ClassNotFound {
		t.Fatalf("ErrUnknownTest class = %q, want %q", testset.ErrUnknownTest.Class, shared.ClassNotFound)
	}
	if testset.ErrInvalidTest.Class != shared.ClassInvalid {
		t.Fatalf("ErrInvalidTest class = %q, want %q", testset.ErrInvalidTest.Class, shared.ClassInvalid)
	}
}

func TestSentinelsMatchByCodeThroughWrapping(t *testing.T) {
	err := testset.ErrUnknownTest.With("id", "x")
	wrapped := fmt.Errorf("get: %w", err)

	if !errors.Is(wrapped, testset.ErrUnknownTest) {
		t.Fatal("errors.Is must match ErrUnknownTest by Code through wrapping")
	}
	if errors.Is(wrapped, testset.ErrInvalidTest) {
		t.Fatal("distinct sentinels must not cross-match")
	}
}

func TestSentinelsStayImmutable(t *testing.T) {
	_ = testset.ErrInvalidTest.WithMessage("custom").With("id", "x")

	if testset.ErrInvalidTest.Message != "invalid test" || len(testset.ErrInvalidTest.Context) != 0 {
		t.Fatalf("sentinel mutated by copy-on-write builder: %+v", testset.ErrInvalidTest)
	}
}
