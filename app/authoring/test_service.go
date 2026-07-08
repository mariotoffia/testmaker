package authoring

import (
	"cmp"
	"context"
	"slices"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

// SectionSpec authors one section of a composed test: it pulls the bank's items
// of one ability family (optionally bounded by difficulty), keeps at most Count
// of them ordered by ascending difficulty, and lays them into a section under
// Title/Timing. Count <= 0 takes every match; a positive Count is an upper cap,
// so a section under-fills silently when the bank holds fewer matches. The
// family drives both the bank query and the section's homogeneous family label.
//
// Because selection keeps the lowest-difficulty matches, a capped adaptive
// section can collapse to a single band and be rejected by testset.NewTest
// (adaptive pools need a spread); widen the difficulty bounds or raise Count.
type SectionSpec struct {
	Title         string
	Family        shared.AbilityFamily
	Timing        testset.Timing
	MinDifficulty int
	MaxDifficulty int
	Count         int
}

// ComposeSpec is the authoring input for a full test: identity, delivery policy,
// global timing and the ordered sections to fill from the bank.
type ComposeSpec struct {
	ID       testset.TestID
	Title    string
	Policy   testset.DeliveryPolicy
	Timing   testset.Timing
	Sections []SectionSpec
}

// TestService is the test-authoring use-case: it composes a test from bank items
// and persists it. It orchestrates two driven ports only — the item bank it
// reads and the test repository it writes — and holds no storage or rule-engine
// knowledge of its own.
type TestService struct {
	bank  ports.ItemRepository
	tests ports.TestRepository
}

// NewTestService wires the item bank (read) and the test repository (write).
func NewTestService(bank ports.ItemRepository, tests ports.TestRepository) *TestService {
	return &TestService{bank: bank, tests: tests}
}

// Compose fills each section from the bank, builds the Test through the
// invariant gate (testset.NewTest) and stores it, returning the test's id. Items
// are ordered by ascending difficulty so a fixed-increasing test satisfies the
// non-decreasing rule; the aggregate rejects anything the query cannot satisfy
// (an empty section, a duplicate item across overlapping sections) as
// testset.ErrInvalidTest, and nothing is stored. A bank read or store write
// error is surfaced unchanged.
func (s *TestService) Compose(ctx context.Context, spec ComposeSpec) (testset.TestID, error) {
	sections := make([]testset.Section, 0, len(spec.Sections))
	for _, ss := range spec.Sections {
		refs, err := s.selectRefs(ctx, ss)
		if err != nil {
			return "", err
		}
		sections = append(sections, testset.Section{
			Title:  ss.Title,
			Family: ss.Family,
			Timing: ss.Timing,
			Items:  refs,
		})
	}

	test, verr := testset.NewTest(testset.TestSpec{
		ID:       spec.ID,
		Title:    spec.Title,
		Policy:   spec.Policy,
		Timing:   spec.Timing,
		Sections: sections,
	})
	if verr != nil {
		return "", verr
	}
	if err := s.tests.SaveTest(ctx, test.Snapshot()); err != nil {
		return "", err
	}
	return test.ID(), nil
}

// selectRefs queries the bank for one section and maps the matches to item refs
// ordered by ascending difficulty band, capped at ss.Count.
func (s *TestService) selectRefs(ctx context.Context, ss SectionSpec) ([]testset.ItemRef, error) {
	snaps, err := s.bank.ListItems(ctx, item.ItemFilter{
		Families:      []shared.AbilityFamily{ss.Family},
		MinDifficulty: ss.MinDifficulty,
		MaxDifficulty: ss.MaxDifficulty,
	})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(snaps, func(a, b item.ItemSnapshot) int {
		return cmp.Compare(a.Difficulty.Band, b.Difficulty.Band)
	})
	if ss.Count > 0 && len(snaps) > ss.Count {
		snaps = snaps[:ss.Count]
	}
	// ponytail: Count is an upper cap only — a section under-fills silently when
	// the bank has fewer matches. No MinCount/exact-count enforcement until a
	// fixed-length standardized battery needs it (Block 10+).

	refs := make([]testset.ItemRef, len(snaps))
	for i, snap := range snaps {
		refs[i] = testset.ItemRef{ItemID: string(snap.ID), Difficulty: snap.Difficulty.Band}
	}
	return refs, nil
}
