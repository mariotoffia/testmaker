package ingest

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// ErrVIQTData marks VIQT source artifacts that are missing or unparseable (the
// codebook and/or response CSV the normalizer needs).
var ErrVIQTData = &shared.TestmakerError{
	Code: "ingest.viqt_data", Class: shared.ClassInvalid, Message: "viqt source artifacts missing or malformed",
}

// VIQTSourceID is the catalogue id this normalizer maps.
const VIQTSourceID source.SourceID = "openpsych-viqt"

// viqtTestType is the verbal taxonomy code for the VIQT synonym items.
const viqtTestType shared.TestTypeCode = "C3"

// viqtItemLine matches a codebook item row: "Q<n> <ans> <five words>".
var viqtItemLine = regexp.MustCompile(`^Q(\d+)\s+(\d+)\s+(.+)$`)

// viqtPair decodes the codebook's answer integer (a binary mask over the five
// words, 1=selected) into the 0-based indices of the two synonyms. The ten
// codes are the only "exactly two of five selected" masks.
func viqtPair(code int) (first, key int, ok bool) {
	switch code {
	case 3:
		return 0, 1, true
	case 5:
		return 0, 2, true
	case 6:
		return 1, 2, true
	case 9:
		return 0, 3, true
	case 10:
		return 1, 3, true
	case 12:
		return 2, 3, true
	case 17:
		return 0, 4, true
	case 18:
		return 1, 4, true
	case 20:
		return 2, 4, true
	case 24:
		return 3, 4, true
	default:
		return 0, 0, false
	}
}

// viqtCodebookItem is one parsed codebook question.
type viqtCodebookItem struct {
	q     int
	code  int      // the recorded answer integer (matches CSV response values)
	first int      // 0-based index of the first synonym (the stem)
	key   int      // 0-based index of the second synonym (the correct option)
	words []string // the five words, in list order
}

// VIQTNormalizer maps the OpenPsychometrics Vocabulary IQ Test artifacts into
// four-option synonym multiple-choice items. Each codebook question lists five
// words of which two are synonyms; the item asks which option is a synonym of
// the first, with the four remaining words as options. Difficulty comes from
// the response CSV: the proportion of all takers who chose the correct pair,
// binned into ordinal bands.
//
// It is a pure transform over the fetched RawItems (codebook.txt + the response
// CSV); the service validates every returned spec through item.NewItem.
func VIQTNormalizer(snap source.Snapshot, raw []ports.RawItem) ([]item.ItemSpec, error) {
	codebook, ok := rawContent(raw, "codebook.txt")
	if !ok {
		return nil, ErrVIQTData.WithMessage("codebook.txt not found among fetched artifacts")
	}
	csv, ok := rawContent(raw, ".csv")
	if !ok {
		return nil, ErrVIQTData.WithMessage("response .csv not found among fetched artifacts")
	}

	items := parseVIQTCodebook(codebook)
	if len(items) == 0 {
		return nil, ErrVIQTData.WithMessage("no parseable questions in codebook.txt")
	}
	pByQ, err := viqtPValues(csv, items)
	if err != nil {
		return nil, err
	}

	specs := make([]item.ItemSpec, 0, len(items))
	for _, ci := range items {
		p, ok := pByQ[ci.q]
		if !ok {
			// No response column / no responses -> no honest difficulty: skip.
			continue
		}
		specs = append(specs, viqtSpec(snap, ci, p))
	}
	if len(specs) == 0 {
		// A valid codebook that yields no items means the response CSV never
		// lined up with it — malformed input, not an empty-but-fine run.
		return nil, ErrVIQTData.WithMessage("no codebook question had usable response data")
	}
	return specs, nil
}

// parseVIQTCodebook extracts the questions whose answer decodes to a synonym
// pair over exactly five distinct words. Malformed rows are skipped.
func parseVIQTCodebook(codebook string) []viqtCodebookItem {
	var out []viqtCodebookItem
	for _, line := range strings.Split(codebook, "\n") {
		m := viqtItemLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		q, _ := strconv.Atoi(m[1])
		ans, _ := strconv.Atoi(m[2])
		words := strings.Fields(m[3])
		first, key, ok := viqtPair(ans)
		if !ok || len(words) != 5 || !distinct(words) {
			continue
		}
		out = append(out, viqtCodebookItem{q: q, code: ans, first: first, key: key, words: words})
	}
	return out
}

