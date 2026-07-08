package rulegen

import (
	"strconv"
	"testing"
)

// TestNewRNGDoesNotFoldSeeds guards the round-3 regression at the PRNG level,
// independent of how rich the figural rule space is: the legacy math/rand source
// folds the seed modulo 2^31-1, so seeds s and s+(2^31-1) shared an identical
// stream. newRNG must draw different sequences for that collision class.
func TestNewRNGDoesNotFoldSeeds(t *testing.T) {
	const fold = int64(1)<<31 - 1 // the modulus the legacy source collapses on
	for _, s := range []int64{0, 1, 2, 999, 123456789} {
		a, b := newRNG(s), newRNG(s+fold)
		same := true
		for i := 0; i < 8; i++ {
			if a.Uint64() != b.Uint64() {
				same = false
				break
			}
		}
		if same {
			t.Fatalf("seed %d and %d produced the same stream — PRNG is folding the seed", s, s+fold)
		}
	}
}

// TestOddOneOutStructureIsUnambiguous proves the A4 construction at the figure
// level: for every band and many seeds the five figures are pairwise visually
// distinct (so the item cannot be solved by "pick the only non-duplicate
// image") and exactly one attribute is shared by four figures — the invariant —
// with the odd figure being the one that lacks it. That is the definition of an
// unambiguous odd-one-out, verified structurally rather than by eyeballing SVG.
func TestOddOneOutStructureIsUnambiguous(t *testing.T) {
	for band := 1; band <= 5; band++ {
		for seed := int64(0); seed < 300; seed++ {
			rng := newRNG(seed)
			odd, conformers, _ := oddOneOut(rng, band)
			all := append([]figure{odd}, conformers...)

			if len(all) != 5 {
				t.Fatalf("band %d seed %d: got %d figures, want 5", band, seed, len(all))
			}
			assertAllVisuallyDistinct(t, band, seed, all)
			assertSingleFourShareInvariant(t, band, seed, odd, all)
		}
	}
}

func assertAllVisuallyDistinct(t *testing.T, band int, seed int64, all []figure) {
	t.Helper()
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if all[i].sameVisual(all[j]) {
				t.Fatalf("band %d seed %d: figures %d and %d render identically (%s)",
					band, seed, i, j, all[i].visualKey())
			}
		}
	}
}

// assertSingleFourShareInvariant checks that exactly one attribute has a value
// shared by exactly four of the five figures, and that the odd figure is not in
// that majority — the structural guarantee of a unique, unambiguous answer.
func assertSingleFourShareInvariant(t *testing.T, band int, seed int64, odd figure, all []figure) {
	t.Helper()
	attrs := map[string]func(figure) string{
		"shape":       func(f figure) string { return string(f.shape) },
		"count":       func(f figure) string { return strconv.Itoa(f.count) },
		"orientation": func(f figure) string { return strconv.Itoa(canonicalOrientation(f.shape, f.orientation)) },
		"fill":        func(f figure) string { return strconv.FormatBool(f.filled) },
	}

	fourShares := 0
	for name, val := range attrs {
		freq := map[string]int{}
		for _, f := range all {
			freq[val(f)]++
		}
		for v, n := range freq {
			if n == 4 {
				fourShares++
				if val(odd) == v {
					t.Fatalf("band %d seed %d: odd figure shares the majority %s=%s (not the odd one out)",
						band, seed, name, v)
				}
			}
		}
	}
	if fourShares != 1 {
		t.Fatalf("band %d seed %d: want exactly one four-share invariant attribute, got %d",
			band, seed, fourShares)
	}
}
