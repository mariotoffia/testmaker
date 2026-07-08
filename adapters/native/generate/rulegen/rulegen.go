package rulegen

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// Adapter error sentinels. Callers match by Code with errors.Is; a validation
// failure keeps the underlying item error reachable through Unwrap.
var (
	// ErrUnsupportedType marks a taxonomy code this engine cannot generate.
	ErrUnsupportedType = &shared.TestmakerError{
		Code: "rulegen.unsupported_type", Class: shared.ClassUnsupported, Message: "unsupported test type",
	}
	// ErrInvalidSpec marks a GenerateSpec that violates a precondition.
	ErrInvalidSpec = &shared.TestmakerError{
		Code: "rulegen.invalid_spec", Class: shared.ClassInvalid, Message: "invalid generate spec",
	}
	// ErrGenerate marks a generated item that failed item.NewItem — an engine
	// bug, surfaced loudly rather than skipped.
	ErrGenerate = &shared.TestmakerError{
		Code: "rulegen.generate", Class: shared.ClassInvalid, Message: "generated item failed validation",
	}
)

// generatorSourceID is the synthetic source id stamped on generated items'
// provenance (Origin=generated); the rule engine is itself the "source".
const generatorSourceID = "rulegen"

// maxCount bounds a single batch. It guards the slice preallocation against an
// absurd Count (e.g. a hostile math.MaxInt that would panic makeslice) and
// keeps the "one batch, drawn from one seeded stream" model in memory-safe
// territory. ponytail: a fixed cap, not streaming — no caller needs > 1000 items
// per request, and larger banks are built from several seeded batches.
const maxCount = 1000

// Generator is the native, IP-free figural rule engine implementing
// ports.Generator. It holds no state: every request is a pure function of the
// spec, so it is safe for concurrent use.
type Generator struct{}

// New returns the rule-engine generator.
func New() *Generator { return &Generator{} }

// engine builds one figural puzzle for a difficulty band from a seeded RNG.
type engine func(rng *rand.Rand, band int) puzzle

// newRNG builds the batch PRNG. It uses math/rand/v2's PCG source seeded with the
// full 64-bit spec seed, so every distinct int64 seed yields a distinct stream.
// The legacy math/rand source must NOT be used here: it folds the seed modulo
// 2^31-1, collapsing seeds like 1 and 1+(2^31-1) onto identical streams and
// breaking the port's "different seeds diverge" contract.
func newRNG(seed int64) *rand.Rand {
	return rand.New(rand.NewPCG(uint64(seed), 0x9E3779B97F4A7C15))
}

// Generate produces spec.Count items of spec.TestType at spec.Difficulty,
// derived deterministically from spec.Seed. Each item is validated through
// item.NewItem, so every returned snapshot is guaranteed valid and correctly
// keyed; an unsupported type or a construction failure is an error, never a
// silently dropped item.
//
// The whole batch is drawn from a single stream seeded from spec.Seed: the same
// spec yields byte-identical items (the port's determinism contract), and two
// different seeds diverge immediately rather than sharing overlapping streams.
// ponytail: items are not guaranteed semantically unique — the figural rule
// space is finite, so a large batch (or reused seeds) can repeat a figure.
// Cross-batch semantic de-duplication is deferred until a caller needs it.
func (g *Generator) Generate(ctx context.Context, spec ports.GenerateSpec) ([]item.ItemSnapshot, error) {
	if spec.Count < 1 {
		return nil, ErrInvalidSpec.WithMessage("count must be >= 1")
	}
	if spec.Count > maxCount {
		return nil, ErrInvalidSpec.WithMessagef("count must be <= %d", maxCount).With("count", spec.Count)
	}
	if spec.Difficulty < 1 {
		return nil, ErrInvalidSpec.WithMessage("difficulty must be >= 1")
	}
	build, ok := engineFor(spec.TestType)
	if !ok {
		return nil, ErrUnsupportedType.
			WithMessagef("rulegen cannot generate test type %q", spec.TestType).
			With("test_type", string(spec.TestType))
	}

	rng := newRNG(spec.Seed)
	out := make([]item.ItemSnapshot, 0, spec.Count)
	for i := 0; i < spec.Count; i++ {
		if err := ctx.Err(); err != nil {
			return nil, ErrGenerate.WithMessage("generation cancelled").Wrap(err)
		}
		p := build(rng, spec.Difficulty)

		it, verr := item.NewItem(item.ItemSpec{
			// ponytail: the id keys on (type, effective band, seed, index) — the
			// full determinant of an item's content today, so re-generation is
			// idempotent. It carries no rulegen ruleset version: if the engine's
			// rules later change, the same id could denote different content.
			// Versioning the id is deferred until a persisted generated bank must
			// survive a rule change (no caller needs that yet).
			ID:           item.ItemID(fmt.Sprintf("gen-%s-b%d-s%d-%d", spec.TestType, p.band, spec.Seed, i)),
			Provenance:   item.Provenance{SourceID: generatorSourceID, Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
			TestType:     spec.TestType,
			Stimulus:     p.stimulus,
			AnswerFormat: item.FormatMultipleChoice,
			Options:      p.options,
			AnswerKey:    p.key,
			Explanation:  p.explanation,
			Difficulty:   item.Difficulty{Band: p.band},
		})
		if verr != nil {
			return nil, ErrGenerate.
				WithMessagef("test type %q item %d failed validation", spec.TestType, i).
				Wrap(verr)
		}
		out = append(out, it.Snapshot())
	}
	return out, nil
}

// engineFor selects the rule engine for a taxonomy code. The primary figural
// families are covered: A1/A3 figure series, A2 matrices, A4 odd-one-out. A3
// (Mensa-style homogeneous figure reasoning) shares the series engine.
func engineFor(code shared.TestTypeCode) (engine, bool) {
	switch code {
	case "A1", "A3":
		return genSeries, true
	case "A2":
		return genMatrix, true
	case "A4":
		return genOddOneOut, true
	default:
		return nil, false
	}
}