// viqtPValues computes, per question, the proportion of all response rows that
// recorded the correct answer code. A tab-delimited header names the Q columns.
func viqtPValues(csv string, items []viqtCodebookItem) (map[int]float64, error) {
	lines := strings.Split(csv, "\n")
	if len(lines) < 2 {
		return nil, ErrVIQTData.WithMessage("response csv has no data rows")
	}
	col, ans := viqtColumns(lines[0], items)
	if len(col) == 0 {
		return nil, ErrVIQTData.WithMessage("response csv has no Q columns")
	}

	correct, total := viqtTally(lines[1:], col, ans)

	p := make(map[int]float64, len(col))
	for q, tot := range total {
		if tot > 0 {
			p[q] = float64(correct[q]) / float64(tot)
		}
	}
	return p, nil
}

// viqtColumns maps each question to its CSV column index and correct answer
// code, keyed off the tab-delimited header row.
func viqtColumns(headerLine string, items []viqtCodebookItem) (col, ans map[int]int) {
	header := strings.Split(strings.TrimRight(headerLine, "\r"), "\t")
	colByName := make(map[string]int, len(header))
	for i, h := range header {
		colByName[strings.TrimSpace(h)] = i
	}
	col = map[int]int{}
	ans = map[int]int{}
	for _, ci := range items {
		if c, ok := colByName["Q"+strconv.Itoa(ci.q)]; ok {
			col[ci.q] = c
			ans[ci.q] = ci.code
		}
	}
	return col, ans
}

// viqtTally counts, per question, the correct responses and the total parseable
// responses across the data rows.
func viqtTally(rows []string, col, ans map[int]int) (correct, total map[int]int) {
	correct = map[int]int{}
	total = map[int]int{}
	for _, line := range rows {
		if line == "" {
			continue
		}
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		for q, c := range col {
			v, ok := fieldInt(fields, c)
			if !ok {
				continue
			}
			total[q]++
			if v == ans[q] {
				correct[q]++
			}
		}
	}
	return correct, total
}

// fieldInt parses the integer in fields[c], reporting ok=false when the column
// is absent or the value is not an integer.
func fieldInt(fields []string, c int) (int, bool) {
	if c >= len(fields) {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(fields[c]))
	if err != nil {
		return 0, false
	}
	return v, true
}

// viqtSpec builds one item spec from a codebook question and its p-value.
func viqtSpec(snap source.Snapshot, ci viqtCodebookItem, p float64) item.ItemSpec {
	stem := ci.words[ci.first]
	keyWord := ci.words[ci.key]

	// Options = the four words other than the stem, in list order; their ids are
	// stable ("o<index>") so the answer key references an existing option.
	options := make([]item.Option, 0, 4)
	for idx, w := range ci.words {
		if idx == ci.first {
			continue
		}
		options = append(options, item.Option{ID: "o" + strconv.Itoa(idx), Text: w})
	}

	return item.ItemSpec{
		ID: item.ItemID(fmt.Sprintf("%s-q%d", snap.ID, ci.q)),
		Provenance: item.Provenance{
			SourceID:        string(snap.ID),
			Origin:          item.OriginFetched,
			Redistributable: snap.License.Redistributable,
		},
		TestType:     viqtTestType,
		Stimulus:     []item.StimulusPart{{Text: fmt.Sprintf("Which word means the same as %q?", stem)}},
		AnswerFormat: item.FormatMultipleChoice,
		Options:      options,
		AnswerKey:    item.AnswerKey{OptionID: "o" + strconv.Itoa(ci.key)},
		Explanation:  fmt.Sprintf("%q and %q have the same meaning.", stem, keyWord),
		Difficulty:   item.Difficulty{Band: viqtBand(p)},
	}
}

// viqtBand bins a proportion-correct into an ordinal difficulty band: the more
// takers answered correctly, the easier (lower) the band.
// ponytail: fixed 5-way p-value split, no IRT calibration (that is Block 9).
func viqtBand(p float64) int {
	switch {
	case p >= 0.8:
		return 1
	case p >= 0.6:
		return 2
	case p >= 0.4:
		return 3
	case p >= 0.2:
		return 4
	default:
		return 5
	}
}

// rawContent returns the inlined text of the first RawItem whose ExternalID
// ends with suffix (case-insensitive).
func rawContent(raw []ports.RawItem, suffix string) (string, bool) {
	low := strings.ToLower(suffix)
	for _, it := range raw {
		if !strings.HasSuffix(strings.ToLower(it.ExternalID), low) {
			continue
		}
		if s, ok := it.Raw["content"].(string); ok {
			return s, true
		}
	}
	return "", false
}

// distinct reports whether all strings are unique.
func distinct(ss []string) bool {
	seen := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		if _, dup := seen[s]; dup {
			return false
		}
		seen[s] = struct{}{}
	}
	return true
}
