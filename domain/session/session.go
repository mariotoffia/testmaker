package session

import (
	"slices"
	"strconv"
	"time"

	"github.com/mariotoffia/testmaker/domain/shared"
)

// State is the lifecycle state of a session (closed set). The legal path is
// created → in-progress → completed | abandoned; every transition method rejects
// a call from any other state.
type State string

const (
	// StateCreated: constructed but not yet started (no items presented).
	StateCreated State = "created"
	// StateInProgress: started; an item is presented and answers are captured.
	StateInProgress State = "in-progress"
	// StateCompleted: finished normally (the taker or executor closed it).
	StateCompleted State = "completed"
	// StateAbandoned: ended without completing (e.g. the global time budget ran out).
	StateAbandoned State = "abandoned"
)

// Policy is how the session presents its items (closed set). It mirrors
// testset.DeliveryPolicy by value — the session context cannot import testset,
// so the string constants are kept identical and the executor maps one to the
// other when it builds the SessionSpec.
type Policy string

const (
	// PolicyFixedIncreasing presents each section's items in authored order.
	PolicyFixedIncreasing Policy = "fixed-increasing"
	// PolicyAdaptive picks, within a section, the undelivered item whose band is
	// closest to a running target that climbs on correct answers and drops on
	// wrong ones.
	PolicyAdaptive Policy = "adaptive"
)

// Valid reports whether the policy is known.
func (p Policy) Valid() bool { return p == PolicyFixedIncreasing || p == PolicyAdaptive }

// Timing is a time budget (global or per-section). A zero Duration means
// "unlimited" for that dimension. It mirrors testset.Timing by value.
type Timing struct {
	// Total is the global (session) or per-section limit; 0 = untimed.
	Total time.Duration
	// PerItem is the per-item limit within scope; 0 = no per-item limit.
	PerItem time.Duration
}

// PlanItem is one bank item placed in a section: a plain-string item id and the
// difficulty band copied from the composed test (mirrors testset.ItemRef).
type PlanItem struct {
	ItemID     string
	Difficulty int
}

// PlanSection is one part of the plan the session runs: a family label, its
// timing and its ordered items. It is a point-in-time copy of the test's
// structure, so a later change to the test's shape never mutates a running
// session. The answer key is deliberately not copied here: the executor grades
// against the live item bank (see app/execution.graded), so freezing keys into
// the plan is a Block 9 fairness decision, not a Block 8 one.
type PlanSection struct {
	Title  string
	Family shared.AbilityFamily
	Timing Timing
	Items  []PlanItem
}

// Answer is the taker's answer, interpreted by the item's answer format:
// OptionID for multiple-choice, Numeric for open-numeric, Verdict for
// true-false-cannotsay. The session stores it verbatim; the executor grades it
// against the item's answer key (the session cannot import the item context).
type Answer struct {
	OptionID string
	Numeric  float64
	Verdict  string
}

// Presented is the item currently in front of the taker. A zero value
// (ItemID == "") means no item is presented — the session has not started, has
// exhausted its plan, or has ended.
type Presented struct {
	ItemID      string
	Difficulty  int
	Section     int
	DeliveredAt time.Time
}

// none reports whether no item is presented.
func (p Presented) none() bool { return p.ItemID == "" }

// Response is one captured answer: which item (and where it sat in the plan),
// the taker's answer, how long they took, and whether it was correct.
type Response struct {
	ItemID     string
	Difficulty int
	Section    int
	Answer     Answer
	Elapsed    time.Duration
	Correct    bool
}

// SessionSpec is the validated input to NewSession. The executor builds it from
// a composed test's snapshot.
type SessionSpec struct {
	ID       SessionID
	TestID   string
	Policy   Policy
	Timing   Timing
	Sections []PlanSection
}

// Session is the aggregate root of the execution context: one attempt at a test.
// All state is private and mutated only through the transition methods, which
// enforce the lifecycle and timing invariants; it crosses ports only as a
// Snapshot.
type Session struct {
	id        SessionID
	testID    string
	policy    Policy
	state     State
	timing    Timing
	sections  []PlanSection
	startedAt time.Time
	endedAt   time.Time
	presented Presented
	responses []Response
}

// NewSession validates a spec and returns a session in the created state. It
// does not touch the clock — the plan is fixed at construction and time starts
// at Begin.
func NewSession(spec SessionSpec) (*Session, *shared.TestmakerError) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	return &Session{
		id:       spec.ID,
		testID:   spec.TestID,
		policy:   spec.Policy,
		state:    StateCreated,
		timing:   spec.Timing,
		sections: clonePlanSections(spec.Sections),
	}, nil
}

// MustSession panics on invalid input; for tests and static fixtures only.
func MustSession(spec SessionSpec) *Session {
	s, err := NewSession(spec)
	if err != nil {
		panic(err)
	}
	return s
}

