package testset

import (
	"slices"
	"strconv"
	"time"

	"github.com/mariotoffia/testmaker/domain/shared"
)

// DeliveryPolicy is how a test presents its items (closed set). It is data on
// the Test, not code in the executor, so one executor runs both kinds by reading
// the policy (DESIGN §3).
type DeliveryPolicy string

const (
	// PolicyFixedIncreasing presents each section's items in non-decreasing
	// difficulty order (the order is authored into the section).
	PolicyFixedIncreasing DeliveryPolicy = "fixed-increasing"
	// PolicyAdaptive selects the next item's difficulty from the running ability
	// estimate; a section is a difficulty-tagged pool, not a fixed sequence, and
	// must span at least two difficulty bands so the executor has room to adapt.
	PolicyAdaptive DeliveryPolicy = "adaptive"
)

// Valid reports whether the delivery policy is known.
func (p DeliveryPolicy) Valid() bool {
	return p == PolicyFixedIncreasing || p == PolicyAdaptive
}

// Timing is the time budget for a test or a section. A zero Duration means
// "unlimited" for that dimension; both must be non-negative. Speed is modeled
// explicitly here, not as an afterthought (DESIGN §3).
//
// ponytail: plain time.Duration data — forbidigo bans only the wall clock
// (time.Now/Sleep/…), which the executor injects as a clock in Block 8; a
// duration budget needs no clock.
type Timing struct {
	// Total is the global (test) or per-section time limit; 0 = untimed.
	Total time.Duration
	// PerItem is the per-item limit within scope; 0 = no per-item limit.
	PerItem time.Duration
}

func (t Timing) valid() bool {
	if t.Total < 0 || t.PerItem < 0 {
		return false
	}
	// A per-item cap larger than the whole budget can never bind — reject it as
	// an incoherent (likely swapped) configuration rather than store a dead limit.
	return t.Total == 0 || t.PerItem <= t.Total
}

// ItemRef points at one bank item placed in a section. ItemID is a plain string
// carrying an item.ItemID — the testset context cannot import the item context
// (they meet only through the shared kernel), the same rule item.Provenance
// follows for its source id. Difficulty is the item's band, copied onto the ref
// so a section can order and validate its items without the item aggregate.
//
// The band is a point-in-time snapshot taken when the test is composed: a
// composed Test is an immutable artifact, so a later recalibration of the bank
// item's difficulty does not (and must not) mutate a stored test. The executor
// reads difficulty from the ref, never by re-reading the bank.
type ItemRef struct {
	ItemID     string
	Difficulty int
}

// Section is one ordered part of a test: a family label, its own timing and the
// ordered items that make it up (DESIGN §3). A composite test combines several
// families across sections.
type Section struct {
	Title  string
	Family shared.AbilityFamily
	Timing Timing
	Items  []ItemRef
}

// TestSpec is the validated input to NewTest.
type TestSpec struct {
	ID       TestID
	Title    string
	Policy   DeliveryPolicy
	Timing   Timing
	Sections []Section
}

// Test is the aggregate root of the test-authoring context: a runnable,
// composed assessment. All state is private and validated on construction; it
// crosses ports only as a Snapshot.
type Test struct {
	id       TestID
	title    string
	policy   DeliveryPolicy
	timing   Timing
	families []shared.AbilityFamily // derived from section families, never accepted
	sections []Section
}

// NewTest validates a spec and returns the aggregate. The covered ability
// families are derived from the section families and are not accepted from
// callers, so a test's family set can never drift from its sections.
func NewTest(spec TestSpec) (*Test, *shared.TestmakerError) {
	if err := validateTest(spec); err != nil {
		return nil, err
	}
	return &Test{
		id:       spec.ID,
		title:    spec.Title,
		policy:   spec.Policy,
		timing:   spec.Timing,
		families: deriveSectionFamilies(spec.Sections),
		sections: cloneSections(spec.Sections),
	}, nil
}

// MustTest panics on invalid input; for tests and static fixtures only.
func MustTest(spec TestSpec) *Test {
	tst, err := NewTest(spec)
	if err != nil {
		panic(err)
	}
	return tst
}

// validateTest enforces the Test invariants (DDD §3.3): id and title present;
// ≥1 section; a valid policy and non-negative, coherent timing; each section
// non-empty with a valid family; every item ref keyed and difficulty-tagged;
// item ids unique across the whole test; under fixed-increasing delivery each
// section's items non-decreasing in difficulty; and under adaptive delivery each
// section spanning at least two difficulty bands.
func validateTest(spec TestSpec) *shared.TestmakerError {
	fail := func(msg string) *shared.TestmakerError {
		return ErrInvalidTest.WithMessage(msg).With("id", string(spec.ID))
	}
	switch {
	case spec.ID == "":
		return ErrInvalidTest.WithMessage("test id is required")
	case spec.Title == "":
		return fail("test title is required")
	case !spec.Policy.Valid():
		return fail("invalid delivery policy: " + string(spec.Policy))
	case !spec.Timing.valid():
		return fail("test timing is invalid: durations must be non-negative and any per-item cap must not exceed the total")
	case len(spec.Sections) == 0:
		return fail("at least one section is required")
	}

	seen := make(map[string]struct{})
	for i, sec := range spec.Sections {
		if err := validateSection(spec, i, sec, seen, fail); err != nil {
			return err
		}
	}
	return nil
}

