package ports

import (
	"context"
	"time"

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

// Delivery is what the executor hands back after each administration step: the
// session snapshot (persisted state, for the caller to resume or score), the
// item now in front of the taker, and when their time on it runs out.
//
// Item is nil when no item is presented — the plan is exhausted (call Complete)
// or the session ended. Deadline is the earliest binding instant for the current
// item (per-item cap or the global budget, whichever is sooner); a zero Deadline
// means untimed. It is advisory to a renderer; the executor itself enforces only
// the global budget, abandoning a session whose total time has run out.
type Delivery struct {
	Session  session.SessionSnapshot
	Item     *item.ItemSnapshot
	Deadline time.Time
}

// Executor administers a test: start it, capture each answer (grading it and,
// under timing and the delivery policy, presenting the next item) and complete
// it (driving port).
//
// Backed by app/execution, which injects a clock (domain/clock), the item bank
// (to grade answers and fetch item content) and the session repository.
type Executor interface {
	// Start creates a session for the test, presents the first item and returns
	// the opening Delivery.
	Start(ctx context.Context, test testset.TestSnapshot) (Delivery, error)
	// Answer grades the taker's answer to itemID in session id and advances to the
	// next Delivery. It fails if the session is unknown or the answer does not
	// target the presented item.
	Answer(ctx context.Context, id session.SessionID, itemID string, ans session.Answer) (Delivery, error)
	// Complete ends session id and returns its final snapshot. It normally
	// records completion, but abandons the session instead when the global time
	// budget has already been exhausted (mirroring the Answer deadline check).
	Complete(ctx context.Context, id session.SessionID) (session.SessionSnapshot, error)
}

// Scorer turns a completed session snapshot into a raw score, a norm-derived
// percentile band and IQ-style scaled score, a speed reading and per-item
// feedback (driving port).
//
// Backed by app/scoring, which injects the item bank (read, to render the
// correct answer and explanation for each item) and the norm book (test id →
// norm table). Like Executor it orchestrates a driven port, so it is a
// use-case, not a pure driven adapter. It scores from the FROZEN grades in the
// snapshot (session.Response.Correct), never re-grading against the live bank,
// so a score is reproducible and immune to later bank drift. It rejects a
// session that is not completed with scoring.ErrNotScorable.
type Scorer interface {
	Score(ctx context.Context, snap session.SessionSnapshot) (scoring.Score, error)
}
