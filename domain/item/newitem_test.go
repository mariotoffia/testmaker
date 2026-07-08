package item_test

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
)

// validMC returns a spec for a well-formed multiple-choice item; tests mutate a
// copy of it to exercise one invariant at a time.
func validMC() item.ItemSpec {
	return item.ItemSpec{
		ID:           "omib-1",
		Provenance:   item.Provenance{SourceID: "omib", Origin: item.OriginFetched, Redistributable: shared.RedistConditional},
		TestType:     "A2",
		Stimulus:     []item.StimulusPart{{Text: "which figure continues?"}, {MediaKind: item.MediaGrid, MediaRef: "blob://x"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:   item.AnswerKey{OptionID: "c"},
		Explanation: "rotation",
		Difficulty:  item.Difficulty{Band: 3},
	}
}

func TestNewItemDerivesFamilyAndSnapshots(t *testing.T) {
	cases := []struct {
		name       string
		spec       item.ItemSpec
		wantFamily shared.AbilityFamily
	}{
		{"multiple-choice", validMC(), shared.FamilyLogical},
		{
			name: "open-numeric",
			spec: item.ItemSpec{
				ID:           "num-1",
				Provenance:   item.Provenance{SourceID: "s", Origin: item.OriginAuthored, Redistributable: shared.RedistYes},
				TestType:     "B1",
				Stimulus:     []item.StimulusPart{{Text: "2,4,8,?"}},
				AnswerFormat: item.FormatOpenNumeric,
				AnswerKey:    item.AnswerKey{Numeric: 16},
				Difficulty:   item.Difficulty{Band: 1},
			},
			wantFamily: shared.FamilyNumerical,
		},
		{
			name: "true-false-cannotsay",
			spec: item.ItemSpec{
				ID:           "tf-1",
				Provenance:   item.Provenance{SourceID: "s", Origin: item.OriginGenerated, Redistributable: shared.RedistNo},
				TestType:     "C1",
				Stimulus:     []item.StimulusPart{{Text: "All cats are mammals."}},
				AnswerFormat: item.FormatTrueFalseCannotSay,
				AnswerKey:    item.AnswerKey{Verdict: item.VerdictTrue},
				Difficulty:   item.Difficulty{Band: 2},
			},
			wantFamily: shared.FamilyVerbal,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it, err := item.NewItem(tc.spec)
			if err != nil {
				t.Fatalf("NewItem: %v", err)
			}
			snap := it.Snapshot()
			if snap.Family != tc.wantFamily {
				t.Fatalf("family = %q, want %q", snap.Family, tc.wantFamily)
			}
			if snap.ID != tc.spec.ID || snap.TestType != tc.spec.TestType {
				t.Fatalf("identity mismatch: %+v", snap)
			}
			// a rehydrated snapshot must equal the original, preserving the
			// nil-vs-empty option slice (the parity both stores depend on).
			got := item.RehydrateFromSnapshot(snap).Snapshot()
			if !reflect.DeepEqual(got, snap) {
				t.Fatalf("rehydrate round-trip mismatch:\n got %+v\nwant %+v", got, snap)
			}
			if (tc.spec.AnswerFormat == item.FormatMultipleChoice) == (snap.Options == nil) {
				t.Fatalf("option-slice nilness wrong for %s: %#v", tc.spec.AnswerFormat, snap.Options)
			}
		})
	}
}

func TestNewItemAcceptsNumericTolerance(t *testing.T) {
	spec := item.ItemSpec{
		ID:           "num-tol",
		Provenance:   item.Provenance{SourceID: "s", Origin: item.OriginAuthored, Redistributable: shared.RedistYes},
		TestType:     "B1",
		Stimulus:     []item.StimulusPart{{Text: "1/3 as a decimal?"}},
		AnswerFormat: item.FormatOpenNumeric,
		AnswerKey:    item.AnswerKey{Numeric: 0.333, Tolerance: 0.01},
		Difficulty:   item.Difficulty{Band: 1},
	}
	it, err := item.NewItem(spec)
	if err != nil {
		t.Fatalf("NewItem with a valid tolerance: %v", err)
	}
	// the tolerance must survive the snapshot round-trip both stores rely on.
	snap := item.RehydrateFromSnapshot(it.Snapshot()).Snapshot()
	if snap.AnswerKey.Tolerance != 0.01 {
		t.Fatalf("tolerance = %v, want 0.01 (lost through snapshot)", snap.AnswerKey.Tolerance)
	}
}

func TestNewItemRejectsInvalidSpecs(t *testing.T) {
	mutate := func(f func(s *item.ItemSpec)) item.ItemSpec {
		s := validMC()
		f(&s)
		return s
	}
	numeric := func(f func(s *item.ItemSpec)) item.ItemSpec {
		s := item.ItemSpec{
			ID:           "n",
			Provenance:   item.Provenance{SourceID: "s", Origin: item.OriginAuthored, Redistributable: shared.RedistYes},
			TestType:     "B1",
			Stimulus:     []item.StimulusPart{{Text: "?"}},
			AnswerFormat: item.FormatOpenNumeric,
			AnswerKey:    item.AnswerKey{Numeric: 1},
			Difficulty:   item.Difficulty{Band: 1},
		}
		f(&s)
		return s
	}
	tf := func(f func(s *item.ItemSpec)) item.ItemSpec {
		s := item.ItemSpec{
			ID:           "t",
			Provenance:   item.Provenance{SourceID: "s", Origin: item.OriginAuthored, Redistributable: shared.RedistYes},
			TestType:     "C1",
			Stimulus:     []item.StimulusPart{{Text: "?"}},
			AnswerFormat: item.FormatTrueFalseCannotSay,
			AnswerKey:    item.AnswerKey{Verdict: item.VerdictFalse},
			Difficulty:   item.Difficulty{Band: 1},
		}
		f(&s)
		return s
	}

	cases := []struct {
		name string
		spec item.ItemSpec
	}{
		{"empty id", mutate(func(s *item.ItemSpec) { s.ID = "" })},
		{"empty source id", mutate(func(s *item.ItemSpec) { s.Provenance.SourceID = "" })},
		{"invalid origin", mutate(func(s *item.ItemSpec) { s.Provenance.Origin = "bogus" })},
		{"invalid redistributable", mutate(func(s *item.ItemSpec) { s.Provenance.Redistributable = "bogus" })},
		{"invalid test type", mutate(func(s *item.ItemSpec) { s.TestType = "Z9" })},
		{"invalid answer format", mutate(func(s *item.ItemSpec) { s.AnswerFormat = "bogus" })},
		{"band below one", mutate(func(s *item.ItemSpec) { s.Difficulty.Band = 0 })},
		{"no stimulus", mutate(func(s *item.ItemSpec) { s.Stimulus = nil })},
		{"empty stimulus part", mutate(func(s *item.ItemSpec) { s.Stimulus = []item.StimulusPart{{}} })},
		{"stimulus media without kind", mutate(func(s *item.ItemSpec) {
			s.Stimulus = []item.StimulusPart{{MediaRef: "blob://x"}}
		})},
		{"stimulus kind without ref", mutate(func(s *item.ItemSpec) {
			s.Stimulus = []item.StimulusPart{{Text: "t", MediaKind: item.MediaGrid}}
		})},
		{"too few options", mutate(func(s *item.ItemSpec) { s.Options = s.Options[:3] })},
		{"too many options", mutate(func(s *item.ItemSpec) {
			s.Options = append(s.Options, item.Option{ID: "e", Text: "E"}, item.Option{ID: "f", Text: "F"}, item.Option{ID: "g", Text: "G"})
		})},
		{"mc key is verdict", mutate(func(s *item.ItemSpec) { s.AnswerKey = item.AnswerKey{Verdict: item.VerdictTrue} })},
		{"mc key has numeric", mutate(func(s *item.ItemSpec) { s.AnswerKey.Numeric = 3 })},
		{"mc key has tolerance", mutate(func(s *item.ItemSpec) { s.AnswerKey.Tolerance = 0.5 })},
		{"option missing id", mutate(func(s *item.ItemSpec) { s.Options[0].ID = "" })},
		{"duplicate option id", mutate(func(s *item.ItemSpec) { s.Options[1].ID = "a" })},
		{"empty option", mutate(func(s *item.ItemSpec) { s.Options[0] = item.Option{ID: "a"} })},
		{"option media without kind", mutate(func(s *item.ItemSpec) {
			s.Options[0] = item.Option{ID: "a", MediaRef: "blob://x"}
		})},
		{"option kind without ref", mutate(func(s *item.ItemSpec) {
			s.Options[0] = item.Option{ID: "a", Text: "A", MediaKind: item.MediaImage}
		})},
		{"key references missing option", mutate(func(s *item.ItemSpec) { s.AnswerKey.OptionID = "zzz" })},
		{"open-numeric has options", numeric(func(s *item.ItemSpec) {
			s.Options = []item.Option{{ID: "a", Text: "A"}}
		})},
		{"open-numeric key has option id", numeric(func(s *item.ItemSpec) { s.AnswerKey.OptionID = "a" })},
		{"open-numeric key has verdict", numeric(func(s *item.ItemSpec) { s.AnswerKey.Verdict = item.VerdictTrue })},
		{"open-numeric key NaN", numeric(func(s *item.ItemSpec) { s.AnswerKey.Numeric = math.NaN() })},
		{"open-numeric key +Inf", numeric(func(s *item.ItemSpec) { s.AnswerKey.Numeric = math.Inf(1) })},
		{"open-numeric key -Inf", numeric(func(s *item.ItemSpec) { s.AnswerKey.Numeric = math.Inf(-1) })},
		{"open-numeric tolerance negative", numeric(func(s *item.ItemSpec) { s.AnswerKey.Tolerance = -0.1 })},
		{"open-numeric tolerance NaN", numeric(func(s *item.ItemSpec) { s.AnswerKey.Tolerance = math.NaN() })},
		{"open-numeric tolerance +Inf", numeric(func(s *item.ItemSpec) { s.AnswerKey.Tolerance = math.Inf(1) })},
		{"tf has options", tf(func(s *item.ItemSpec) { s.Options = []item.Option{{ID: "a", Text: "A"}} })},
		{"tf key has option id", tf(func(s *item.ItemSpec) { s.AnswerKey.OptionID = "a" })},
		{"tf key has numeric", tf(func(s *item.ItemSpec) { s.AnswerKey.Numeric = 1 })},
		{"tf key has tolerance", tf(func(s *item.ItemSpec) { s.AnswerKey.Tolerance = 0.5 })},
		{"tf key verdict invalid", tf(func(s *item.ItemSpec) { s.AnswerKey.Verdict = "maybe" })},
		{"tf key verdict empty", tf(func(s *item.ItemSpec) { s.AnswerKey.Verdict = "" })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it, err := item.NewItem(tc.spec)
			if err == nil {
				t.Fatalf("expected ErrInvalidItem, got item %+v", it.Snapshot())
			}
			if !errors.Is(err, item.ErrInvalidItem) {
				t.Fatalf("want ErrInvalidItem, got %v", err)
			}
		})
	}
}

func TestNewItemDoesNotAliasSpecSlices(t *testing.T) {
	spec := validMC()
	it, err := item.NewItem(spec)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	// mutating the caller's slices after construction must not reach the aggregate
	spec.Options[0].Text = "mutated"
	spec.Stimulus[0].Text = "mutated"
	if got := it.Snapshot(); got.Options[0].Text != "A" || got.Stimulus[0].Text != "which figure continues?" {
		t.Fatalf("aggregate aliased caller spec: %+v", got)
	}

	// a returned snapshot must be independent of the aggregate's own state
	s1 := it.Snapshot()
	s1.Options[0].Text = "mutated"
	if it.Snapshot().Options[0].Text != "A" {
		t.Fatal("Snapshot returned an aliased option slice")
	}
}