// validateSpec enforces the plan invariants: an id and test id; a valid policy
// and coherent timing; at least one section, each with a valid family, coherent
// timing and at least one keyed, difficulty-tagged item; and item ids unique
// across the whole plan (a taker never sees the same item twice). These mirror
// the testset.Test invariants the plan was built from, re-checked here so a
// hand-built spec cannot smuggle in a broken plan.
func validateSpec(spec SessionSpec) *shared.TestmakerError {
	fail := func(msg string) *shared.TestmakerError {
		return ErrInvalidSession.WithMessage(msg).With("id", string(spec.ID))
	}
	switch {
	case spec.ID == "":
		return ErrInvalidSession.WithMessage("session id is required")
	case spec.TestID == "":
		return fail("test id is required")
	case !spec.Policy.Valid():
		return fail("invalid delivery policy: " + string(spec.Policy))
	case !spec.Timing.valid():
		return fail("session timing is invalid: durations must be non-negative and any per-item cap must not exceed the total")
	case len(spec.Sections) == 0:
		return fail("at least one section is required")
	}
	seen := make(map[string]struct{})
	for i, sec := range spec.Sections {
		if err := validatePlanSection(i, sec, seen, fail); err != nil {
			return err
		}
	}
	return nil
}

func validatePlanSection(
	idx int, sec PlanSection, seen map[string]struct{}, fail func(string) *shared.TestmakerError,
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
	for _, it := range sec.Items {
		if it.ItemID == "" {
			return where("plan item id is required")
		}
		if it.Difficulty < 1 {
			return where("plan item difficulty band must be >= 1: " + it.ItemID)
		}
		if _, dup := seen[it.ItemID]; dup {
			return where("duplicate item across the plan: " + it.ItemID)
		}
		seen[it.ItemID] = struct{}{}
	}
	return nil
}

func (t Timing) valid() bool {
	if t.Total < 0 || t.PerItem < 0 {
		return false
	}
	return t.Total == 0 || t.PerItem <= t.Total
}

// Begin starts the session: it stamps the start time, moves to in-progress and
// presents the first item. now is the executor's clock reading.
func (s *Session) Begin(now time.Time) *shared.TestmakerError {
	if s.state != StateCreated {
		return ErrInvalidSession.WithMessagef("cannot begin a %s session", s.state).With("id", string(s.id))
	}
	s.state = StateInProgress
	s.startedAt = now
	s.present(now)
	return nil
}

// Record captures the taker's answer to the presented item and advances the
// session. correct is the executor's grading of ans against the item key. It
// enforces that the session is in progress, that itemID targets the presented
// item, and that now does not run backwards from when the item was delivered.
// Answering the last remaining item leaves no item presented but keeps the
// session in progress until Complete is called.
func (s *Session) Record(itemID string, ans Answer, correct bool, now time.Time) *shared.TestmakerError {
	switch {
	case s.state != StateInProgress:
		return ErrInvalidSession.WithMessagef("cannot record in a %s session", s.state).With("id", string(s.id))
	case s.presented.none():
		return ErrInvalidSession.WithMessage("no item is presented").With("id", string(s.id))
	case itemID != s.presented.ItemID:
		return ErrInvalidSession.
			WithMessagef("answer targets %q but %q is presented", itemID, s.presented.ItemID).
			With("id", string(s.id))
	case now.Before(s.presented.DeliveredAt):
		return ErrInvalidSession.WithMessage("clock ran backwards before the item was delivered").With("id", string(s.id))
	}
	s.responses = append(s.responses, Response{
		ItemID:     s.presented.ItemID,
		Difficulty: s.presented.Difficulty,
		Section:    s.presented.Section,
		Answer:     ans,
		Elapsed:    now.Sub(s.presented.DeliveredAt),
		Correct:    correct,
	})
	s.present(now)
	return nil
}

// Complete ends the session normally. It requires an in-progress session and a
// non-backwards clock, clears any presented item and stamps the end time.
func (s *Session) Complete(now time.Time) *shared.TestmakerError {
	return s.finish(StateCompleted, now)
}

// Abandon ends the session without completing (e.g. the global budget ran out).
// Same guards as Complete.
func (s *Session) Abandon(now time.Time) *shared.TestmakerError {
	return s.finish(StateAbandoned, now)
}

func (s *Session) finish(to State, now time.Time) *shared.TestmakerError {
	if s.state != StateInProgress {
		return ErrInvalidSession.WithMessagef("cannot end a %s session", s.state).With("id", string(s.id))
	}
	if now.Before(s.startedAt) {
		return ErrInvalidSession.WithMessage("clock ran backwards before the session started").With("id", string(s.id))
	}
	s.state = to
	s.endedAt = now
	s.presented = Presented{}
	return nil
}

// present selects the next undelivered item and puts it in front of the taker,
// stamped at now; if the plan is exhausted it clears the presented item.
func (s *Session) present(now time.Time) {
	next, ok := s.selectNext()
	if !ok {
		s.presented = Presented{}
		return
	}
	next.DeliveredAt = now
	s.presented = next
}