// validateSection checks one section and accumulates the test-wide set of item
// ids (so the same item cannot be placed twice anywhere in the test).
func validateSection(
	spec TestSpec, idx int, sec Section, seen map[string]struct{},
	fail func(string) *shared.TestmakerError,
) *shared.TestmakerError {
	where := func(msg string) *shared.TestmakerError {
		return fail("section " + strconv.Itoa(idx) + ": " + msg)
	}
	if !sec.Family.Valid() {
		return where("invalid family: " + string(sec.Family))
	}
	if !sec.Timing.valid() {
		return where("section timing is invalid: durations must be non-negative and any per-item cap must not exceed the total")
	}
	if len(sec.Items) == 0 {
		return where("at least one item is required")
	}
	prev := 0
	bands := make(map[int]struct{})
	for _, ref := range sec.Items {
		if ref.ItemID == "" {
			return where("item ref id is required")
		}
		if ref.Difficulty < 1 {
			return where("item ref difficulty band must be >= 1: " + ref.ItemID)
		}
		if _, dup := seen[ref.ItemID]; dup {
			return where("duplicate item ref across the test: " + ref.ItemID)
		}
		seen[ref.ItemID] = struct{}{}
		bands[ref.Difficulty] = struct{}{}
		if spec.Policy == PolicyFixedIncreasing && ref.Difficulty < prev {
			return where("fixed-increasing items must be non-decreasing in difficulty: " + ref.ItemID)
		}
		prev = ref.Difficulty
	}
	// An adaptive section is a pool the executor climbs/descends through, so it
	// must offer at least two difficulty bands to move between — a single-band
	// pool cannot adapt (DDD §3.3).
	if spec.Policy == PolicyAdaptive && len(bands) < 2 {
		return where("adaptive sections need items spanning at least two difficulty bands")
	}
	return nil
}

// deriveSectionFamilies returns the distinct, sorted ability families the
// sections cover. Every section carries a valid family (NewTest enforces it), so
// this is a total projection of the sections.
func deriveSectionFamilies(sections []Section) []shared.AbilityFamily {
	seen := map[shared.AbilityFamily]struct{}{}
	for _, sec := range sections {
		seen[sec.Family] = struct{}{}
	}
	out := make([]shared.AbilityFamily, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

// cloneSections deep-copies the sections and their item-ref slices so stored
// state never aliases caller memory.
func cloneSections(sections []Section) []Section {
	if sections == nil {
		return nil
	}
	out := make([]Section, len(sections))
	for i, sec := range sections {
		sec.Items = slices.Clone(sec.Items)
		out[i] = sec
	}
	return out
}

// Accessors (immutable identity; copies for slices).

func (t *Test) ID() TestID { return t.id }

func (t *Test) Title() string { return t.title }

func (t *Test) Policy() DeliveryPolicy { return t.policy }

func (t *Test) Timing() Timing { return t.timing }

func (t *Test) Families() []shared.AbilityFamily { return slices.Clone(t.families) }

func (t *Test) Sections() []Section { return cloneSections(t.sections) }

// Snapshot returns the persistence/transport DTO for the aggregate. It carries
// the derived Families alongside the sections so a store can index or filter by
// family without re-deriving them.
func (t *Test) Snapshot() TestSnapshot {
	return TestSnapshot{
		ID:       t.id,
		Title:    t.title,
		Policy:   t.policy,
		Timing:   t.timing,
		Families: slices.Clone(t.families),
		Sections: cloneSections(t.sections),
	}
}

// RehydrateFromSnapshot rebuilds an aggregate from a trusted snapshot without
// re-validating (the snapshot is assumed to have passed NewTest previously). It
// deep-copies slice fields, so a snapshot round-tripped through
// Rehydrate().Snapshot() shares no memory with stored state — the property the
// stores rely on to hand back isolated snapshots.
func RehydrateFromSnapshot(s TestSnapshot) *Test {
	return &Test{
		id:       s.ID,
		title:    s.Title,
		policy:   s.Policy,
		timing:   s.Timing,
		families: slices.Clone(s.Families),
		sections: cloneSections(s.Sections),
	}
}

// TestSnapshot is the dependency-neutral DTO used to persist/transport a Test.
// It carries the derived Families alongside the sections so a store can index or
// filter by family without re-deriving them.
type TestSnapshot struct {
	ID       TestID
	Title    string
	Policy   DeliveryPolicy
	Timing   Timing
	Families []shared.AbilityFamily
	Sections []Section
}
