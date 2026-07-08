// Package testdbtest provides the reusable conformance suites that every
// "TestDb" repository implementation must pass. Running the same suite against
// every adapter (memory today, sqlite next) guarantees behavioural parity — the
// memorycatalog/sourcetest pattern applied to Test/Item/Session persistence.
package testdbtest

import (
	"context"
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

// RunTestRepositoryTests runs the full conformance suite against a fresh, empty
// repository produced by newRepo.
func RunTestRepositoryTests(t *testing.T, newRepo func() ports.TestRepository) {
	t.Helper()

	ctx := context.Background()

	t.Run("SaveThenGet", func(t *testing.T) {
		repo := newRepo()
		want := testset.TestSnapshot{ID: "gia", Title: "GIA"}
		if err := repo.SaveTest(ctx, want); err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := repo.GetTest(ctx, "gia")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("GetUnknownReturnsErrUnknownTest", func(t *testing.T) {
		repo := newRepo()
		if _, err := repo.GetTest(ctx, "nope"); !errors.Is(err, testset.ErrUnknownTest) {
			t.Fatalf("want ErrUnknownTest, got %v", err)
		}
	})

	t.Run("SaveEmptyIDReturnsErrInvalidTest", func(t *testing.T) {
		repo := newRepo()
		if err := repo.SaveTest(ctx, testset.TestSnapshot{Title: "no id"}); !errors.Is(err, testset.ErrInvalidTest) {
			t.Fatalf("want ErrInvalidTest, got %v", err)
		}
	})

	t.Run("SaveReplacesSameID", func(t *testing.T) {
		repo := newRepo()
		mustSaveTest(t, repo, testset.TestSnapshot{ID: "gia", Title: "GIA"})
		mustSaveTest(t, repo, testset.TestSnapshot{ID: "gia", Title: "GIA v2"})
		all, err := repo.ListTests(ctx, testset.TestFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("expected 1 after replace, got %d", len(all))
		}
		got, _ := repo.GetTest(ctx, "gia")
		if got.Title != "GIA v2" {
			t.Fatalf("replace failed: %q", got.Title)
		}
	})

	t.Run("ListEmptyAndSortedByID", func(t *testing.T) {
		repo := newRepo()
		empty, err := repo.ListTests(ctx, testset.TestFilter{})
		if err != nil || len(empty) != 0 {
			t.Fatalf("list empty = %d, %v", len(empty), err)
		}
		// insert out of id order; List must return them sorted by id
		mustSaveTest(t, repo, testset.TestSnapshot{ID: "ravens", Title: "Raven"})
		mustSaveTest(t, repo, testset.TestSnapshot{ID: "gia", Title: "GIA"})
		mustSaveTest(t, repo, testset.TestSnapshot{ID: "matrigma", Title: "Matrigma"})
		got, err := repo.ListTests(ctx, testset.TestFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		ids := make([]testset.TestID, len(got))
		for i, s := range got {
			ids[i] = s.ID
		}
		want := []testset.TestID{"gia", "matrigma", "ravens"}
		if len(ids) != len(want) {
			t.Fatalf("got %v, want %v", ids, want)
		}
		for i := range want {
			if ids[i] != want[i] {
				t.Fatalf("unsorted list: got %v, want %v", ids, want)
			}
		}
	})

	t.Run("Delete", func(t *testing.T) {
		repo := newRepo()
		mustSaveTest(t, repo, testset.TestSnapshot{ID: "gia", Title: "GIA"})
		if err := repo.DeleteTest(ctx, "gia"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := repo.GetTest(ctx, "gia"); !errors.Is(err, testset.ErrUnknownTest) {
			t.Fatalf("expected gone, got %v", err)
		}
		// deleting an absent id is not an error
		if err := repo.DeleteTest(ctx, "gia"); err != nil {
			t.Fatalf("delete absent: %v", err)
		}
	})

	t.Run("StoredSnapshotIsolatedFromInput", func(t *testing.T) {
		repo := newRepo()
		snap := testset.TestSnapshot{ID: "gia", Title: "GIA"}
		mustSaveTest(t, repo, snap)
		// mutating the input after Save must not change stored state
		snap.Title = "mutated"
		got, _ := repo.GetTest(ctx, "gia")
		if got.Title != "GIA" {
			t.Fatalf("store aliased caller input: %q", got.Title)
		}
	})
}

// RunItemRepositoryTests runs the full conformance suite against a fresh, empty
// repository produced by newRepo.
func RunItemRepositoryTests(t *testing.T, newRepo func() ports.ItemRepository) {
	t.Helper()

	ctx := context.Background()

	t.Run("SaveThenGet", func(t *testing.T) {
		repo := newRepo()
		want := item.ItemSnapshot{ID: "omib-1", SourceID: "omib", Stem: "next figure?"}
		if err := repo.SaveItem(ctx, want); err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := repo.GetItem(ctx, "omib-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("GetUnknownReturnsErrUnknownItem", func(t *testing.T) {
		repo := newRepo()
		if _, err := repo.GetItem(ctx, "nope"); !errors.Is(err, item.ErrUnknownItem) {
			t.Fatalf("want ErrUnknownItem, got %v", err)
		}
	})

	t.Run("SaveEmptyIDReturnsErrInvalidItem", func(t *testing.T) {
		repo := newRepo()
		if err := repo.SaveItem(ctx, item.ItemSnapshot{Stem: "no id"}); !errors.Is(err, item.ErrInvalidItem) {
			t.Fatalf("want ErrInvalidItem, got %v", err)
		}
	})

	t.Run("SaveReplacesSameID", func(t *testing.T) {
		repo := newRepo()
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "omib-1", Stem: "v1"})
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "omib-1", Stem: "v2"})
		all, err := repo.ListItems(ctx, item.ItemFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("expected 1 after replace, got %d", len(all))
		}
		got, _ := repo.GetItem(ctx, "omib-1")
		if got.Stem != "v2" {
			t.Fatalf("replace failed: %q", got.Stem)
		}
	})

	t.Run("ListEmptyAndSortedByID", func(t *testing.T) {
		repo := newRepo()
		empty, err := repo.ListItems(ctx, item.ItemFilter{})
		if err != nil || len(empty) != 0 {
			t.Fatalf("list empty = %d, %v", len(empty), err)
		}
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "c"})
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "a"})
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "b"})
		got, err := repo.ListItems(ctx, item.ItemFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		want := []item.ItemID{"a", "b", "c"}
		if len(got) != len(want) {
			t.Fatalf("got %d items, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i] {
				t.Fatalf("unsorted list: got %+v, want %v", got, want)
			}
		}
	})

	t.Run("StoredSnapshotIsolatedFromInput", func(t *testing.T) {
		repo := newRepo()
		snap := item.ItemSnapshot{ID: "omib-1", Stem: "orig"}
		mustSaveItem(t, repo, snap)
		snap.Stem = "mutated"
		got, _ := repo.GetItem(ctx, "omib-1")
		if got.Stem != "orig" {
			t.Fatalf("store aliased caller input: %q", got.Stem)
		}
	})
}

