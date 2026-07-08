package authoring_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/app/authoring"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

// fakeTestRepo is an in-memory ports.TestRepository that can be told to fail
// writes. Reads go through the same map the writes populate, so a Compose that
// saves is immediately reloadable — the round-trip the "done when" needs.
type fakeTestRepo struct {
	saved    map[testset.TestID]testset.TestSnapshot
	writeErr error // non-nil = SaveTest fails
}

func newFakeTestRepo() *fakeTestRepo {
	return &fakeTestRepo{saved: map[testset.TestID]testset.TestSnapshot{}}
}

func (r *fakeTestRepo) SaveTest(_ context.Context, snap testset.TestSnapshot) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	r.saved[snap.ID] = snap
	return nil
}

func (r *fakeTestRepo) GetTest(_ context.Context, id testset.TestID) (testset.TestSnapshot, error) {
	s, ok := r.saved[id]
	if !ok {
		return testset.TestSnapshot{}, testset.ErrUnknownTest
	}
	return s, nil
}

func (r *fakeTestRepo) ListTests(_ context.Context, _ testset.TestFilter) ([]testset.TestSnapshot, error) {
	out := make([]testset.TestSnapshot, 0, len(r.saved))
	for _, s := range r.saved {
		out = append(out, s)
	}
	return out, nil
}

func (r *fakeTestRepo) DeleteTest(_ context.Context, id testset.TestID) error {
	delete(r.saved, id)
	return nil
}

var _ ports.TestRepository = (*fakeTestRepo)(nil)

