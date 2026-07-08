// Package scoring is the "scoring" bounded context — turning a completed
// session into a raw score, a percentile / normal-distribution band and an
// IQ-style scaled score (mean 100, SD 15 by convention), plus per-item
// explanations and a speed reading.
//
// It is pure and stdlib-only: the model (Score, NormTable, Band, Outcome) and
// the psychometric math (normal normalization, the staircase ability estimator)
// live here; the app/scoring use-case maps a session snapshot onto these types
// and resolves norms, because this context cannot import the session or item
// contexts (bounded contexts meet only through the shared kernel).
package scoring

import "github.com/mariotoffia/testmaker/domain/shared"

// ErrNotScorable is returned when a session is not in a scorable state — scoring
// requires a completed attempt.
var ErrNotScorable = &shared.TestmakerError{
	Code: "scoring.not_scorable", Class: shared.ClassInvalid, Message: "session is not scorable",
}
