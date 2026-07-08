package scoring_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/app/scoring"
	"github.com/mariotoffia/testmaker/domain/item"
	model "github.com/mariotoffia/testmaker/domain/scoring"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// Compile-time proof the service satisfies the driving port.
var _ ports.Scorer = (*scoring.Service)(nil)

// --- in-process item bank fake (no adapter imports, no I/O) ------------------

type fakeBank struct {
	items map[item.ItemID]item.ItemSnapshot
}

func newBank(snaps ...item.ItemSnapshot) *fakeBank {
	b := &fakeBank{items: map[item.ItemID]item.ItemSnapshot{}}
	for _, s := range snaps {
		b.items[s.ID] = s
	}
	return b
}

func (b *fakeBank) SaveItem(_ context.Context, snap item.ItemSnapshot) error {
	b.items[snap.ID] = snap
	return nil
}

func (b *fakeBank) GetItem(_ context.Context, id item.ItemID) (item.ItemSnapshot, error) {
	s, ok := b.items[id]
	if !ok {
		return item.ItemSnapshot{}, item.ErrUnknownItem
	}
	return s, nil
}

func (b *fakeBank) ListItems(_ context.Context, _ item.ItemFilter) ([]item.ItemSnapshot, error) {
	out := make([]item.ItemSnapshot, 0, len(b.items))
	for _, s := range b.items {
		out = append(out, s)
	}
	return out, nil
}

