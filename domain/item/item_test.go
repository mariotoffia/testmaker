package item_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
)

func TestSentinelsClassifyCorrectly(t *testing.T) {
	if item.ErrUnknownItem.Class != shared.ClassNotFound {
		t.Fatalf("ErrUnknownItem class = %q, want %q", item.ErrUnknownItem.Class, shared.ClassNotFound)
	}
	if item.ErrInvalidItem.Class != shared.ClassInvalid {
		t.Fatalf("ErrInvalidItem class = %q, want %q", item.ErrInvalidItem.Class, shared.ClassInvalid)
	}
}

func TestSentinelsMatchByCodeThroughWrapping(t *testing.T) {
	err := item.ErrUnknownItem.With("id", "x")
	wrapped := fmt.Errorf("get: %w", err)

	if !errors.Is(wrapped, item.ErrUnknownItem) {
		t.Fatal("errors.Is must match ErrUnknownItem by Code through wrapping")
	}
	if errors.Is(wrapped, item.ErrInvalidItem) {
		t.Fatal("distinct sentinels must not cross-match")
	}
}

func TestSentinelsStayImmutable(t *testing.T) {
	_ = item.ErrInvalidItem.WithMessage("custom").With("id", "x")

	if item.ErrInvalidItem.Message != "invalid item" || len(item.ErrInvalidItem.Context) != 0 {
		t.Fatalf("sentinel mutated by copy-on-write builder: %+v", item.ErrInvalidItem)
	}
}
