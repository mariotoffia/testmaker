package scoring

import (
	"math"
	"time"
)

// Score is the value result of scoring one completed attempt: how many items
// were correct, the ability dimension that was normed, the norm-derived
// percentile / band / IQ, a speed reading, and per-item feedback. It carries no
// identity — a Scorer produces it from a session snapshot.
type Score struct {
	// Raw is the number of correct responses.
	Raw int
	// Max is the number of scored responses (the raw-score denominator).
	Max int
	// Ability is the adaptive ability estimate in difficulty-band units (the
	// staircase reversal mean); 0 for a fixed-increasing attempt, whose normed
	// dimension is Raw. It is reported for transparency into what was normed.
	Ability float64
	// Normed reports whether a norm table applied. When false the test carries no
	// norms and Percentile/ScaledIQ are 0 with Band == BandUnnormed.
	Normed bool
	// Percentile is the percentile rank (0..100) of the normed dimension under
	// the test's normal norm; 0 when unnormed.
	Percentile float64
	// ScaledIQ is the normed dimension on the mean-100, SD-15 IQ scale; 0 when
	// unnormed.
	ScaledIQ int
	// Band classifies ScaledIQ (BandUnnormed when unnormed).
	Band Band
	// Speed is the response-time dimension over the whole attempt.
	Speed Speed
	// Items is per-item feedback (the correct answer and its explanation).
	Items []ItemFeedback
	// DegradedFeedback counts items whose feedback text could not be resolved
	// because the item is no longer in the bank. It distinguishes one deleted
	// item (a small count) from a misconfigured/empty bank (== len(Items)); the
	// grades are still frozen and correct either way.
	DegradedFeedback int
}

// Speed is the response-time scoring dimension. Speed contributes to the scaled
// score only where a test defines it (number-speed, perceptual-speed families);
// here it is always reported as data, and folding it into ScaledIQ is a
// per-family norm decision deferred until a speed-normed test needs it.
//
// ponytail: report the rate, do not fold it into IQ. A speed-weighted composite
// needs a speed norm model (mean/SD of the rate) that no test carries yet.
type Speed struct {
	// Total is the summed time spent across all responses.
	Total time.Duration
	// Mean is the average time per response (0 when there are no responses).
	Mean time.Duration
	// CorrectPerMinute is correct answers per minute of total time (0 when no
	// time was spent).
	CorrectPerMinute float64
}

// ItemFeedback is the post-completion explanation for one administered item: the
// taker's answer, the correct answer, and why. Given/CorrectAnswer are rendered
// strings so the feedback is display-ready without importing the item context.
type ItemFeedback struct {
	ItemID        string
	SourceID      string
	Correct       bool
	Given         string
	CorrectAnswer string
	Explanation   string
	Elapsed       time.Duration
}

// Outcome is one graded response reduced to what scoring needs: whether it was
// correct and the difficulty band of the item administered. The application
// layer maps a session.Response to this — the scoring context cannot import the
// session context (bounded contexts meet only through the shared kernel).
type Outcome struct {
	Correct bool
	Band    int
}

// NormTable is a parametric normal model of a test's scored dimension: the
// population Mean and SD of that dimension (raw score for fixed tests, ability
// band for adaptive). It maps a raw/ability value onto a percentile and the
// IQ scale.
//
// ponytail: a two-parameter normal model, not an empirical percentile lookup.
// It is the standard IQ normalization (percentile = Φ(z), IQ = 100 + 15z) and
// needs no per-point table; an empirical/piecewise table lands only if a test's
// norms are published as one.
type NormTable struct {
	Mean float64
	SD   float64
}

// Valid reports whether the norm is usable (a positive spread).
func (n NormTable) Valid() bool { return n.SD > 0 }

// z is the standard score of x under the norm.
func (n NormTable) z(x float64) float64 { return (x - n.Mean) / n.SD }

// Percentile returns the percentile rank of x under the normal model, clamped to
// the meaningful open interval: a continuous norm never ranks a taker above or
// below literally everyone, so it stays inside (minPercentile, maxPercentile).
func (n NormTable) Percentile(x float64) float64 {
	return min(max(100*phi(n.z(x)), minPercentile), maxPercentile)
}

// ScaledIQ maps x onto the mean-100, SD-15 IQ scale, rounded to the nearest
// integer and clamped to [minScaledIQ, maxScaledIQ]. Beyond ~±4 SD the
// parametric normal tail is not psychometrically valid, so an extreme raw/ability
// value reports the floor/ceiling instead of an absurd IQ (e.g. −50 or 370).
func (n NormTable) ScaledIQ(x float64) int {
	return min(max(int(math.Round(100+15*n.z(x))), minScaledIQ), maxScaledIQ)
}

