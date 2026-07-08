package rulegen

import (
	"math/rand/v2"
	"strings"

	"github.com/mariotoffia/testmaker/domain/item"
)

// This file holds the three native rule engines. Each builds a figural puzzle
// whose correct answer is *derived from the same rules that build the stimulus*
// — never guessed — so the key is ground-truth by construction. Distractors are
// distinct one-attribute perturbations, giving exactly one correct option.

// puzzle is the figural content one engine produces; Generate wraps it with the
// common item fields (id, provenance, difficulty). band is the *effective*
// difficulty tier the engine realized: rule complexity saturates at each
// family's top tier, so a request beyond it is capped here and the item is
// tagged honestly rather than labelled with an unrealized band.
type puzzle struct {
	stimulus    []item.StimulusPart
	options     []item.Option
	key         item.AnswerKey
	explanation string
	band        int
}

// genMatrix builds a 3x3 matrix (A2). Rule complexity grows with the band:
// element count always increases left-to-right; from band 2 the shape changes
// down each column; from band 3 fill alternates in a checkerboard. The missing
// bottom-right tile is the derived correct answer.
func genMatrix(rng *rand.Rand, band int) puzzle {
	rules := clamp(band, 1, 3)
	shape0 := pick(rng, shapes())
	fill0 := rng.IntN(2) == 1

	rowShapes := shapes()
	rng.Shuffle(len(rowShapes), func(i, j int) { rowShapes[i], rowShapes[j] = rowShapes[j], rowShapes[i] })

	cell := func(r, c int) figure {
		f := figure{shape: shape0, count: c + 1, orientation: 0, filled: fill0}
		if rules >= 2 {
			f.shape = rowShapes[r]
		}
		if rules >= 3 {
			f.filled = (r+c)%2 == 0
		}
		return f
	}

	cells := make([]figure, 9)
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			cells[r*3+c] = cell(r, c)
		}
	}
	const blankIdx = 8
	correct := cells[blankIdx]

	figs := append([]figure{correct}, distinctNeighbors(rng, correct, false, 4)...)
	opts, key := assembleOptions(rng, figs)

	ruleText := []string{"the number of elements increases from left to right"}
	if rules >= 2 {
		ruleText = append(ruleText, "the shape changes down each column")
	}
	if rules >= 3 {
		ruleText = append(ruleText, "filled and outlined alternate like a checkerboard")
	}
	return puzzle{
		stimulus: []item.StimulusPart{
			{Text: "Which figure completes the matrix?"},
			{MediaKind: item.MediaGrid, MediaRef: gridURI(cells, blankIdx)},
		},
		options:     opts,
		key:         key,
		band:        rules,
		explanation: "Missing tile: " + correct.describe() + ". Rule: " + strings.Join(ruleText, "; ") + ".",
	}
}

// genSeries builds a figure series (A1/A3): three figures follow a progression
// and the solver picks the fourth. Band 1 progresses element count; band 2
// progresses orientation (90° clockwise per step, on an asymmetric shape so the
// rotation is visible); band 3+ progresses both.
func genSeries(rng *rand.Rand, band int) puzzle {
	useOrient := band >= 2
	useCount := band == 1 || band >= 3

	shp := pick(rng, shapes())
	if useOrient {
		shp = pick(rng, asymmetricShapes())
	}
	fill0 := rng.IntN(2) == 1

	figAt := func(step int) figure {
		f := figure{shape: shp, count: 1, orientation: 0, filled: fill0}
		if useCount {
			f.count = step + 1
		}
		if useOrient {
			f.orientation = step * 90
		}
		return f
	}
	seq := []figure{figAt(0), figAt(1), figAt(2)}
	correct := figAt(3)

	figs := append([]figure{correct}, distinctNeighbors(rng, correct, useOrient, 4)...)
	opts, key := assembleOptions(rng, figs)

	var ruleText []string
	if useCount {
		ruleText = append(ruleText, "the number of elements increases by one each step")
	}
	if useOrient {
		ruleText = append(ruleText, "the figure rotates 90 degrees clockwise each step")
	}
	return puzzle{
		stimulus: []item.StimulusPart{
			{Text: "Which figure continues the series?"},
			{MediaKind: item.MediaSVG, MediaRef: stripURI(seq)},
		},
		options:     opts,
		key:         key,
		band:        clamp(band, 1, 3),
		explanation: "Next in series: " + correct.describe() + ". Rule: " + strings.Join(ruleText, "; ") + ".",
	}
}

// genOddOneOut builds a classification item (A4): four conformer figures that
// share exactly one invariant attribute plus one odd figure that breaks only
// that invariant. The invariant is chosen by the band (shape → fill → count →
// orientation, increasingly subtle). The conformers are varied along a decoy
// attribute so all five figures are pairwise visually distinct — the item
// therefore cannot be solved by "pick the only non-duplicate image", it requires
// spotting the shared property. oddOneOut guarantees no attribute other than the
// invariant is shared by four figures, so the odd is the single unambiguous
// answer.
func genOddOneOut(rng *rand.Rand, band int) puzzle {
	odd, conformers, ruleWord := oddOneOut(rng, band)
	figs := append([]figure{odd}, conformers...) // figs[0] is the odd = correct
	opts, key := assembleOptions(rng, figs)
	return puzzle{
		stimulus:    []item.StimulusPart{{Text: "Which figure does not belong with the others?"}},
		options:     opts,
		key:         key,
		band:        clamp(band, 1, 4),
		explanation: "The odd one out (" + odd.describe() + ") is the only figure that differs from the others in " + ruleWord + ".",
	}
}

