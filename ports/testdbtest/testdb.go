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
		// insert out of id order; List must return them sorted by id, and every
		// non-id field must survive the round-trip (a column dropped in the list
		// query would pass an id-only check).
		mustSaveTest(t, repo, testset.TestSnapshot{ID: "ravens", Title: "Raven"})
		mustSaveTest(t, repo, testset.TestSnapshot{ID: "gia", Title: "GIA"})
		mustSaveTest(t, repo, testset.TestSnapshot{ID: "matrigma", Title: "Matrigma"})
		got, err := repo.ListTests(ctx, testset.TestFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		want := []testset.TestSnapshot{
			{ID: "gia", Title: "GIA"},
			{ID: "matrigma", Title: "Matrigma"},
			{ID: "ravens", Title: "Raven"},
		}
		if len(got) != len(want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("list mismatch at %d: got %+v, want %+v", i, got[i], want[i])
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
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "omib-1", SourceID: "omib", Stem: "v1"})
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "omib-1", SourceID: "ravens", Stem: "v2"})
		all, err := repo.ListItems(ctx, item.ItemFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("expected 1 after replace, got %d", len(all))
		}
		// every mutable field must be replaced, not just stem: an ON CONFLICT that
		// forgets a column would leave a stale source_id here.
		got, _ := repo.GetItem(ctx, "omib-1")
		if got.SourceID != "ravens" || got.Stem != "v2" {
			t.Fatalf("replace failed: %+v", got)
		}
	})

	t.Run("ListEmptyAndSortedByID", func(t *testing.T) {
		repo := newRepo()
		empty, err := repo.ListItems(ctx, item.ItemFilter{})
		if err != nil || len(empty) != 0 {
			t.Fatalf("list empty = %d, %v", len(empty), err)
		}
		// full snapshots inserted out of order; List must sort by id and preserve
		// every field (a dropped source_id/stem would pass an id-only check).
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "c", SourceID: "sc", Stem: "stem-c"})
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "a", SourceID: "sa", Stem: "stem-a"})
		mustSaveItem(t, repo, item.ItemSnapshot{ID: "b", SourceID: "sb", Stem: "stem-b"})
		got, err := repo.ListItems(ctx, item.ItemFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		want := []item.ItemSnapshot{
			{ID: "a", SourceID: "sa", Stem: "stem-a"},
			{ID: "b", SourceID: "sb", Stem: "stem-b"},
			{ID: "c", SourceID: "sc", Stem: "stem-c"},
		}
		if len(got) != len(want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("list mismatch at %d: got %+v, want %+v", i, got[i], want[i])
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

// TestDB is the whole "TestDb" surface: one store serving all three
// repositories. RunSharedKeyspaceTests exercises the guarantee that they keep
// independent keyspaces, which a single-port suite cannot see.
type TestDB interface {
	ports.TestRepository
	ports.ItemRepository
	ports.SessionRepository
}

// RunSharedKeyspaceTests proves the three repositories backed by one store keep
// independent keyspaces: the same id in each repo does not collide, and deleting
// a test leaves an item/session with the same id untouched. It guards a wrong
// table name, a shared keyspace, or a cross-repo Delete in any implementation.
func RunSharedKeyspaceTests(t *testing.T, newStore func() TestDB) {
	t.Helper()

	ctx := context.Background()
	s := newStore()

	if err := s.SaveTest(ctx, testset.TestSnapshot{ID: "x", Title: "T"}); err != nil {
		t.Fatalf("save test: %v", err)
	}
	if err := s.SaveItem(ctx, item.ItemSnapshot{ID: "x", Stem: "I"}); err != nil {
		t.Fatalf("save item: %v", err)
	}
	if err := s.SaveSession(ctx, session.SessionSnapshot{ID: "x", TestID: "T"}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	if got, err := s.GetTest(ctx, "x"); err != nil || got.Title != "T" {
		t.Fatalf("test keyspace: got %+v, err %v", got, err)
	}
	if got, err := s.GetItem(ctx, "x"); err != nil || got.Stem != "I" {
		t.Fatalf("item keyspace: got %+v, err %v", got, err)
	}
	if got, err := s.GetSession(ctx, "x"); err != nil || got.TestID != "T" {
		t.Fatalf("session keyspace: got %+v, err %v", got, err)
	}

	// deleting the test must not touch the item/session with the same id
	if err := s.DeleteTest(ctx, "x"); err != nil {
		t.Fatalf("delete test: %v", err)
	}
	if _, err := s.GetItem(ctx, "x"); err != nil {
		t.Fatalf("item removed by test delete: %v", err)
	}
	if _, err := s.GetSession(ctx, "x"); err != nil {
		t.Fatalf("session removed by test delete: %v", err)
	}
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