// mcItem builds a valid multiple-choice bank item with a known key + explanation.
func mcItem(t *testing.T, id, key, explanation string, band int) item.ItemSnapshot {
	t.Helper()
	it, err := item.NewItem(item.ItemSpec{
		ID:           item.ItemID(id),
		Provenance:   item.Provenance{SourceID: "rulegen", Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
		TestType:     "A2",
		Stimulus:     []item.StimulusPart{{Text: "which figure continues?"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:   item.AnswerKey{OptionID: key},
		Explanation: explanation,
		Difficulty:  item.Difficulty{Band: band},
	})
	if err != nil {
		t.Fatalf("build item %s: %v", id, err)
	}
	return it.Snapshot()
}

// numItemS builds a valid open-numeric bank item with a known key + explanation.
func numItemS(t *testing.T, id string, key float64, explanation string, band int) item.ItemSnapshot {
	t.Helper()
	it, err := item.NewItem(item.ItemSpec{
		ID:           item.ItemID(id),
		Provenance:   item.Provenance{SourceID: "rulegen", Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
		TestType:     "B1",
		Stimulus:     []item.StimulusPart{{Text: "5 - 5 = ?"}},
		AnswerFormat: item.FormatOpenNumeric,
		AnswerKey:    item.AnswerKey{Numeric: key},
		Explanation:  explanation,
		Difficulty:   item.Difficulty{Band: band},
	})
	if err != nil {
		t.Fatalf("build numeric item %s: %v", id, err)
	}
	return it.Snapshot()
}

func resp(id string, band, section int, answer string, correct bool, elapsed time.Duration) session.Response {
	return session.Response{
		ItemID:     id,
		Difficulty: band,
		Section:    section,
		Answer:     session.Answer{OptionID: answer},
		Elapsed:    elapsed,
		Correct:    correct,
	}
}

// completed builds a completed session snapshot over the given responses.
func completed(testID string, policy session.Policy, responses ...session.Response) session.SessionSnapshot {
	return session.SessionSnapshot{
		ID:        "sess-1",
		TestID:    testID,
		Policy:    policy,
		State:     session.StateCompleted,
		Responses: responses,
	}
}

// --- fixed-policy scoring ----------------------------------------------------

func TestScoreFixedNormedWithFeedback(t *testing.T) {
	ctx := context.Background()
	bank := newBank(
		mcItem(t, "i1", "b", "rotate 90", 1),
		mcItem(t, "i2", "b", "add a dot", 2),
		mcItem(t, "i3", "b", "mirror it", 3),
		mcItem(t, "i4", "b", "count sides", 4),
	)
	// 3 of 4 correct. Norm Mean 2, SD 1 ⇒ z = 1 ⇒ IQ 115, ~84th percentile.
	norms := model.NormBook{"t-fixed": {Mean: 2, SD: 1}}
	svc := scoring.NewService(bank, norms)

	snap := completed("t-fixed", session.PolicyFixedIncreasing,
		resp("i1", 1, 0, "b", true, 20*time.Second),  // correct
		resp("i2", 2, 0, "a", false, 10*time.Second), // wrong
		resp("i3", 3, 0, "b", true, 15*time.Second),  // correct
		resp("i4", 4, 0, "b", true, 15*time.Second),  // correct
	)

	score, err := svc.Score(ctx, snap)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if score.Raw != 3 || score.Max != 4 {
		t.Fatalf("raw/max = %d/%d, want 3/4", score.Raw, score.Max)
	}
	if !score.Normed {
		t.Fatal("want normed")
	}
	if score.ScaledIQ != 115 {
		t.Fatalf("scaled IQ = %d, want 115", score.ScaledIQ)
	}
	if score.Band != model.BandHighAverage {
		t.Fatalf("band = %q, want high-average", score.Band)
	}
	if score.Ability != 0 {
		t.Fatalf("fixed ability = %v, want 0 (fixed norms on Raw)", score.Ability)
	}
	// Speed: 60s over 4 items ⇒ 15s mean; 3 correct / 1 min = 3/min.
	if score.Speed.Total != 60*time.Second || score.Speed.Mean != 15*time.Second {
		t.Fatalf("speed = %+v, want total 60s mean 15s", score.Speed)
	}
	if score.Speed.CorrectPerMinute != 3 {
		t.Fatalf("correct/min = %v, want 3", score.Speed.CorrectPerMinute)
	}
	// Feedback carries the frozen grade plus the bank's key + explanation, in order.
	if len(score.Items) != 4 {
		t.Fatalf("feedback items = %d, want 4", len(score.Items))
	}
	fb := score.Items[1] // the wrong answer to i2
	if fb.ItemID != "i2" || fb.Correct {
		t.Fatalf("feedback[1] = %+v, want i2 incorrect", fb)
	}
	if fb.Given != "a" || fb.CorrectAnswer != "b" || fb.Explanation != "add a dot" {
		t.Fatalf("feedback[1] = %+v, want given a / correct b / 'add a dot'", fb)
	}
}

func TestScoreUnnormedTestIsRawOnly(t *testing.T) {
	ctx := context.Background()
	bank := newBank(mcItem(t, "i1", "b", "x", 1), mcItem(t, "i2", "b", "y", 2))
	svc := scoring.NewService(bank, nil) // no norms at all

	snap := completed("t-nonorm", session.PolicyFixedIncreasing,
		resp("i1", 1, 0, "b", true, time.Second),
		resp("i2", 2, 0, "a", false, time.Second),
	)
	score, err := svc.Score(ctx, snap)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if score.Raw != 1 || score.Normed || score.ScaledIQ != 0 || score.Percentile != 0 {
		t.Fatalf("unnormed score = %+v, want raw 1 and no norm values", score)
	}
	if score.Band != model.BandUnnormed {
		t.Fatalf("band = %q, want unnormed", score.Band)
	}
}

// --- adaptive scoring consumes the delivery ORDER ----------------------------

func TestScoreAdaptiveConsumesDeliveryOrder(t *testing.T) {
	ctx := context.Background()
	bank := newBank(
		mcItem(t, "b1", "b", "", 1), mcItem(t, "b2", "b", "", 2), mcItem(t, "b3", "b", "", 3),
	)
	norms := model.NormBook{"t-adapt": {Mean: 2, SD: 1}}
	svc := scoring.NewService(bank, norms)

	// Same items, same count correct (2/3) — only the order differs.
	// A: C(b1), W(b3), C(b2) ⇒ reversals at bands 3,2 ⇒ ability 2.5 ⇒ IQ 108.
	a := completed("t-adapt", session.PolicyAdaptive,
		resp("b1", 1, 0, "b", true, time.Second),
		resp("b3", 3, 0, "a", false, time.Second),
		resp("b2", 2, 0, "b", true, time.Second),
	)
	// B: C(b2), W(b1), C(b3) ⇒ reversals at bands 1,3 ⇒ ability 2.0 ⇒ IQ 100.
	b := completed("t-adapt", session.PolicyAdaptive,
		resp("b2", 2, 0, "b", true, time.Second),
		resp("b1", 1, 0, "a", false, time.Second),
		resp("b3", 3, 0, "b", true, time.Second),
	)

	sa, err := svc.Score(ctx, a)
	if err != nil {
		t.Fatalf("score a: %v", err)
	}
	sb, err := svc.Score(ctx, b)
	if err != nil {
		t.Fatalf("score b: %v", err)
	}
	if sa.Raw != sb.Raw {
		t.Fatalf("raw a=%d b=%d, want equal (order must not change the count)", sa.Raw, sb.Raw)
	}
	if sa.Ability != 2.5 || sb.Ability != 2.0 {
		t.Fatalf("ability a=%v b=%v, want 2.5 and 2.0 (order-dependent)", sa.Ability, sb.Ability)
	}
	if sa.ScaledIQ == sb.ScaledIQ {
		t.Fatalf("adaptive IQ identical (%d) despite different paths — adaptive is cosmetic", sa.ScaledIQ)
	}
	if sa.ScaledIQ != 108 || sb.ScaledIQ != 100 {
		t.Fatalf("adaptive IQ a=%d b=%d, want 108 and 100", sa.ScaledIQ, sb.ScaledIQ)
	}
}

// --- error + resilience paths ------------------------------------------------

func TestScoreAdaptiveWeightsAbilityBySectionLength(t *testing.T) {
	ctx := context.Background()
	bank := newBank(
		mcItem(t, "s0", "b", "", 4),
		mcItem(t, "s1a", "b", "", 1), mcItem(t, "s1b", "b", "", 1), mcItem(t, "s1c", "b", "", 1),
	)
	svc := scoring.NewService(bank, model.NormBook{"t-adapt": {Mean: 2, SD: 1}})

	// Section 0: one correct at band 4 (monotone ⇒ ability 4).
	// Section 1: three correct at band 1 (monotone ⇒ ability 1).
	// A plain per-section mean would be (4+1)/2 = 2.5; the response-count-weighted
	// mean is (4·1 + 1·3)/4 = 1.75, so the longer section pulls proportionally.
	snap := completed("t-adapt", session.PolicyAdaptive,
		resp("s0", 4, 0, "b", true, time.Second),
		resp("s1a", 1, 1, "b", true, time.Second),
		resp("s1b", 1, 1, "b", true, time.Second),
		resp("s1c", 1, 1, "b", true, time.Second),
	)
	score, err := svc.Score(ctx, snap)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if score.Ability != 1.75 {
		t.Fatalf("weighted ability = %v, want 1.75 (an unweighted mean would be 2.5)", score.Ability)
	}
}

// TestScoreRendersNumericZeroAsGiven guards the regression where a legitimate
// numeric answer of 0 (e.g. "5 − 5") rendered as blank: 0 is a real answer.
func TestScoreRendersNumericZeroAsGiven(t *testing.T) {
	ctx := context.Background()
	bank := newBank(numItemS(t, "n0", 0, "five minus five is zero", 1))
	svc := scoring.NewService(bank, model.NormBook{"t": {Mean: 0, SD: 1}})

	snap := completed("t", session.PolicyFixedIncreasing,
		session.Response{
			ItemID: "n0", Difficulty: 1, Section: 0,
			Answer:  session.Answer{Numeric: 0},
			Elapsed: time.Second, Correct: true,
		},
	)
	score, err := svc.Score(ctx, snap)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	fb := score.Items[0]
	if fb.Given != "0" {
		t.Fatalf("given numeric-0 answer = %q, want %q (0 is a real answer, not blank)", fb.Given, "0")
	}
	if fb.CorrectAnswer != "0" {
		t.Fatalf("correct answer = %q, want %q", fb.CorrectAnswer, "0")
	}
}

// TestScoreRendersBlankMultipleChoiceAsBlank is the other half of the format
// dispatch: a blank multiple-choice answer must render "", not a spurious "0".
func TestScoreRendersBlankMultipleChoiceAsBlank(t *testing.T) {
	ctx := context.Background()
	bank := newBank(mcItem(t, "m1", "b", "because b", 1))
	svc := scoring.NewService(bank, model.NormBook{"t": {Mean: 0, SD: 1}})

	snap := completed("t", session.PolicyFixedIncreasing,
		session.Response{
			ItemID: "m1", Difficulty: 1, Section: 0,
			Answer:  session.Answer{},
			Elapsed: time.Second, Correct: false,
		},
	)
	score, err := svc.Score(ctx, snap)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if got := score.Items[0].Given; got != "" {
		t.Fatalf("blank multiple-choice answer rendered %q, want %q", got, "")
	}
}

func TestScoreRejectsEmptyCompletedSession(t *testing.T) {
	ctx := context.Background()
	// A norm is present precisely to prove the guard fires *before* norming: without
	// it, Raw 0 would stamp a confident low IQ/band on an attempt that answered nothing.
	svc := scoring.NewService(newBank(), model.NormBook{"t": {Mean: 5, SD: 2}})
	snap := completed("t", session.PolicyFixedIncreasing) // completed, zero responses
	if _, err := svc.Score(ctx, snap); !errors.Is(err, model.ErrNotScorable) {
		t.Fatalf("empty completed session: err = %v, want ErrNotScorable", err)
	}
}

func TestScoreRejectsIncompleteSession(t *testing.T) {
	ctx := context.Background()
	svc := scoring.NewService(newBank(), nil)
	for _, st := range []session.State{session.StateCreated, session.StateInProgress, session.StateAbandoned} {
		snap := session.SessionSnapshot{ID: "s", TestID: "t", Policy: session.PolicyFixedIncreasing, State: st}
		if _, err := svc.Score(ctx, snap); !errors.Is(err, model.ErrNotScorable) {
			t.Fatalf("state %s: err = %v, want ErrNotScorable", st, err)
		}
	}
}

func TestScoreFeedbackDegradesForMissingItem(t *testing.T) {
	ctx := context.Background()
	// i1 is in the bank; i2 was administered but has since been removed.
	bank := newBank(mcItem(t, "i1", "b", "keep", 1))
	svc := scoring.NewService(bank, model.NormBook{"t": {Mean: 1, SD: 1}})

	snap := completed("t", session.PolicyFixedIncreasing,
		resp("i1", 1, 0, "b", true, time.Second),
		resp("i2", 2, 0, "a", false, time.Second),
	)
	score, err := svc.Score(ctx, snap)
	if err != nil {
		t.Fatalf("score with a missing item must not fail: %v", err)
	}
	if score.Raw != 1 || score.Max != 2 {
		t.Fatalf("raw/max = %d/%d, want 1/2 (frozen grades still score)", score.Raw, score.Max)
	}
	gone := score.Items[1]
	if gone.ItemID != "i2" || gone.Correct {
		t.Fatalf("feedback[1] = %+v, want i2 incorrect", gone)
	}
	if gone.CorrectAnswer != "" || gone.Explanation != "" {
		t.Fatalf("feedback[1] text = %+v, want blank (item removed)", gone)
	}
	if gone.Given != "a" {
		t.Fatalf("feedback[1] given = %q, want a (from the frozen response)", gone.Given)
	}
	if score.DegradedFeedback != 1 {
		t.Fatalf("degraded feedback = %d, want 1 (one item removed)", score.DegradedFeedback)
	}
}
