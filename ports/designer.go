package ports

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/scoring"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/testset"
)

// GenerateSpec parameterizes procedural item generation (rules, family,
// difficulty band, count, seed).
//
// SCAFFOLD: firms up in the Designer / Generator block.
type GenerateSpec struct {
	TestType   string // taxonomy code, e.g. "A2"
	Difficulty int
	Count      int
	Seed       int64
}

// Generator procedurally generates items (driven port). Backed by the rule
// engines catalogued as generator sources (Sandia, matRiks, RAVEN family, ...).
//
// SCAFFOLD.
type Generator interface {
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
