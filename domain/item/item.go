package item

import (
	"math"
	"slices"

	"github.com/mariotoffia/testmaker/domain/shared"
)

// Item-context sentinels. Matched by Code via errors.Is (see shared.TestmakerError).
var (
	// ErrInvalidItem is returned when an ItemSpec/snapshot violates an invariant.
	ErrInvalidItem = &shared.TestmakerError{
		Code: "item.invalid", Class: shared.ClassInvalid, Message: "invalid item",
	}
	// ErrUnknownItem is returned when an item id is not in the bank.
	ErrUnknownItem = &shared.TestmakerError{
		Code: "item.unknown", Class: shared.ClassNotFound, Message: "unknown item",
	}
)

// ItemID uniquely identifies a bank item.
type ItemID string

// Origin records how an item entered the bank.
type Origin string

const (
	// OriginFetched: pulled from a source and normalized.
	OriginFetched Origin = "fetched"
	// OriginGenerated: produced by a rule engine / LLM.
	OriginGenerated Origin = "generated"
	// OriginAuthored: written by hand.
	OriginAuthored Origin = "authored"
)

// Valid reports whether the origin is known.
func (o Origin) Valid() bool {
	return o == OriginFetched || o == OriginGenerated || o == OriginAuthored
}

// AnswerFormat is how an item is answered (closed set).
type AnswerFormat string

const (
	// FormatMultipleChoice: pick one of 4–6 options.
	FormatMultipleChoice AnswerFormat = "multiple-choice"
	// FormatOpenNumeric: type a numeric value.
	FormatOpenNumeric AnswerFormat = "open-numeric"
	// FormatTrueFalseCannotSay: true / false / cannot-say verdict.
	FormatTrueFalseCannotSay AnswerFormat = "true-false-cannotsay"
)

// Valid reports whether the answer format is known.
func (f AnswerFormat) Valid() bool {
	return f == FormatMultipleChoice || f == FormatOpenNumeric || f == FormatTrueFalseCannotSay
}

// Verdict is the answer of a true-false-cannotsay item.
type Verdict string

const (
	VerdictTrue      Verdict = "true"
	VerdictFalse     Verdict = "false"
	VerdictCannotSay Verdict = "cannot-say"
)

// Valid reports whether the verdict is known.
func (v Verdict) Valid() bool {
	return v == VerdictTrue || v == VerdictFalse || v == VerdictCannotSay
}

// MediaKind classifies a figural media reference (closed set).
type MediaKind string

const (
	MediaImage  MediaKind = "image"
	MediaSVG    MediaKind = "svg"
	MediaGrid   MediaKind = "grid"
	MediaFigure MediaKind = "figure"
)

// Valid reports whether the media kind is known.
func (m MediaKind) Valid() bool {
	return m == MediaImage || m == MediaSVG || m == MediaGrid || m == MediaFigure
}

// Provenance is the origin of an item: which source it came from, how it was
// produced, and the redistributability it inherits from that source. The
// SourceID is a plain string carrying a source.SourceID — the item context
// cannot import the source context (they meet only through the shared kernel).
type Provenance struct {
	SourceID        string
	Origin          Origin
	Redistributable shared.Redistributable
}

// StimulusPart is one ordered piece of an item's prompt: either text or a
// figural media reference (blob key / URL). A part carries text, media, or both.
type StimulusPart struct {
	Text      string
	MediaKind MediaKind // set together with MediaRef for a figural part
	MediaRef  string    // blob key / URL, resolved by a blob store (Block 11)
}

func (p StimulusPart) hasMedia() bool { return p.MediaRef != "" }
func (p StimulusPart) empty() bool    { return p.Text == "" && p.MediaRef == "" }

// mediaKindWithoutRef reports a contradictory part: a media kind is set but there
// is no media reference for it to describe (kind and ref are set together).
func (p StimulusPart) mediaKindWithoutRef() bool { return p.MediaKind != "" && p.MediaRef == "" }

// Option is one multiple-choice answer option; it may be textual, figural, or
// both. ID is referenced by the AnswerKey of a multiple-choice item.
type Option struct {
	ID        string
	Text      string
	MediaKind MediaKind
	MediaRef  string
}

func (o Option) hasMedia() bool { return o.MediaRef != "" }
func (o Option) empty() bool    { return o.Text == "" && o.MediaRef == "" }

// mediaKindWithoutRef reports a contradictory option: a media kind with no media
// reference for it to describe.
func (o Option) mediaKindWithoutRef() bool { return o.MediaKind != "" && o.MediaRef == "" }

// AnswerKey is the correct answer, interpreted by AnswerFormat: OptionID for
// multiple-choice, Numeric for open-numeric, Verdict for true-false-cannotsay.
// Only the field matching the format may be set; NewItem rejects a key that
// carries a value for a non-matching field, so a snapshot is always canonical.
//
// Numeric has no presence bit: 0 is a valid open-numeric answer (e.g. "5 − 5"),
// so a zero key is accepted, and for the other formats Numeric is canonically 0.
// Distinguishing an omitted key from a deliberate 0 is a modelling decision
// (a pointer / presence flag / per-format key types) deferred until a producer
// — the generator (Block 6) or authoring — actually needs it.
type AnswerKey struct {
	OptionID string
	Numeric  float64
	Verdict  Verdict
}

