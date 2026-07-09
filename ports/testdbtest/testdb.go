// Package testdbtest provides the reusable conformance suites that every
// "TestDb" repository implementation must pass. Running the same suite against
// every adapter (memory today, sqlite next) guarantees behavioural parity — the
// memorycatalog/sourcetest pattern applied to Test/Item/Session persistence.
package testdbtest

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
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
		// a composite, timed, difficulty-ordered snapshot — every nested value
		// object (sections, item refs, timing, derived families) must survive.
		want := compositeTestSnapshot(t, "gia", "GIA")
		if err := repo.SaveTest(ctx, want); err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := repo.GetTest(ctx, "gia")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
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
		mustSaveTest(t, repo, compositeTestSnapshot(t, "gia", "GIA"))
		// replace with a snapshot that differs in every mutable field (title,
		// policy, timing, sections); a store that forgets a column leaks the old
		// value here.
		replacement := adaptiveTestSnapshot(t, "gia", "GIA v2")
		mustSaveTest(t, repo, replacement)
		all, err := repo.ListTests(ctx, testset.TestFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("expected 1 after replace, got %d", len(all))
		}
		got, _ := repo.GetTest(ctx, "gia")
		if !reflect.DeepEqual(got, replacement) {
			t.Fatalf("replace failed:\n got %+v\nwant %+v", got, replacement)
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
		want := []testset.TestSnapshot{
			compositeTestSnapshot(t, "gia", "GIA"),
			compositeTestSnapshot(t, "matrigma", "Matrigma"),
			compositeTestSnapshot(t, "ravens", "Raven"),
		}
		mustSaveTest(t, repo, want[2])
		mustSaveTest(t, repo, want[0])
		mustSaveTest(t, repo, want[1])
		got, err := repo.ListTests(ctx, testset.TestFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("list mismatch:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		repo := newRepo()
		mustSaveTest(t, repo, compositeTestSnapshot(t, "gia", "GIA"))
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
		snap := compositeTestSnapshot(t, "gia", "GIA")
		want := compositeTestSnapshot(t, "gia", "GIA")
		mustSaveTest(t, repo, snap)
		// mutating a nested slice field after Save must not change stored state;
		// a store that aliased the caller's section/item slices would fail here.
		snap.Sections[0].Items[0].ItemID = "mutated"
		snap.Families[0] = "mutated"
		got, _ := repo.GetTest(ctx, "gia")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("store aliased caller input:\n got %+v\nwant %+v", got, want)
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
		want := mcItemSnapshot(t, "omib-1", "A2", 3)
		if err := repo.SaveItem(ctx, want); err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := repo.GetItem(ctx, "omib-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		// a full aggregate snapshot must survive the round-trip intact — every
		// nested value object (provenance, stimulus parts, options, key,
		// difficulty), not just the id.
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
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
		if err := repo.SaveItem(ctx, item.ItemSnapshot{Explanation: "no id"}); !errors.Is(err, item.ErrInvalidItem) {
			t.Fatalf("want ErrInvalidItem, got %v", err)
		}
	})

	t.Run("SaveReplacesSameID", func(t *testing.T) {
		repo := newRepo()
		mustSaveItem(t, repo, mcItemSnapshot(t, "omib-1", "A2", 2))
		// replace with a snapshot that differs in every mutable field; a store
		// that forgets a column (or a JSON blob it never rewrites) would leak the
		// old value here.
		replacement := numericItemSnapshot(t, "omib-1", "B1", 7)
		mustSaveItem(t, repo, replacement)

		all, err := repo.ListItems(ctx, item.ItemFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("expected 1 after replace, got %d", len(all))
		}
		got, _ := repo.GetItem(ctx, "omib-1")
		if !reflect.DeepEqual(got, replacement) {
			t.Fatalf("replace failed:\n got %+v\nwant %+v", got, replacement)
		}
	})

	t.Run("ListEmptyAndSortedByID", func(t *testing.T) {
		repo := newRepo()
		empty, err := repo.ListItems(ctx, item.ItemFilter{})
		if err != nil || len(empty) != 0 {
			t.Fatalf("list empty = %d, %v", len(empty), err)
		}
		// full snapshots inserted out of order; List must sort by id and preserve
		// every field (a dropped column / blob field passes an id-only check).
		want := []item.ItemSnapshot{
			mcItemSnapshot(t, "a", "A1", 1),
			mcItemSnapshot(t, "b", "A2", 4),
			mcItemSnapshot(t, "c", "A3", 9),
		}
		mustSaveItem(t, repo, want[2])
		mustSaveItem(t, repo, want[0])
		mustSaveItem(t, repo, want[1])

		got, err := repo.ListItems(ctx, item.ItemFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("list mismatch:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("ListFiltersByFamilyTypeDifficulty", func(t *testing.T) {
		repo := newRepo()
		// Three items differing in family, test type, difficulty, origin AND
		// redistributability, so a filter subtest fails if any field is ignored:
		//   a = A2 logical  band 2  fetched   conditional
		//   b = B1 numerical band 5  generated yes
		//   c = A1 logical  band 8  authored  no
		mustSaveItem(t, repo, mcItemSnapshotP(t, "a", "A2", 2, item.OriginFetched, shared.RedistConditional))
		mustSaveItem(t, repo, numericItemSnapshotP(t, "b", "B1", 5, item.OriginGenerated, shared.RedistYes))
		mustSaveItem(t, repo, mcItemSnapshotP(t, "c", "A1", 8, item.OriginAuthored, shared.RedistNo))

		cases := []struct {
			name    string
			filter  item.ItemFilter
			wantIDs []item.ItemID
		}{
			{"empty matches all", item.ItemFilter{}, []item.ItemID{"a", "b", "c"}},
			{"by family", item.ItemFilter{Families: []shared.AbilityFamily{shared.FamilyLogical}}, []item.ItemID{"a", "c"}},
			{"by test type", item.ItemFilter{TestTypes: []shared.TestTypeCode{"A2"}}, []item.ItemID{"a"}},
			{"min difficulty", item.ItemFilter{MinDifficulty: 5}, []item.ItemID{"b", "c"}},
			{"max difficulty", item.ItemFilter{MaxDifficulty: 2}, []item.ItemID{"a"}},
			{"difficulty band", item.ItemFilter{MinDifficulty: 3, MaxDifficulty: 6}, []item.ItemID{"b"}},
			{"by origin", item.ItemFilter{Origins: []item.Origin{item.OriginAuthored}}, []item.ItemID{"c"}},
			{"origin OR (multi-value)", item.ItemFilter{Origins: []item.Origin{item.OriginFetched, item.OriginAuthored}}, []item.ItemID{"a", "c"}},
			{"by redistributable", item.ItemFilter{Redistributable: []shared.Redistributable{shared.RedistYes}}, []item.ItemID{"b"}},
			{"family AND difficulty", item.ItemFilter{
				Families:      []shared.AbilityFamily{shared.FamilyLogical},
				MinDifficulty: 5,
			}, []item.ItemID{"c"}},
			{"family AND redistributable miss", item.ItemFilter{
				Families:        []shared.AbilityFamily{shared.FamilyNumerical},
				Redistributable: []shared.Redistributable{shared.RedistNo},
			}, []item.ItemID{}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, err := repo.ListItems(ctx, tc.filter)
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				gotIDs := make([]item.ItemID, len(got))
				for i, s := range got {
					gotIDs[i] = s.ID
				}
				if !reflect.DeepEqual(gotIDs, tc.wantIDs) {
					t.Fatalf("filter %+v: got ids %v, want %v", tc.filter, gotIDs, tc.wantIDs)
				}
			})
		}
	})

	t.Run("StoredSnapshotIsolatedFromInput", func(t *testing.T) {
		repo := newRepo()
		// two independent builds: want stays pristine while snap is mutated after
		// Save, so a store that aliased the caller's slices would fail here.
		snap := mcItemSnapshot(t, "omib-1", "A2", 3)
		want := mcItemSnapshot(t, "omib-1", "A2", 3)
		mustSaveItem(t, repo, snap)
		snap.Stimulus[0].Text = "mutated"
		snap.Options[0].Text = "mutated"

		got, _ := repo.GetItem(ctx, "omib-1")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("store aliased caller input:\n got %+v\nwant %+v", got, want)
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
		// a started, partially-answered session — every nested value object
		// (plan sections/items, timing, the presented item, captured responses,
		// normalized timestamps) must survive the round-trip.
		want := sessionSnapshot(t, "sess-1", "gia")
		if err := repo.SaveSession(ctx, want); err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := repo.GetSession(ctx, "sess-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
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
		mustSaveSession(t, repo, sessionSnapshot(t, "sess-1", "gia"))
		// a different plan under the same id must rewrite every field; the replace
		// is the next optimistic version (2) past the stored one (1).
		want := sessionSnapshot(t, "sess-1", "ravens")
		want.Version = 2
		mustSaveSession(t, repo, want)
		got, err := repo.GetSession(ctx, "sess-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("replace did not rewrite every field:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("OptimisticConcurrency", func(t *testing.T) {
		repo := newRepo()
		// a first save must start at version 1 (stored is absent == 0).
		v1 := sessionSnapshot(t, "sess-1", "gia")
		mustSaveSession(t, repo, v1)

		// a stale writer still holding version 1 is rejected — this is the guard
		// that stops two concurrent Answers (or an Answer racing a Complete) from
		// last-writer-wins clobbering the attempt.
		if err := repo.SaveSession(ctx, v1); !errors.Is(err, session.ErrSessionConflict) {
			t.Fatalf("stale save: want ErrSessionConflict, got %v", err)
		}
		// the writer that advanced from the stored version (1 -> 2) succeeds.
		next := sessionSnapshot(t, "sess-1", "gia")
		next.Version = 2
		mustSaveSession(t, repo, next)
		if got, _ := repo.GetSession(ctx, "sess-1"); got.Version != 2 {
			t.Fatalf("stored version = %d, want 2", got.Version)
		}
		// creating a brand-new id cannot jump ahead of version 1.
		ahead := sessionSnapshot(t, "sess-2", "gia")
		ahead.Version = 2
		if err := repo.SaveSession(ctx, ahead); !errors.Is(err, session.ErrSessionConflict) {
			t.Fatalf("create at version 2: want ErrSessionConflict, got %v", err)
		}
	})

	t.Run("ConcurrentSaveAtSameVersionRecordsOnce", func(t *testing.T) {
		repo := newRepo()
		mustSaveSession(t, repo, sessionSnapshot(t, "sess-1", "gia")) // stored version 1

		// The contended proof of the CAS: N writers that all loaded version 1
		// race to save version 2. The store's compare-and-swap must let exactly
		// one commit and reject the rest with ErrSessionConflict, whatever the
		// goroutine scheduling — this is what stops two concurrent Answers (or an
		// Answer racing a Complete) from clobbering the attempt. It is
		// deterministic because each store's compare-and-swap is atomic (memory:
		// under its mutex; sqlite: one guarded INSERT/UPDATE holding the write
		// lock, so across the file backing's real connection pool only the first
		// writer's UPDATE matches version 1), so the first writer advances stored
		// to 2 and every later writer sees 2 and needs 3. The HTTP surface
		// serializes too tightly to ever exercise the conflict path (a loser's own
		// answer is rejected first because the session has advanced), so the
		// deterministic guarantee is pinned here, at the store contract. Run under
		// -race it also guards the critical section itself.
		next := sessionSnapshot(t, "sess-1", "gia")
		next.Version = 2 // read-only, shared: SaveSession never mutates its input.

		const n = 16
		errs := make([]error, n)
		var wg sync.WaitGroup
		wg.Add(n)
		for i := range n {
			go func(i int) {
				defer wg.Done()
				errs[i] = repo.SaveSession(ctx, next)
			}(i)
		}
		wg.Wait()

		winners, conflicts := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				winners++
			case errors.Is(err, session.ErrSessionConflict):
				conflicts++
			default:
				t.Fatalf("unexpected save error: %v", err)
			}
		}
		if winners != 1 || conflicts != n-1 {
			t.Fatalf("contended CAS: %d winners / %d conflicts, want 1 / %d", winners, conflicts, n-1)
		}
		if got, _ := repo.GetSession(ctx, "sess-1"); got.Version != 2 {
			t.Fatalf("stored version = %d, want 2", got.Version)
		}
	})

	t.Run("StoredSnapshotIsolatedFromInput", func(t *testing.T) {
		repo := newRepo()
		snap := sessionSnapshot(t, "sess-1", "gia")
		want := sessionSnapshot(t, "sess-1", "gia") // an untouched twin to compare against
		mustSaveSession(t, repo, snap)
		// mutate the nested plan and response slices the caller still holds.
		snap.Sections[0].Items[0].Difficulty = 99
		snap.Responses[0].Correct = false

		got, _ := repo.GetSession(ctx, "sess-1")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("store aliased caller input:\n got %+v\nwant %+v", got, want)
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
	if err := s.SaveItem(ctx, mcItemSnapshot(t, "x", "A2", 3)); err != nil {
		t.Fatalf("save item: %v", err)
	}
	if err := s.SaveSession(ctx, session.SessionSnapshot{ID: "x", TestID: "T", Version: 1}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	if got, err := s.GetTest(ctx, "x"); err != nil || got.Title != "T" {
		t.Fatalf("test keyspace: got %+v, err %v", got, err)
	}
	if got, err := s.GetItem(ctx, "x"); err != nil || got.TestType != "A2" {
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

// compositeTestSnapshot builds a valid composite, timed, fixed-increasing test
// snapshot via the real aggregate, so the conformance suite exercises the same
// normalization (derived families, cloned sections) every store must preserve.
// The id is woven into the item refs so distinct tests hold distinct sections.
func compositeTestSnapshot(t *testing.T, id testset.TestID, title string) testset.TestSnapshot {
	t.Helper()
	p := string(id)
	tst, err := testset.NewTest(testset.TestSpec{
		ID:     id,
		Title:  title,
		Policy: testset.PolicyFixedIncreasing,
		Timing: testset.Timing{Total: 30 * time.Minute},
		Sections: []testset.Section{
			{
				Title:  "Reasoning",
				Family: shared.FamilyLogical,
				Timing: testset.Timing{Total: 6 * time.Minute, PerItem: 60 * time.Second},
				Items: []testset.ItemRef{
					{ItemID: p + "-log-1", Difficulty: 1},
					{ItemID: p + "-log-2", Difficulty: 3},
				},
			},
			{
				Title:  "Numeric",
				Family: shared.FamilyNumerical,
				Timing: testset.Timing{Total: 6 * time.Minute},
				Items: []testset.ItemRef{
					{ItemID: p + "-num-1", Difficulty: 2},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build composite test %s: %v", id, err)
	}
	return tst.Snapshot()
}

// adaptiveTestSnapshot builds a single-section adaptive test snapshot (differs
// from compositeTestSnapshot in policy, timing and shape) so replace tests can
// prove every field is rewritten.
func adaptiveTestSnapshot(t *testing.T, id testset.TestID, title string) testset.TestSnapshot {
	t.Helper()
	p := string(id)
	tst, err := testset.NewTest(testset.TestSpec{
		ID:     id,
		Title:  title,
		Policy: testset.PolicyAdaptive,
		Timing: testset.Timing{PerItem: 45 * time.Second},
		Sections: []testset.Section{
			{
				Title:  "Adaptive pool",
				Family: shared.FamilySpatial,
				Items: []testset.ItemRef{
					{ItemID: p + "-sp-1", Difficulty: 5},
					{ItemID: p + "-sp-2", Difficulty: 1},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build adaptive test %s: %v", id, err)
	}
	return tst.Snapshot()
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

// sessionSnapshot builds a started, partially-answered session snapshot via the
// real aggregate, so the conformance suite exercises the same normalization
// (cloned plan slices, UTC-normalized timestamps, captured responses) every
// store must preserve. The id is woven into the item ids so distinct sessions
// hold distinct plans. Timestamps are UTC so the snapshot is DeepEqual-stable
// across a JSON round-trip.
func sessionSnapshot(t *testing.T, id session.SessionID, testID string) session.SessionSnapshot {
	t.Helper()
	p := string(id)
	start := time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC)
	sess, err := session.NewSession(session.SessionSpec{
		ID:     id,
		TestID: testID,
		Policy: session.PolicyFixedIncreasing,
		Timing: session.Timing{Total: 20 * time.Minute},
		Sections: []session.PlanSection{{
			Title:  "Reasoning",
			Family: shared.FamilyLogical,
			Timing: session.Timing{PerItem: 60 * time.Second},
			Items: []session.PlanItem{
				{ItemID: p + "-a", Difficulty: 1},
				{ItemID: p + "-b", Difficulty: 2},
			},
		}},
	})
	if err != nil {
		t.Fatalf("build session %s: %v", id, err)
	}
	if err := sess.Begin(start); err != nil {
		t.Fatalf("begin session %s: %v", id, err)
	}
	if err := sess.Record(p+"-a", session.Answer{OptionID: "c"}, true, start.Add(15*time.Second)); err != nil {
		t.Fatalf("record session %s: %v", id, err)
	}
	snap := sess.Snapshot()
	// A session that has been persisted once carries optimistic version 1 (the
	// executor advances Version on every save; 0 marks a never-stored session).
	snap.Version = 1
	return snap
}

// mcItemSnapshot builds a valid multiple-choice item snapshot (non-nil option
// slice, one text + one figural stimulus part) via the real aggregate, so the
// conformance suite exercises the same normalization every store must preserve.
func mcItemSnapshot(t *testing.T, id item.ItemID, tt shared.TestTypeCode, band int) item.ItemSnapshot {
	t.Helper()
	return mcItemSnapshotP(t, id, tt, band, item.OriginFetched, shared.RedistConditional)
}

// mcItemSnapshotP is mcItemSnapshot with an explicit origin and redistributability
// so filter subtests can vary those provenance fields.
func mcItemSnapshotP(t *testing.T, id item.ItemID, tt shared.TestTypeCode, band int, origin item.Origin, redist shared.Redistributable) item.ItemSnapshot {
	t.Helper()
	it, err := item.NewItem(item.ItemSpec{
		ID:           id,
		Provenance:   item.Provenance{SourceID: "omib", Origin: origin, Redistributable: redist},
		TestType:     tt,
		Stimulus:     []item.StimulusPart{{Text: "which figure continues?"}, {MediaKind: item.MediaGrid, MediaRef: "blob://" + string(id)}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:   item.AnswerKey{OptionID: "c"},
		Explanation: "rotation by 90 degrees",
		Difficulty:  item.Difficulty{Band: band},
	})
	if err != nil {
		t.Fatalf("build mc item %s: %v", id, err)
	}
	return it.Snapshot()
}

// numericItemSnapshot builds a valid open-numeric item snapshot (nil option
// slice) via the real aggregate, covering the nil-vs-empty slice parity that
// memory and sqlite stores must both preserve under reflect.DeepEqual.
func numericItemSnapshot(t *testing.T, id item.ItemID, tt shared.TestTypeCode, band int) item.ItemSnapshot {
	t.Helper()
	return numericItemSnapshotP(t, id, tt, band, item.OriginFetched, shared.RedistConditional)
}

// numericItemSnapshotP is numericItemSnapshot with an explicit origin and
// redistributability.
func numericItemSnapshotP(t *testing.T, id item.ItemID, tt shared.TestTypeCode, band int, origin item.Origin, redist shared.Redistributable) item.ItemSnapshot {
	t.Helper()
	it, err := item.NewItem(item.ItemSpec{
		ID:           id,
		Provenance:   item.Provenance{SourceID: "omib", Origin: origin, Redistributable: redist},
		TestType:     tt,
		Stimulus:     []item.StimulusPart{{Text: "2, 4, 8, 16, ?"}},
		AnswerFormat: item.FormatOpenNumeric,
		AnswerKey:    item.AnswerKey{Numeric: 32, Tolerance: 0.5},
		Explanation:  "doubling series",
		Difficulty:   item.Difficulty{Band: band},
	})
	if err != nil {
		t.Fatalf("build numeric item %s: %v", id, err)
	}
	return it.Snapshot()
}