// RunSessionRepositoryTests runs the full conformance suite against a fresh,
// empty repository produced by newRepo.
func RunSessionRepositoryTests(t *testing.T, newRepo func() ports.SessionRepository) {
	t.Helper()

	ctx := context.Background()

	t.Run("SaveThenGet", func(t *testing.T) {
		repo := newRepo()
		want := session.SessionSnapshot{ID: "sess-1", TestID: "gia"}
		if err := repo.SaveSession(ctx, want); err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := repo.GetSession(ctx, "sess-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("GetUnknownReturnsErrUnknownSession", func(t *testing.T) {
		repo := newRepo()
		if _, err := repo.GetSession(ctx, "nope"); !errors.Is(err, session.ErrUnknownSession) {
			t.Fatalf("want ErrUnknownSession, got %v", err)
		}
	})

	t.Run("SaveEmptyIDReturnsErrInvalidSession", func(t *testing.T) {
		repo := newRepo()
		if err := repo.SaveSession(ctx, session.SessionSnapshot{TestID: "gia"}); !errors.Is(err, session.ErrInvalidSession) {
			t.Fatalf("want ErrInvalidSession, got %v", err)
		}
	})

	t.Run("SaveReplacesSameID", func(t *testing.T) {
		repo := newRepo()
		mustSaveSession(t, repo, session.SessionSnapshot{ID: "sess-1", TestID: "gia"})
		mustSaveSession(t, repo, session.SessionSnapshot{ID: "sess-1", TestID: "ravens"})
		got, err := repo.GetSession(ctx, "sess-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.TestID != "ravens" {
			t.Fatalf("replace failed: %q", got.TestID)
		}
	})

	t.Run("StoredSnapshotIsolatedFromInput", func(t *testing.T) {
		repo := newRepo()
		snap := session.SessionSnapshot{ID: "sess-1", TestID: "gia"}
		mustSaveSession(t, repo, snap)
		snap.TestID = "mutated"
		got, _ := repo.GetSession(ctx, "sess-1")
		if got.TestID != "gia" {
			t.Fatalf("store aliased caller input: %q", got.TestID)
		}
	})
}

func mustSaveTest(t *testing.T, repo ports.TestRepository, snap testset.TestSnapshot) {
	t.Helper()
	if err := repo.SaveTest(context.Background(), snap); err != nil {
		t.Fatalf("save test %s: %v", snap.ID, err)
	}
}

func mustSaveItem(t *testing.T, repo ports.ItemRepository, snap item.ItemSnapshot) {
	t.Helper()
	if err := repo.SaveItem(context.Background(), snap); err != nil {
		t.Fatalf("save item %s: %v", snap.ID, err)
	}
}

func mustSaveSession(t *testing.T, repo ports.SessionRepository, snap session.SessionSnapshot) {
	t.Helper()
	if err := repo.SaveSession(context.Background(), snap); err != nil {
		t.Fatalf("save session %s: %v", snap.ID, err)
	}
}