// Difficulty is an item's ordinal difficulty band (1..N).
//
// ponytail: band only for now. IRT a/b/c parameters and item norms (p-value /
// response-time baseline) are deferred — adaptive delivery (Block 8) resolves
// IRT-vs-classical and calibration/norms belong to scoring (Block 9). Adding
// unused psychometric fields now would be speculative structure.
type Difficulty struct {
	Band int
}

// minMCOptions and maxMCOptions bound a multiple-choice item's option count.
const (
	minMCOptions = 4
	maxMCOptions = 6
)

// ItemSpec is the validated input to NewItem.
type ItemSpec struct {
	ID           ItemID
	Provenance   Provenance
	TestType     shared.TestTypeCode
	Stimulus     []StimulusPart
	AnswerFormat AnswerFormat
	Options      []Option
	AnswerKey    AnswerKey
	Explanation  string
	Difficulty   Difficulty
}

// Item is the aggregate root of the item bank: one scored test item. All state
// is private and validated on construction; it crosses ports only as a Snapshot.
type Item struct {
	id           ItemID
	provenance   Provenance
	testType     shared.TestTypeCode
	family       shared.AbilityFamily // derived from testType, never accepted
	stimulus     []StimulusPart
	answerFormat AnswerFormat
	options      []Option
	answerKey    AnswerKey
	explanation  string
	difficulty   Difficulty
}

// NewItem validates a spec and returns the aggregate. The AbilityFamily is
// derived from the TestType and is not accepted from callers.
func NewItem(spec ItemSpec) (*Item, *shared.TestmakerError) {
	if err := validateCommon(spec); err != nil {
		return nil, err
	}
	if err := validateAnswer(spec); err != nil {
		return nil, err
	}

	family, _ := spec.TestType.Family() // ok: TestType.Valid() checked above
	return &Item{
		id:           spec.ID,
		provenance:   spec.Provenance,
		testType:     spec.TestType,
		family:       family,
		stimulus:     slices.Clone(spec.Stimulus),
		answerFormat: spec.AnswerFormat,
		options:      slices.Clone(spec.Options),
		answerKey:    spec.AnswerKey,
		explanation:  spec.Explanation,
		difficulty:   spec.Difficulty,
	}, nil
}

// MustItem panics on invalid input; for tests and static fixtures only.
func MustItem(spec ItemSpec) *Item {
	it, err := NewItem(spec)
	if err != nil {
		panic(err)
	}
	return it
}

// validateCommon checks the invariants shared by every answer format.
func validateCommon(spec ItemSpec) *shared.TestmakerError {
	fail := func(msg string) *shared.TestmakerError {
		return ErrInvalidItem.WithMessage(msg).With("id", string(spec.ID))
	}
	switch {
	case spec.ID == "":
		return ErrInvalidItem.WithMessage("item id is required")
	case spec.Provenance.SourceID == "":
		return fail("provenance source id is required")
	case !spec.Provenance.Origin.Valid():
		return fail("invalid provenance origin: " + string(spec.Provenance.Origin))
	case !spec.Provenance.Redistributable.Valid():
		return fail("invalid provenance redistributable: " + string(spec.Provenance.Redistributable))
	case !spec.TestType.Valid():
		return fail("invalid test type: " + string(spec.TestType))
	case !spec.AnswerFormat.Valid():
		return fail("invalid answer format: " + string(spec.AnswerFormat))
	case spec.Difficulty.Band < 1:
		return fail("difficulty band must be >= 1")
	}
	if len(spec.Stimulus) == 0 {
		return fail("at least one stimulus part is required")
	}
	for _, p := range spec.Stimulus {
		if p.empty() {
			return fail("stimulus part has neither text nor media")
		}
		if p.mediaKindWithoutRef() {
			return fail("stimulus part has a media kind but no media reference")
		}
		if p.hasMedia() && !p.MediaKind.Valid() {
			return fail("stimulus part media kind is invalid: " + string(p.MediaKind))
		}
	}
	return nil
}

// validateAnswer checks the format-specific invariants (option count, key shape).
func validateAnswer(spec ItemSpec) *shared.TestmakerError {
	fail := func(msg string) *shared.TestmakerError {
		return ErrInvalidItem.WithMessage(msg).With("id", string(spec.ID))
	}
	switch spec.AnswerFormat {
	case FormatMultipleChoice:
		return validateMultipleChoice(spec, fail)
	case FormatOpenNumeric:
		if len(spec.Options) != 0 {
			return fail("open-numeric item must have no options")
		}
		if spec.AnswerKey.OptionID != "" || spec.AnswerKey.Verdict != "" {
			return fail("open-numeric key must be numeric only")
		}
		// a non-finite key is meaningless and, crucially, is not JSON-encodable —
		// it would save in memory but fail in the sqlite store, breaking parity.
		if math.IsNaN(spec.AnswerKey.Numeric) || math.IsInf(spec.AnswerKey.Numeric, 0) {
			return fail("open-numeric key must be a finite number")
		}
		return nil
	case FormatTrueFalseCannotSay:
		if len(spec.Options) != 0 {
			return fail("true-false-cannotsay item must have no options")
		}
		if spec.AnswerKey.OptionID != "" || spec.AnswerKey.Numeric != 0 {
			return fail("true-false-cannotsay key must be a verdict only")
		}
		if !spec.AnswerKey.Verdict.Valid() {
			return fail("true-false-cannotsay key verdict is invalid: " + string(spec.AnswerKey.Verdict))
		}
		return nil
	default:
		return fail("invalid answer format: " + string(spec.AnswerFormat))
	}
}

