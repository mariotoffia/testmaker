package testset_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
)

// validSpec builds a composite, timed, difficulty-ordered fixed-increasing test:
// a logical section (bands 1,2,3) and a numerical section (bands 2,4) — enough to
// exercise families derivation, ordering and multi-section composition.
func validSpec() testset.TestSpec {
	return testset.TestSpec{
		ID:     "gia",
		Title:  "GIA",
		Policy: testset.PolicyFixedIncreasing,
		Timing: testset.Timing{Total: 30 * time.Minute},
		Sections: []testset.Section{
			{
				Title:  "Reasoning",
				Family: shared.FamilyLogical,
				Timing: testset.Timing{Total: 6 * time.Minute, PerItem: 60 * time.Second},
				Items: []testset.ItemRef{
					{ItemID: "log-1", Difficulty: 1},
					{ItemID: "log-2", Difficulty: 2},
					{ItemID: "log-3", Difficulty: 3},
				},
			},
			{
				Title:  "Numeric",
				Family: shared.FamilyNumerical,
				Timing: testset.Timing{Total: 6 * time.Minute},
				Items: []testset.ItemRef{
					{ItemID: "num-1", Difficulty: 2},
					{ItemID: "num-2", Difficulty: 4},
				},
			},
		},
	}
}

func TestNewTestValidComposite(t *testing.T) {
	tst, err := testset.NewTest(validSpec())
	if err != nil {
		t.Fatalf("NewTest: %v", err)
	}
	if tst.ID() != "gia" || tst.Title() != "GIA" {
		t.Fatalf("identity mismatch: %s/%s", tst.ID(), tst.Title())
	}
	if tst.Policy() != testset.PolicyFixedIncreasing {
		t.Fatalf("policy = %q", tst.Policy())
	}
	// families are derived from the sections and sorted (logical < numerical).
	want := []shared.AbilityFamily{shared.FamilyLogical, shared.FamilyNumerical}
	if got := tst.Families(); !reflect.DeepEqual(got, want) {
		t.Fatalf("families = %v, want %v", got, want)
	}
}

func TestNewTestSnapshotRoundTrips(t *testing.T) {
	tst := testset.MustTest(validSpec())
	snap := tst.Snapshot()
	// a full aggregate must survive Snapshot -> Rehydrate -> Snapshot intact.
	if got := testset.RehydrateFromSnapshot(snap).Snapshot(); !reflect.DeepEqual(got, snap) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, snap)
	}
}

// TestAccessorsAndSnapshotIsolateState proves neither Snapshot nor the slice
// accessors leak internal memory: mutating what they return cannot change the
// aggregate (the store isolation guarantee depends on this).
func TestAccessorsAndSnapshotIsolateState(t *testing.T) {
	tst := testset.MustTest(validSpec())

	snap := tst.Snapshot()
	snap.Sections[0].Items[0].ItemID = "mutated"
	snap.Families[0] = "mutated"
	if tst.Sections()[0].Items[0].ItemID != "log-1" {
		t.Fatal("Snapshot aliased internal section items")
	}
	if tst.Families()[0] != shared.FamilyLogical {
		t.Fatal("Snapshot aliased internal families")
	}

	secs := tst.Sections()
	secs[0].Items[0].Difficulty = 99
	if tst.Sections()[0].Items[0].Difficulty != 1 {
		t.Fatal("Sections() aliased internal item refs")
	}
}

func TestNewTestRejectsInvariantViolations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testset.TestSpec)
	}{
		{"empty id", func(s *testset.TestSpec) { s.ID = "" }},
		{"empty title", func(s *testset.TestSpec) { s.Title = "" }},
		{"invalid policy", func(s *testset.TestSpec) { s.Policy = "sideways" }},
		{"negative test timing", func(s *testset.TestSpec) { s.Timing.Total = -time.Second }},
		{"no sections", func(s *testset.TestSpec) { s.Sections = nil }},
		{"invalid section family", func(s *testset.TestSpec) { s.Sections[0].Family = "psychic" }},
		{"empty section family", func(s *testset.TestSpec) { s.Sections[0].Family = "" }},
		{"negative section timing", func(s *testset.TestSpec) { s.Sections[0].Timing.PerItem = -1 }},
		{"empty section", func(s *testset.TestSpec) { s.Sections[0].Items = nil }},
		{"empty item id", func(s *testset.TestSpec) { s.Sections[0].Items[0].ItemID = "" }},
		{"zero difficulty", func(s *testset.TestSpec) { s.Sections[0].Items[0].Difficulty = 0 }},
		{"duplicate item across sections", func(s *testset.TestSpec) {
			s.Sections[1].Items[0].ItemID = "log-1"
		}},
		{"fixed-increasing out of order", func(s *testset.TestSpec) {
			s.Sections[0].Items = []testset.ItemRef{
				{ItemID: "log-1", Difficulty: 5},
				{ItemID: "log-2", Difficulty: 2},
			}
		}},
		{"adaptive single band", func(s *testset.TestSpec) {
			s.Policy = testset.PolicyAdaptive
			s.Sections[0].Items = []testset.ItemRef{
				{ItemID: "log-1", Difficulty: 2},
				{ItemID: "log-2", Difficulty: 2},
			}
		}},
		{"section per-item exceeds total", func(s *testset.TestSpec) {
			s.Sections[0].Timing = testset.Timing{Total: time.Minute, PerItem: 2 * time.Minute}
		}},
		{"test per-item exceeds total", func(s *testset.TestSpec) {
			s.Timing = testset.Timing{Total: time.Minute, PerItem: time.Hour}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSpec()
			tc.mutate(&spec)
			if _, err := testset.NewTest(spec); !errors.Is(err, testset.ErrInvalidTest) {
				t.Fatalf("want ErrInvalidTest, got %v", err)
			}
		})
	}
}

// TestAdaptiveAllowsUnorderedDifficulty proves the non-decreasing rule is scoped
// to fixed-increasing delivery: an adaptive section is a difficulty-tagged pool,
// so its items need no particular order.
func TestAdaptiveAllowsUnorderedDifficulty(t *testing.T) {
	spec := validSpec()
	spec.Policy = testset.PolicyAdaptive
	spec.Sections[0].Items = []testset.ItemRef{
		{ItemID: "log-1", Difficulty: 5},
		{ItemID: "log-2", Difficulty: 1},
		{ItemID: "log-3", Difficulty: 3},
	}
	if _, err := testset.NewTest(spec); err != nil {
		t.Fatalf("adaptive section rejected valid pool: %v", err)
	}
}