// selectNext finds the next item to deliver: the first section (in order) that
// still has undelivered items, and within it the policy's pick — authored order
// for fixed-increasing, closest-to-target band for adaptive. Item ids are unique
// across the plan, so "delivered" is tracked by id from the captured responses.
func (s *Session) selectNext() (Presented, bool) {
	delivered := make(map[string]struct{}, len(s.responses))
	for _, r := range s.responses {
		delivered[r.ItemID] = struct{}{}
	}
	for si := range s.sections {
		sec := s.sections[si]
		idx := s.pickInSection(si, sec, delivered)
		if idx < 0 {
			continue
		}
		it := sec.Items[idx]
		return Presented{ItemID: it.ItemID, Difficulty: it.Difficulty, Section: si}, true
	}
	return Presented{}, false
}

// pickInSection returns the index of the item to deliver next within section si,
// or -1 if the section has no undelivered items.
func (s *Session) pickInSection(si int, sec PlanSection, delivered map[string]struct{}) int {
	if s.policy == PolicyAdaptive {
		return s.pickAdaptive(si, sec, delivered)
	}
	// fixed-increasing: first undelivered item in authored order.
	for i, it := range sec.Items {
		if _, done := delivered[it.ItemID]; !done {
			return i
		}
	}
	return -1
}

// pickAdaptive returns the undelivered item in section si whose band is closest
// to the running target, breaking ties toward the lower band then the earlier
// authored position. The target starts at the section's easiest band and moves
// one band per graded answer: up on correct, down on wrong (a classical
// up/down staircase; IRT-based selection is deferred to scoring, Block 9).
func (s *Session) pickAdaptive(si int, sec PlanSection, delivered map[string]struct{}) int {
	minBand, maxBand := bandRange(sec.Items)
	target := clamp(minBand+s.sectionNet(si), minBand, maxBand)

	best := -1
	var bestDist, bestBand int
	for i, it := range sec.Items {
		if _, done := delivered[it.ItemID]; done {
			continue
		}
		dist := abs(it.Difficulty - target)
		if best < 0 || dist < bestDist || (dist == bestDist && it.Difficulty < bestBand) {
			best, bestDist, bestBand = i, dist, it.Difficulty
		}
	}
	return best
}

// sectionNet is correct-minus-wrong over the answers captured in section si.
func (s *Session) sectionNet(si int) int {
	net := 0
	for _, r := range s.responses {
		if r.Section != si {
			continue
		}
		if r.Correct {
			net++
		} else {
			net--
		}
	}
	return net
}

// Deadline is the earliest binding instant for the presented item: the sooner of
// its per-item cap (min of the session and section per-item limits) and the
// global total budget. A zero time means untimed or no item presented. The
// executor returns it to the renderer as advisory; only the global budget is a
// hard stop (see GlobalDeadline).
func (s *Session) Deadline() time.Time {
	if s.presented.none() {
		return time.Time{}
	}
	var out time.Time
	if per := effectivePerItem(s.timing.PerItem, s.sections[s.presented.Section].Timing.PerItem); per > 0 {
		out = s.presented.DeliveredAt.Add(per)
	}
	if gd := s.GlobalDeadline(); !gd.IsZero() && (out.IsZero() || gd.Before(out)) {
		out = gd
	}
	return out
}

// GlobalDeadline is startedAt + the global total budget, or a zero time when the
// session is untimed or not started. The executor treats a now past this instant
// as grounds to abandon the session.
func (s *Session) GlobalDeadline() time.Time {
	if s.timing.Total == 0 || s.startedAt.IsZero() {
		return time.Time{}
	}
	return s.startedAt.Add(s.timing.Total)
}

// Accessors (immutable identity; copies for slices).

func (s *Session) ID() SessionID { return s.id }

func (s *Session) TestID() string { return s.testID }

func (s *Session) Policy() Policy { return s.policy }

func (s *Session) State() State { return s.state }

func (s *Session) Timing() Timing { return s.timing }

func (s *Session) StartedAt() time.Time { return s.startedAt }

func (s *Session) EndedAt() time.Time { return s.endedAt }

func (s *Session) Presented() Presented { return s.presented }

func (s *Session) Sections() []PlanSection { return clonePlanSections(s.sections) }

func (s *Session) Responses() []Response { return slices.Clone(s.responses) }

// --- small helpers ---

func effectivePerItem(sessionPerItem, sectionPerItem time.Duration) time.Duration {
	switch {
	case sessionPerItem == 0:
		return sectionPerItem
	case sectionPerItem == 0:
		return sessionPerItem
	case sectionPerItem < sessionPerItem:
		return sectionPerItem
	default:
		return sessionPerItem
	}
}

func bandRange(items []PlanItem) (minBand, maxBand int) {
	minBand, maxBand = items[0].Difficulty, items[0].Difficulty
	for _, it := range items[1:] {
		if it.Difficulty < minBand {
			minBand = it.Difficulty
		}
		if it.Difficulty > maxBand {
			maxBand = it.Difficulty
		}
	}
	return minBand, maxBand
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// clonePlanSections deep-copies the plan so stored state never aliases caller
// (or snapshot) memory.
func clonePlanSections(sections []PlanSection) []PlanSection {
	if sections == nil {
		return nil
	}
	out := make([]PlanSection, len(sections))
	for i, sec := range sections {
		sec.Items = slices.Clone(sec.Items)
		out[i] = sec
	}
	return out
}
