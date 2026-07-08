package ports

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/scoring"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
)

// GenerateSpec parameterizes one procedural generation request: which item type
// to emit, the target difficulty band, how many items, and the seed that makes
// the batch reproducible.
//
// Determinism is a contract, not a nicety (DESIGN §7): the same spec — same
// TestType, Difficulty, Count and Seed — must yield byte-identical items, so a
// generated bank can be rebuilt and tests can assert on exact output.
type GenerateSpec struct {
	// TestType is the taxonomy code to generate (e.g. "A2"); an unsupported code
	// is rejected by the generator.
	TestType shared.TestTypeCode
	// Difficulty is the target difficulty band (>= 1); it drives rule complexity.
	// Rule complexity can saturate below a high requested band, in which case the
	// produced item carries the *effective* band the generator realized (never
	// above the requested band), so a difficulty tag is always honest.
	Difficulty int
	// Count is how many items to produce (>= 1).
	Count int
	// Seed makes the batch reproducible; the whole batch is drawn from a single
	// full-width stream seeded from it, so the same spec yields byte-identical
	// items. Distinct seeds draw from independent streams, but content is NOT
	// guaranteed unique across seeds: the figural rule space is finite, so two
	// different seeds can land on the same item (only its id, which embeds the
	// seed, then differs). Deduplication across seeds is a generator concern, not
	// a Seed guarantee.
	Seed int64
}

// Generator procedurally generates items with ground-truth keys (driven port).
// It builds each item through item.NewItem, so a returned snapshot is always
// valid — a construction failure is a generator bug and surfaces as an error,
// never a silently skipped item.
//
// Backed by a native rule engine (adapters/native/generate/rulegen); the
// external engines catalogued as generator sources (Sandia SGMT, matRiks,
// RAVEN family) are format references, not dependencies.
type Generator interface {
	// Generate returns Count items for spec.TestType at spec.Difficulty, derived
	// from spec.Seed. An unsupported TestType or an invalid spec is an error.
	Generate(ctx context.Context, spec GenerateSpec) ([]item.ItemSnapshot, error)
}

// Executor administers a test: start, deliver the next item (honoring timing
// and adaptive difficulty), capture responses and complete (driving port).
//
// SCAFFOLD: firms up in the Renderer / Executor block.
type Executor interface {
	Start(ctx context.Context, test testset.TestSnapshot) (session.SessionSnapshot, error)
	Complete(ctx context.Context, id session.SessionID) (session.SessionSnapshot, error)
}

// Scorer turns a completed session into a raw score, percentile band and
// IQ-style scaled score (driven port).
//
// SCAFFOLD: firms up in the Scoring & Feedback block.
type Scorer interface {
	Score(ctx context.Context, snap session.SessionSnapshot) (scoring.Score, error)
}
