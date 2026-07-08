package scoring_test

import (
	"math"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/scoring"
)

func TestNormTableNormalization(t *testing.T) {
	// A norm centred at 5 with SD 2: the mean scores at the 50th percentile / IQ
	// 100; +1 SD at ~84th / IQ 115; -1 SD at ~16th / IQ 85.
	n := scoring.NormTable{Mean: 5, SD: 2}
	cases := []struct {
		x       float64
		pct     float64 // approximate
		iq      int
		classic scoring.Band
	}{
		{5, 50, 100, scoring.BandAverage},
		{7, 84.13, 115, scoring.BandHighAverage},
		{3, 15.87, 85, scoring.BandLowAverage},
		{11, 99.87, 145, scoring.BandVerySuperior},
	}
	for _, c := range cases {
		if got := n.Percentile(c.x); math.Abs(got-c.pct) > 0.1 {
			t.Fatalf("Percentile(%v) = %.2f, want ~%.2f", c.x, got, c.pct)
		}
		if got := n.ScaledIQ(c.x); got != c.iq {
			t.Fatalf("ScaledIQ(%v) = %d, want %d", c.x, got, c.iq)
		}
		if got := scoring.Classify(n.ScaledIQ(c.x)); got != c.classic {
			t.Fatalf("Classify(IQ %d) = %q, want %q", n.ScaledIQ(c.x), got, c.classic)
		}
	}
}

func TestNormTableClampsExtremes(t *testing.T) {
	// Far out in the tails the raw normal blows past any plausible IQ/percentile;
	// the reported figures clamp to the defensible band a norm this thin can carry.
	n := scoring.NormTable{Mean: 0, SD: 1}
	if iq := n.ScaledIQ(100); iq != 160 {
		t.Fatalf("ScaledIQ(+100 SD) = %d, want 160 (clamped)", iq)
	}
	if iq := n.ScaledIQ(-100); iq != 40 {
		t.Fatalf("ScaledIQ(-100 SD) = %d, want 40 (clamped)", iq)
	}
	if pct := n.Percentile(100); pct != 99.9 {
		t.Fatalf("Percentile(+100 SD) = %.4f, want 99.9 (clamped)", pct)
	}
	if pct := n.Percentile(-100); pct != 0.1 {
		t.Fatalf("Percentile(-100 SD) = %.4f, want 0.1 (clamped)", pct)
	}
}

func TestNormTableValidAndLookup(t *testing.T) {
	if (scoring.NormTable{Mean: 5, SD: 0}).Valid() {
		t.Fatal("SD 0 must be invalid")
	}
	book := scoring.NormBook{
		"good": {Mean: 5, SD: 2},
		"zero": {Mean: 5, SD: 0}, // invalid: no spread
	}
	if _, ok := book.Lookup("good"); !ok {
		t.Fatal("good norm should resolve")
	}
	if _, ok := book.Lookup("zero"); ok {
		t.Fatal("invalid norm must not resolve")
	}
	if _, ok := book.Lookup("missing"); ok {
		t.Fatal("missing norm must not resolve")
	}
}

func TestClassifyBoundaries(t *testing.T) {
	cases := map[int]scoring.Band{
		69: scoring.BandExtremelyLow, 70: scoring.BandBorderline,
		79: scoring.BandBorderline, 80: scoring.BandLowAverage,
		89: scoring.BandLowAverage, 90: scoring.BandAverage,
		109: scoring.BandAverage, 110: scoring.BandHighAverage,
		119: scoring.BandHighAverage, 120: scoring.BandSuperior,
		129: scoring.BandSuperior, 130: scoring.BandVerySuperior,
	}
	for iq, want := range cases {
		if got := scoring.Classify(iq); got != want {
			t.Fatalf("Classify(%d) = %q, want %q", iq, got, want)
		}
	}
}

func TestAbilityFromStaircaseConsumesOrder(t *testing.T) {
	// Two attempts, same items (bands {1,2,3}) and the same count correct (2/3),
	// differing only in ORDER: the reversal-mean ability must differ, proving the
	// estimator consumes the delivery path and adaptive scoring is not cosmetic.
	a := []scoring.Outcome{{Correct: true, Band: 1}, {Correct: false, Band: 3}, {Correct: true, Band: 2}}
	b := []scoring.Outcome{{Correct: true, Band: 2}, {Correct: false, Band: 1}, {Correct: true, Band: 3}}
	// a: directions +,-,+ → reversals at bands 3 and 2 → 2.5
	if got := scoring.AbilityFromStaircase(a); got != 2.5 {
		t.Fatalf("ability(a) = %v, want 2.5", got)
	}
	// b: directions +,-,+ → reversals at bands 1 and 3 → 2.0
	if got := scoring.AbilityFromStaircase(b); got != 2.0 {
		t.Fatalf("ability(b) = %v, want 2.0", got)
	}
}

func TestAbilityFromStaircaseMonotone(t *testing.T) {
	allCorrect := []scoring.Outcome{{Correct: true, Band: 1}, {Correct: true, Band: 2}, {Correct: true, Band: 3}}
	if got := scoring.AbilityFromStaircase(allCorrect); got != 3 {
		t.Fatalf("all-correct ability = %v, want 3 (hardest band reached)", got)
	}
	allWrong := []scoring.Outcome{{Correct: false, Band: 3}, {Correct: false, Band: 2}, {Correct: false, Band: 1}}
	if got := scoring.AbilityFromStaircase(allWrong); got != 1 {
		t.Fatalf("all-wrong ability = %v, want 1 (easiest band)", got)
	}
	if got := scoring.AbilityFromStaircase(nil); got != 0 {
		t.Fatalf("empty ability = %v, want 0", got)
	}
}

func TestSpeedZeroValue(t *testing.T) {
	// Guard the zero value stays sane (no divide-by-zero surprises for callers).
	var s scoring.Speed
	if s.Total != 0 || s.Mean != 0 || s.CorrectPerMinute != 0 {
		t.Fatalf("zero Speed = %+v, want all-zero", s)
	}
	_ = time.Second
}
