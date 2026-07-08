// Package scoring is the application service (use-case layer) for the scoring &
// feedback concern: it turns a completed session snapshot into a scoring.Score.
//
// It implements the ports.Scorer driving port. Like app/execution (the executor
// half of the renderer), scoring is a use-case that orchestrates driven ports —
// it reads the item bank (ports.ItemRepository) to render per-item feedback and
// resolves the test's norm table — so it cannot be a pure driven adapter. The
// psychometric math (normalization, the staircase ability estimator, band
// classification) lives in domain/scoring; this service only maps a
// session.SessionSnapshot onto that model and assembles the result.
//
// The raw score is read from the FROZEN grades captured at administration
// (session.Response.Correct), never re-graded against the live bank, so a score
// is reproducible and immune to later bank drift/deletion. The bank is consulted
// only to render feedback text, which degrades to blank for an item removed
// since the attempt.
package scoring

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/mariotoffia/testmaker/domain/item"
	model "github.com/mariotoffia/testmaker/domain/scoring"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/ports"
)

// Service scores completed attempts. It holds the item bank (read, for feedback)
// and the norm book (test id → norm table). It is stateless beyond those
// dependencies, so one Service safely scores many attempts concurrently.
type Service struct {
	bank  ports.ItemRepository
	norms model.NormBook
}

// NewService wires the item bank and the norm book. A nil/empty norm book scores
// every test raw-only (Score.Normed == false); the composition root supplies the
// norms a deployment carries.
func NewService(bank ports.ItemRepository, norms model.NormBook) *Service {
	return &Service{bank: bank, norms: norms}
}

// Score turns a completed session into a raw score, a norm-derived percentile /
// band / scaled IQ (when the test is normed), a speed reading and per-item
// feedback. It rejects a session that is not completed — or a completed one that
// answered nothing — with scoring.ErrNotScorable, and nothing is scored.
//
// The normed dimension is the raw correct count for a fixed-increasing attempt
// and the staircase ability estimate for an adaptive one — so an adaptive score
// reflects the delivery path taken, not just the count correct.
func (s *Service) Score(ctx context.Context, snap session.SessionSnapshot) (model.Score, error) {
	if snap.State != session.StateCompleted {
		return model.Score{}, model.ErrNotScorable.
			WithMessagef("session %s is %s, not completed", snap.ID, snap.State).
			With("id", string(snap.ID))
	}
	// An attempt that answered nothing carries no data to norm: scoring it would
	// stamp a confident IQ/band (Raw 0 ⇒ a low z) on an empty completion. There is
	// no measurable ability, so it is not scorable.
	if len(snap.Responses) == 0 {
		return model.Score{}, model.ErrNotScorable.
			WithMessagef("session %s completed with no responses", snap.ID).
			With("id", string(snap.ID))
	}

	score := model.Score{
		Raw: countCorrect(snap.Responses),
		// ponytail: Max is the ANSWERED count. The executor's normal flow answers
		// every planned item, so for a full administration Max == the planned
		// length the norm is calibrated on. A norm-derived score therefore assumes
		// a full administration; a taker who completes early is normed against a
		// full-length norm (an under-count). Re-derive Max from snap.Sections
		// (the planned list) if partial-attempt norming ever needs its own norm.
		Max:   len(snap.Responses),
		Speed: speedOf(snap.Responses),
	}

	normed := float64(score.Raw)
	if snap.Policy == session.PolicyAdaptive {
		score.Ability = abilityOf(snap.Responses)
		normed = score.Ability
	}

	if norm, ok := s.norms.Lookup(snap.TestID); ok {
		score.Normed = true
		score.Percentile = norm.Percentile(normed)
		score.ScaledIQ = norm.ScaledIQ(normed)
		score.Band = model.Classify(score.ScaledIQ)
	} else {
		score.Band = model.BandUnnormed
	}

	feedback, degraded, err := s.feedback(ctx, snap.Responses)
	if err != nil {
		return model.Score{}, err
	}
	score.Items = feedback
	score.DegradedFeedback = degraded
	return score, nil
}

// countCorrect tallies the frozen graded outcomes.
func countCorrect(responses []session.Response) int {
	n := 0
	for _, r := range responses {
		if r.Correct {
			n++
		}
	}
	return n
}