// oddOneOut builds the five figures for a classification item: four conformers
// sharing one invariant attribute (value fixed) but varied along a decoy
// attribute so they are all visually distinct, plus one odd that differs only in
// the invariant. Construction guarantees (a) all five are pairwise distinct and
// (b) the invariant is the only attribute a value is shared by exactly four
// figures — so the odd is the lone, unambiguous answer. The decoy is count
// (four distinct counts) except when the invariant *is* count, where it is shape
// (four distinct shapes).
func oddOneOut(rng *rand.Rand, band int) (odd figure, conformers []figure, ruleWord string) {
	attr := clamp(band, 1, 4)
	shape0 := pick(rng, shapes())
	count0 := pick(rng, counts())
	fill0 := rng.IntN(2) == 1

	switch attr {
	case 2: // invariant: fill; decoy: count
		for _, c := range counts() {
			conformers = append(conformers, figure{shape0, c, 0, fill0})
		}
		odd = figure{shape0, pick(rng, counts()), 0, !fill0}
		ruleWord = "fill"
	case 3: // invariant: count; decoy: shape
		for _, s := range shapes() {
			conformers = append(conformers, figure{s, count0, 0, fill0})
		}
		odd = figure{pick(rng, shapes()), differentCount(rng, count0), 0, fill0}
		ruleWord = "the number of elements"
	case 4: // invariant: orientation; decoy: count (asymmetric shape so rotation shows)
		shape0 = pick(rng, asymmetricShapes())
		for _, c := range counts() {
			conformers = append(conformers, figure{shape0, c, 0, fill0})
		}
		odd = figure{shape0, pick(rng, counts()), orientations()[1+rng.IntN(3)], fill0}
		ruleWord = "orientation"
	default: // band 1 -> invariant: shape; decoy: count
		for _, c := range counts() {
			conformers = append(conformers, figure{shape0, c, 0, fill0})
		}
		odd = figure{differentShape(rng, shape0), pick(rng, counts()), 0, fill0}
		ruleWord = "shape"
	}
	return odd, conformers, ruleWord
}

// --- shared helpers --------------------------------------------------------

// assembleOptions shuffles figs (figs[0] is the correct answer), assigns option
// ids a, b, c, … and renders each figure to an SVG option, returning the answer
// key that points at wherever the correct figure landed.
func assembleOptions(rng *rand.Rand, figs []figure) ([]item.Option, item.AnswerKey) {
	order := rng.Perm(len(figs))
	opts := make([]item.Option, len(figs))
	var keyID string
	for pos, old := range order {
		id := optionID(pos)
		opts[pos] = item.Option{ID: id, MediaKind: item.MediaSVG, MediaRef: figureURI(figs[old])}
		if old == 0 {
			keyID = id
		}
	}
	return opts, item.AnswerKey{OptionID: keyID}
}

// distinctNeighbors returns up to n figures that each differ from base in
// exactly one attribute and are visually distinct from base and from each other
// — the plausible-but-wrong distractors. Orientation is varied only when
// allowOrientation is set (the base shape must then be asymmetric).
func distinctNeighbors(rng *rand.Rand, base figure, allowOrientation bool, n int) []figure {
	var cands []figure
	for _, s := range shapes() {
		if s != base.shape {
			cands = append(cands, figure{s, base.count, base.orientation, base.filled})
		}
	}
	for _, c := range counts() {
		if c != base.count {
			cands = append(cands, figure{base.shape, c, base.orientation, base.filled})
		}
	}
	cands = append(cands, figure{base.shape, base.count, base.orientation, !base.filled})
	if allowOrientation {
		for _, o := range orientations() {
			if o != base.orientation {
				cands = append(cands, figure{base.shape, base.count, o, base.filled})
			}
		}
	}

	seen := map[string]bool{base.visualKey(): true}
	uniq := make([]figure, 0, len(cands))
	for _, c := range cands {
		k := c.visualKey()
		if seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, c)
	}
	rng.Shuffle(len(uniq), func(i, j int) { uniq[i], uniq[j] = uniq[j], uniq[i] })
	if n < len(uniq) {
		uniq = uniq[:n]
	}
	return uniq
}

// optionID maps a 0-based index to a lower-case letter id (a, b, c, …).
func optionID(i int) string { return string(rune('a' + i)) }

// clamp bounds v to [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// pick returns a uniformly random element of xs (xs must be non-empty).
func pick[T any](rng *rand.Rand, xs []T) T { return xs[rng.IntN(len(xs))] }

// differentShape returns a shape other than s.
func differentShape(rng *rand.Rand, s shape) shape {
	for {
		if c := pick(rng, shapes()); c != s {
			return c
		}
	}
}

// differentCount returns a count other than c.
func differentCount(rng *rand.Rand, c int) int {
	for {
		if n := pick(rng, counts()); n != c {
			return n
		}
	}
}