// Reporting bounds for a normal norm. The IQ range is the conventional Wechsler
// floor/ceiling; the percentile range keeps the rank strictly inside 0..100.
// The two are independent presentation caps, not a single-model correspondence —
// a saturated report may read "IQ 160, 99.9 percentile" though those z-values
// differ; both simply mean "off the top of what this norm resolves".
//
// ponytail: clamp to the range the model is valid over, do not extrapolate the
// tail. Empirical extreme-score norms are the upgrade path if a test publishes
// them.
const (
	minScaledIQ   = 40
	maxScaledIQ   = 160
	minPercentile = 0.1
	maxPercentile = 99.9
)

// NormBook resolves a test's norm table by test id; a test with no entry (or an
// invalid one) is scored raw-only (Score.Normed == false).
type NormBook map[string]NormTable

// Lookup returns the valid norm table for testID, if any.
func (b NormBook) Lookup(testID string) (NormTable, bool) {
	n, ok := b[testID]
	return n, ok && n.Valid()
}

// Band is a qualitative classification of a scaled IQ (Wechsler-style labels).
type Band string

const (
	// BandUnnormed marks a score produced without a norm table.
	BandUnnormed Band = "unnormed"
	// BandExtremelyLow is IQ < 70.
	BandExtremelyLow Band = "extremely-low"
	// BandBorderline is IQ 70–79.
	BandBorderline Band = "borderline"
	// BandLowAverage is IQ 80–89.
	BandLowAverage Band = "low-average"
	// BandAverage is IQ 90–109.
	BandAverage Band = "average"
	// BandHighAverage is IQ 110–119.
	BandHighAverage Band = "high-average"
	// BandSuperior is IQ 120–129.
	BandSuperior Band = "superior"
	// BandVerySuperior is IQ >= 130.
	BandVerySuperior Band = "very-superior"
)

// Classify maps a scaled IQ onto its band.
func Classify(iq int) Band {
	switch {
	case iq >= 130:
		return BandVerySuperior
	case iq >= 120:
		return BandSuperior
	case iq >= 110:
		return BandHighAverage
	case iq >= 90:
		return BandAverage
	case iq >= 80:
		return BandLowAverage
	case iq >= 70:
		return BandBorderline
	default:
		return BandExtremelyLow
	}
}

// AbilityFromStaircase estimates ability (in difficulty-band units) from an
// adaptive up/down attempt as the mean band at the reversal points — the
// classical transformed up/down estimator. A reversal is a change of direction
// (a correct answer after a wrong one, or the reverse), so this consumes the
// ORDER of the delivery path, not just the count correct: two attempts with the
// same items and the same number correct but a different sequence get different
// abilities. Without a reversal the taker was monotone — all correct settles at
// the hardest band reached, all wrong at the easiest.
//
// ponytail: reversal-mean, the standard staircase estimator, is the honest
// ceiling while Difficulty.Band is the only calibration signal. IRT/MLE theta
// (a/b/c item parameters) is the upgrade path once the bank is calibrated.
//
// Two known biases of the plain reversal-mean, both accepted at this ceiling:
// it counts every reversal including the initial pre-convergence transient
// (classical up/down discards the first one or two), and it counts a
// correctness flip while the staircase is pinned at the easiest/hardest band
// (the item pool is exhausted) as a real reversal. Both bias short or saturated
// staircases; discarding the transient lands with IRT calibration, when the
// estimator is replaced rather than tuned.
func AbilityFromStaircase(outcomes []Outcome) float64 {
	if len(outcomes) == 0 {
		return 0
	}
	minBand, maxBand := outcomes[0].Band, outcomes[0].Band
	var reversalSum float64
	reversals := 0
	for i, o := range outcomes {
		if o.Band < minBand {
			minBand = o.Band
		}
		if o.Band > maxBand {
			maxBand = o.Band
		}
		if i > 0 && o.Correct != outcomes[i-1].Correct {
			reversalSum += float64(o.Band)
			reversals++
		}
	}
	if reversals > 0 {
		return reversalSum / float64(reversals)
	}
	// Monotone path: no direction change. All correct ⇒ never failed ⇒ ability at
	// the hardest band solved; all wrong ⇒ ability at the easiest attempted.
	if outcomes[0].Correct {
		return float64(maxBand)
	}
	return float64(minBand)
}

// phi is the standard-normal CDF, via the complementary error function.
func phi(z float64) float64 { return 0.5 * math.Erfc(-z/math.Sqrt2) }
