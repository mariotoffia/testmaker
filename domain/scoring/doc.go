// Package scoring is the "scoring" bounded context — turning a completed
// session into a raw score, a percentile / normal-distribution band and an
// IQ-style scaled score, plus per-item explanations.
//
// SCAFFOLD: only the DTO shell referenced by ports is present so the workspace
// compiles. ScoringPolicy, norm tables and the scaled-score model land in the
// "Scoring & Feedback" block.
package scoring

// Score is a placeholder result DTO. Fields will expand (band, per-item
// breakdown, explanations) in the Scoring block.
type Score struct {
	Raw        int
	Percentile float64
	ScaledIQ   int
}