// bankItem builds a valid snapshot of a given test type (its first letter fixes
// the family) and difficulty band, for seeding the fake bank.
func bankItem(t *testing.T, id item.ItemID, testType shared.TestTypeCode, band int) item.ItemSnapshot {
	t.Helper()
	it, err := item.NewItem(item.ItemSpec{
		ID:           id,
		Provenance:   item.Provenance{SourceID: "rulegen", Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
		TestType:     testType,
		Stimulus:     []item.StimulusPart{{Text: "stem"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:  item.AnswerKey{OptionID: "b"},
		Difficulty: item.Difficulty{Band: band},
	})
	if err != nil {
		t.Fatalf("bank item %q: %v", id, err)
	}
	return it.Snapshot()
}

// seedComposite fills a bank with logical (A*) and numerical (B*) items whose
// bands are deliberately out of order, so ordering is observable.
func seedComposite(t *testing.T) *fakeBank {
	t.Helper()
	bank := newFakeBank()
	for _, it := range []item.ItemSnapshot{
		bankItem(t, "log-hard", "A1", 3),
		bankItem(t, "log-easy", "A2", 1),
		bankItem(t, "log-mid", "A3", 2),
		bankItem(t, "num-hard", "B1", 2),
		bankItem(t, "num-easy", "B2", 1),
	} {
		if err := bank.SaveItem(context.Background(), it); err != nil {
			t.Fatalf("seed bank: %v", err)
		}
	}
	return bank
}

// TestComposeAuthorsStoresAndReloadsCompositeTest is the Block 7 "done when":
// a composite, timed, difficulty-ordered test is authored from the bank, stored
// and reloaded, with each section ordered by ascending difficulty.
func TestComposeAuthorsStoresAndReloadsCompositeTest(t *testing.T) {
	ctx := context.Background()
	bank := seedComposite(t)
	repo := newFakeTestRepo()
	svc := authoring.NewTestService(bank, repo)

	id, err := svc.Compose(ctx, authoring.ComposeSpec{
		ID:     "gia-composite",
		Title:  "Composite Aptitude",
		Policy: testset.PolicyFixedIncreasing,
		Timing: testset.Timing{Total: 30 * time.Minute},
		Sections: []authoring.SectionSpec{
			{Title: "Logical", Family: shared.FamilyLogical, Timing: testset.Timing{Total: 10 * time.Minute, PerItem: time.Minute}},
			{Title: "Numerical", Family: shared.FamilyNumerical, Timing: testset.Timing{Total: 8 * time.Minute}},
		},
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if id != "gia-composite" {
		t.Fatalf("id = %q, want gia-composite", id)
	}

	// Reload through the repository — the store round-trip, not the in-memory
	// aggregate.
	got, err := repo.GetTest(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Policy != testset.PolicyFixedIncreasing || got.Timing.Total != 30*time.Minute {
		t.Fatalf("test header wrong: %+v", got)
	}
	wantFamilies := []shared.AbilityFamily{shared.FamilyLogical, shared.FamilyNumerical}
	if len(got.Families) != 2 || got.Families[0] != wantFamilies[0] || got.Families[1] != wantFamilies[1] {
		t.Fatalf("families = %v, want %v", got.Families, wantFamilies)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(got.Sections))
	}

	// The logical section carries all three logical items ordered easy->hard.
	logical := got.Sections[0]
	assertRefOrder(t, logical.Items, []testset.ItemRef{
		{ItemID: "log-easy", Difficulty: 1},
		{ItemID: "log-mid", Difficulty: 2},
		{ItemID: "log-hard", Difficulty: 3},
	})
	numerical := got.Sections[1]
	assertRefOrder(t, numerical.Items, []testset.ItemRef{
		{ItemID: "num-easy", Difficulty: 1},
		{ItemID: "num-hard", Difficulty: 2},
	})
}

func assertRefOrder(t *testing.T, got, want []testset.ItemRef) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ref[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestComposeCapsSectionAtCount keeps only the Count lowest-difficulty matches.
func TestComposeCapsSectionAtCount(t *testing.T) {
	ctx := context.Background()
	bank := seedComposite(t)
	repo := newFakeTestRepo()
	svc := authoring.NewTestService(bank, repo)
	id, err := svc.Compose(ctx, authoring.ComposeSpec{
		ID:     "capped",
		Title:  "Capped",
		Policy: testset.PolicyFixedIncreasing,
		Sections: []authoring.SectionSpec{
			{Title: "Logical", Family: shared.FamilyLogical, Count: 2},
		},
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	got, err := repo.GetTest(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	assertRefOrder(t, got.Sections[0].Items, []testset.ItemRef{
		{ItemID: "log-easy", Difficulty: 1},
		{ItemID: "log-mid", Difficulty: 2},
	})
}

// TestComposeAdaptiveKeepsPoolOrdering shows adaptive delivery still composes
// (unordered difficulty is allowed; the bank order is sorted deterministically).
func TestComposeAdaptiveKeepsPoolOrdering(t *testing.T) {
	ctx := context.Background()
	bank := seedComposite(t)
	repo := newFakeTestRepo()
	svc := authoring.NewTestService(bank, repo)

	id, err := svc.Compose(ctx, authoring.ComposeSpec{
		ID:     "adaptive",
		Title:  "Adaptive",
		Policy: testset.PolicyAdaptive,
		Sections: []authoring.SectionSpec{
			{Title: "Logical", Family: shared.FamilyLogical},
		},
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if _, err := repo.GetTest(ctx, id); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

// TestComposeEmptySectionIsInvalidTest proves a section whose filter matches
// nothing is rejected by the aggregate and nothing is stored.
func TestComposeEmptySectionIsInvalidTest(t *testing.T) {
	ctx := context.Background()
	bank := seedComposite(t) // no spatial items
	repo := newFakeTestRepo()
	svc := authoring.NewTestService(bank, repo)

	_, err := svc.Compose(ctx, authoring.ComposeSpec{
		ID:     "empty",
		Title:  "Empty",
		Policy: testset.PolicyFixedIncreasing,
		Sections: []authoring.SectionSpec{
			{Title: "Spatial", Family: shared.FamilySpatial},
		},
	})
	if !errors.Is(err, testset.ErrInvalidTest) {
		t.Fatalf("want ErrInvalidTest, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Fatalf("stored %d tests, want 0 on invalid compose", len(repo.saved))
	}
}

// TestComposePropagatesBankError surfaces a bank read failure unchanged.
func TestComposePropagatesBankError(t *testing.T) {
	sentinel := &shared.TestmakerError{Code: "testdb.read", Class: shared.ClassUnavailable, Message: "boom"}
	bank := newFakeBank()
	bank.listErr = sentinel
	repo := newFakeTestRepo()
	svc := authoring.NewTestService(bank, repo)

	_, err := svc.Compose(context.Background(), authoring.ComposeSpec{
		ID:     "x",
		Title:  "X",
		Policy: testset.PolicyFixedIncreasing,
		Sections: []authoring.SectionSpec{
			{Title: "Logical", Family: shared.FamilyLogical},
		},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want bank error propagated, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Fatalf("stored a test despite bank error")
	}
}

// TestComposeAbortsOnStoreError surfaces a store write failure unchanged.
func TestComposeAbortsOnStoreError(t *testing.T) {
	writeErr := &shared.TestmakerError{Code: "testdb.write", Class: shared.ClassUnavailable, Message: "disk full"}
	bank := seedComposite(t)
	repo := newFakeTestRepo()
	repo.writeErr = writeErr
	svc := authoring.NewTestService(bank, repo)

	_, err := svc.Compose(context.Background(), authoring.ComposeSpec{
		ID:     "y",
		Title:  "Y",
		Policy: testset.PolicyFixedIncreasing,
		Sections: []authoring.SectionSpec{
			{Title: "Logical", Family: shared.FamilyLogical},
		},
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("want store error propagated, got %v", err)
	}
}