// abilityOf estimates ability from an adaptive attempt: the reversal-mean per
// section (each section runs its own staircase), combined across sections as a
// response-count-weighted mean so a longer section counts proportionally.
// Responses carry their section index, so no plan is needed.
//
// ponytail: response-weighted mean assumes the sections' bands are on a
// comparable scale (they are, today: one ordinal band axis across families). A
// per-family ability vector — not a single scalar — is the upgrade path if
// families ever need separate norming.
func abilityOf(responses []session.Response) float64 {
	bySection := map[int][]model.Outcome{}
	var order []int
	for _, r := range responses {
		if _, seen := bySection[r.Section]; !seen {
			order = append(order, r.Section)
		}
		bySection[r.Section] = append(bySection[r.Section], model.Outcome{Correct: r.Correct, Band: r.Difficulty})
	}
	if len(order) == 0 {
		return 0
	}
	var weighted, total float64
	for _, si := range order {
		outcomes := bySection[si]
		weighted += model.AbilityFromStaircase(outcomes) * float64(len(outcomes))
		total += float64(len(outcomes))
	}
	return weighted / total
}

// speedOf reduces the response elapsed times to the speed dimension.
func speedOf(responses []session.Response) model.Speed {
	if len(responses) == 0 {
		return model.Speed{}
	}
	var total time.Duration
	correct := 0
	for _, r := range responses {
		total += r.Elapsed
		if r.Correct {
			correct++
		}
	}
	sp := model.Speed{Total: total, Mean: total / time.Duration(len(responses))}
	if total > 0 {
		sp.CorrectPerMinute = float64(correct) / total.Minutes()
	}
	return sp
}

// feedback renders per-item feedback in delivery order, reading the correct
// answer and explanation from the bank. An item removed since administration is
// not an error: the frozen r.Correct already scored it, so its feedback text
// just degrades to blank (see the package doc on drift-immunity) and is counted
// in the returned degraded tally.
func (s *Service) feedback(ctx context.Context, responses []session.Response) ([]model.ItemFeedback, int, error) {
	out := make([]model.ItemFeedback, len(responses))
	degraded := 0
	for i, r := range responses {
		fb := model.ItemFeedback{
			ItemID:  r.ItemID,
			Correct: r.Correct,
			Elapsed: r.Elapsed,
		}
		it, err := s.bank.GetItem(ctx, item.ItemID(r.ItemID))
		switch {
		case err == nil:
			fb.CorrectAnswer = keyAnswer(it)
			fb.Explanation = it.Explanation
		case errors.Is(err, item.ErrUnknownItem):
			// ponytail: item gone since administration; frozen r.Correct still
			// scores, feedback text degrades to blank. Upgrade path = freeze the
			// key/explanation into the plan (Block 10 execution hardening).
			degraded++
		default:
			return nil, 0, err
		}
		// Render the answer by the item's format (it is the zero snapshot when the
		// item is gone, so givenAnswer falls back to whichever field is populated).
		fb.Given = givenAnswer(r.Answer, it.AnswerFormat)
		out[i] = fb
	}
	return out, degraded, nil
}

// givenAnswer renders the taker's answer for display, interpreted by the item's
// answer format (as keyAnswer does for the key), so a numeric 0 — a valid answer,
// e.g. "5 − 5" — renders as "0" while a genuinely blank multiple-choice answer
// renders as "". When the item is gone (unknown format) it falls back to
// whichever field the answer populated; the feedback text is already blank then.
func givenAnswer(ans session.Answer, format item.AnswerFormat) string {
	switch format {
	case item.FormatMultipleChoice:
		return ans.OptionID
	case item.FormatOpenNumeric:
		return strconv.FormatFloat(ans.Numeric, 'g', -1, 64)
	case item.FormatTrueFalseCannotSay:
		return ans.Verdict
	default:
		switch {
		case ans.OptionID != "":
			return ans.OptionID
		case ans.Verdict != "":
			return ans.Verdict
		default:
			return strconv.FormatFloat(ans.Numeric, 'g', -1, 64)
		}
	}
}

// keyAnswer renders an item's correct answer for display, by its answer format.
func keyAnswer(it item.ItemSnapshot) string {
	switch it.AnswerFormat {
	case item.FormatMultipleChoice:
		return it.AnswerKey.OptionID
	case item.FormatOpenNumeric:
		return strconv.FormatFloat(it.AnswerKey.Numeric, 'g', -1, 64)
	case item.FormatTrueFalseCannotSay:
		return string(it.AnswerKey.Verdict)
	default:
		return ""
	}
}
