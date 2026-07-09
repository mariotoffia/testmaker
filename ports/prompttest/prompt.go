// Package prompttest provides a reusable conformance suite that every
// ports.PromptRepository implementation must pass, so all backends (in-memory
// and file today, sqlite/DynamoDB later) are guaranteed behavioural parity. It
// asserts the universal contract: put/get round-trip, unknown-id error,
// deterministic ByPurpose selection (highest Version, ties by smallest ID),
// list, put-replaces-by-id, delete (absent id is not an error), snapshot
// isolation (a returned snapshot never aliases stored slice state), and
// empty-id rejection.
package prompttest

import (
	"context"
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/domain/prompt"
	"github.com/mariotoffia/testmaker/ports"
)

// RunPromptRepositoryTests runs the full conformance suite against a fresh,
// empty repository produced by newRepo.
func RunPromptRepositoryTests(t *testing.T, newRepo func() ports.PromptRepository) {
	t.Helper()

	ctx := context.Background()

	t.Run("PutThenGet", func(t *testing.T) {
		repo := newRepo()
		want := sampleSnapshot(t, "extract-items", 1, prompt.PurposeExtraction)
		mustPut(t, repo, want)
		got, err := repo.Get(ctx, "extract-items")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.ID != want.ID || got.Version != want.Version ||
			got.Purpose != want.Purpose || got.Template != want.Template {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("GetUnknownReturnsErrUnknownPrompt", func(t *testing.T) {
		repo := newRepo()
		if _, err := repo.Get(ctx, "nope"); !errors.Is(err, prompt.ErrUnknownPrompt) {
			t.Fatalf("want ErrUnknownPrompt, got %v", err)
		}
	})

	t.Run("ByPurposeSelectsHighestVersionThenSmallestID", func(t *testing.T) {
		repo := newRepo()
		// Same purpose: highest Version wins; among equal-highest versions the
		// smallest id wins — deterministic across every adapter.
		mustPut(t, repo, sampleSnapshot(t, "z-old", 1, prompt.PurposeExtraction))
		mustPut(t, repo, sampleSnapshot(t, "m-new", 3, prompt.PurposeExtraction))
		mustPut(t, repo, sampleSnapshot(t, "a-new", 3, prompt.PurposeExtraction))
		got, err := repo.ByPurpose(ctx, prompt.PurposeExtraction)
		if err != nil {
			t.Fatalf("by purpose: %v", err)
		}
		if got.ID != "a-new" {
			t.Fatalf("ByPurpose selected %q, want a-new (highest version, smallest id)", got.ID)
		}
	})

	t.Run("ByPurposeUnknownReturnsErrUnknownPrompt", func(t *testing.T) {
		repo := newRepo()
		mustPut(t, repo, sampleSnapshot(t, "extract-items", 1, prompt.PurposeExtraction))
		if _, err := repo.ByPurpose(ctx, prompt.PurposeTranslation); !errors.Is(err, prompt.ErrUnknownPrompt) {
			t.Fatalf("want ErrUnknownPrompt for a purpose with no prompt, got %v", err)
		}
	})

	t.Run("ListEmptyThenAll", func(t *testing.T) {
		repo := newRepo()
		all, err := repo.List(ctx)
		if err != nil || len(all) != 0 {
			t.Fatalf("list empty = %d, %v", len(all), err)
		}
		mustPut(t, repo, sampleSnapshot(t, "extract-items", 1, prompt.PurposeExtraction))
		mustPut(t, repo, sampleSnapshot(t, "translate-item", 2, prompt.PurposeTranslation))
		all, err = repo.List(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(all) != 2 {
			t.Fatalf("list returned %d prompts, want 2", len(all))
		}
	})

	t.Run("PutReplacesSameID", func(t *testing.T) {
		repo := newRepo()
		mustPut(t, repo, sampleSnapshot(t, "extract-items", 1, prompt.PurposeExtraction))
		updated := sampleSnapshot(t, "extract-items", 2, prompt.PurposeExtraction)
		mustPut(t, repo, updated)
		got, err := repo.Get(ctx, "extract-items")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Version != 2 {
			t.Fatalf("replace failed: version = %d, want 2", got.Version)
		}
		all, _ := repo.List(ctx)
		if len(all) != 1 {
			t.Fatalf("expected 1 prompt after replace, got %d", len(all))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		repo := newRepo()
		mustPut(t, repo, sampleSnapshot(t, "extract-items", 1, prompt.PurposeExtraction))
		if err := repo.Delete(ctx, "extract-items"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := repo.Get(ctx, "extract-items"); !errors.Is(err, prompt.ErrUnknownPrompt) {
			t.Fatalf("expected gone, got %v", err)
		}
		// deleting an absent id is not an error
		if err := repo.Delete(ctx, "extract-items"); err != nil {
			t.Fatalf("delete absent: %v", err)
		}
	})

	t.Run("SnapshotIsolation", func(t *testing.T) {
		repo := newRepo()
		snap := sampleSnapshot(t, "extract-items", 1, prompt.PurposeExtraction)
		mustPut(t, repo, snap)
		got, _ := repo.Get(ctx, "extract-items")
		if len(got.Params) > 0 {
			got.Params[0] = "mutated"
		}
		again, _ := repo.Get(ctx, "extract-items")
		if len(again.Params) > 0 && again.Params[0] == "mutated" {
			t.Fatal("repository leaked internal slice state")
		}
	})

	t.Run("PutRejectsEmptyID", func(t *testing.T) {
		repo := newRepo()
		bad := sampleSnapshot(t, "extract-items", 1, prompt.PurposeExtraction)
		bad.ID = ""
		if err := repo.Put(ctx, bad); !errors.Is(err, prompt.ErrInvalidPrompt) {
			t.Fatalf("empty id: want ErrInvalidPrompt, got %v", err)
		}
	})
}

func mustPut(t *testing.T, repo ports.PromptRepository, snap prompt.Snapshot) {
	t.Helper()
	if err := repo.Put(context.Background(), snap); err != nil {
		t.Fatalf("put %s: %v", snap.ID, err)
	}
}

// sampleSnapshot builds a valid prompt snapshot via the domain aggregate, so the
// suite never fabricates state the constructor would reject.
func sampleSnapshot(t *testing.T, id prompt.PromptID, version int, purpose prompt.Purpose) prompt.Snapshot {
	t.Helper()
	p, err := prompt.NewPrompt(prompt.Spec{
		ID:       id,
		Version:  version,
		Purpose:  purpose,
		Template: "Extract items from {{.payload}} for source {{.source}}.",
		Params:   []string{"payload", "source"},
		Notes:    "sample",
	})
	if err != nil {
		t.Fatalf("build sample %s: %v", id, err)
	}
	return p.Snapshot()
}