// validateMultipleChoice enforces 4–6 unique, non-empty options and a key that
// references one of them.
func validateMultipleChoice(spec ItemSpec, fail func(string) *shared.TestmakerError) *shared.TestmakerError {
	if n := len(spec.Options); n < minMCOptions || n > maxMCOptions {
		return fail("multiple-choice item must have 4–6 options")
	}
	if spec.AnswerKey.Verdict != "" || spec.AnswerKey.Numeric != 0 {
		return fail("multiple-choice key must be an option id only")
	}
	seen := make(map[string]struct{}, len(spec.Options))
	for _, o := range spec.Options {
		if o.ID == "" {
			return fail("multiple-choice option id is required")
		}
		if _, dup := seen[o.ID]; dup {
			return fail("duplicate multiple-choice option id: " + o.ID)
		}
		seen[o.ID] = struct{}{}
		if o.empty() {
			return fail("multiple-choice option has neither text nor media")
		}
		if o.mediaKindWithoutRef() {
			return fail("multiple-choice option has a media kind but no media reference")
		}
		if o.hasMedia() && !o.MediaKind.Valid() {
			return fail("multiple-choice option media kind is invalid: " + string(o.MediaKind))
		}
	}
	if _, ok := seen[spec.AnswerKey.OptionID]; !ok {
		return fail("multiple-choice key does not reference an existing option: " + spec.AnswerKey.OptionID)
	}
	return nil
}

// Accessors (immutable identity; copies for slices).

func (i *Item) ID() ItemID { return i.id }

func (i *Item) Provenance() Provenance { return i.provenance }

func (i *Item) TestType() shared.TestTypeCode { return i.testType }

func (i *Item) Family() shared.AbilityFamily { return i.family }

func (i *Item) AnswerFormat() AnswerFormat { return i.answerFormat }

func (i *Item) AnswerKey() AnswerKey { return i.answerKey }

func (i *Item) Difficulty() Difficulty { return i.difficulty }

func (i *Item) Explanation() string { return i.explanation }

func (i *Item) Stimulus() []StimulusPart { return slices.Clone(i.stimulus) }

func (i *Item) Options() []Option { return slices.Clone(i.options) }

// Redistributable reports the reuse gate this item inherited from its source.
func (i *Item) Redistributable() shared.Redistributable { return i.provenance.Redistributable }

// ItemSnapshot is the dependency-neutral DTO used to persist/transport an Item.
// It carries the derived Family alongside the TestType so a store can index or
// filter by family without re-deriving it.
type ItemSnapshot struct {
	ID           ItemID
	Provenance   Provenance
	TestType     shared.TestTypeCode
	Family       shared.AbilityFamily
	Stimulus     []StimulusPart
	AnswerFormat AnswerFormat
	Options      []Option
	AnswerKey    AnswerKey
	Explanation  string
	Difficulty   Difficulty
}

// Snapshot returns the persistence/transport DTO for the aggregate.
func (i *Item) Snapshot() ItemSnapshot {
	return ItemSnapshot{
		ID:           i.id,
		Provenance:   i.provenance,
		TestType:     i.testType,
		Family:       i.family,
		Stimulus:     slices.Clone(i.stimulus),
		AnswerFormat: i.answerFormat,
		Options:      slices.Clone(i.options),
		AnswerKey:    i.answerKey,
		Explanation:  i.explanation,
		Difficulty:   i.difficulty,
	}
}

// RehydrateFromSnapshot rebuilds an aggregate from a trusted snapshot without
// re-validating (the snapshot is assumed to have passed NewItem previously). It
// deep-copies slice fields via slices.Clone (preserving the nil-vs-empty
// distinction), so a snapshot round-tripped through Rehydrate().Snapshot()
// carries no aliased slices — the property both stores rely on to hand back
// snapshots that never share memory with stored state or each other.
func RehydrateFromSnapshot(s ItemSnapshot) *Item {
	return &Item{
		id:           s.ID,
		provenance:   s.Provenance,
		testType:     s.TestType,
		family:       s.Family,
		stimulus:     slices.Clone(s.Stimulus),
		answerFormat: s.AnswerFormat,
		options:      slices.Clone(s.Options),
		answerKey:    s.AnswerKey,
		explanation:  s.Explanation,
		difficulty:   s.Difficulty,
	}
}
