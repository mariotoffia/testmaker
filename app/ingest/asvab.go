package ingest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// ErrASVABData marks ASVAB source pages that carry quiz questions the
// normalizer cannot key: the base64 answer-key config is absent or unreadable.
var ErrASVABData = &shared.TestmakerError{
	Code: "ingest.asvab_data", Class: shared.ClassInvalid, Message: "asvab source pages missing or malformed",
}

// ASVABSourceID is the catalogue id this normalizer maps.
const ASVABSourceID source.SourceID = "asvab-official"

// asvabDefaultBand is the difficulty assigned to every ASVAB item. The official
// sample questions ship no per-item norms, so honest calibration is impossible
// here.
// ponytail: fixed mid-band placeholder until a normed source or IRT (Block 9).
const asvabDefaultBand = 3

// Regexes over the ARI Stream Quiz markup the ASVAB pages embed. RE2 (no
// backreferences); (?s) lets '.' span the newlines inside a title or label.
var (
	// asvabTitleCode pulls the two-letter subtest code out of the quiz heading,
	// e.g. `quiz-title">Word Knowledge (WK)` -> "WK".
	asvabTitleCode = regexp.MustCompile(`quiz-title"[^>]*>[^(<]*\(([A-Za-z]{2})\)`)
	// asvabQuestion matches a question div (`data-question-id="9">`, note the
	// bare '>') immediately followed by its title div, capturing id + title
	// HTML. Answer inputs carry the same attribute but end `" />`, so they never
	// match here.
	asvabQuestion = regexp.MustCompile(`(?s)data-question-id="(\d+)">\s*<div class="quiz-question-title"[^>]*>(.*?)</div>`)
	// asvabAnswer ties each answer to its question: the radio input carries
	// data-question-id and ends `" />`, immediately followed by the label whose
	// `for` names the numeric answer id that the config keys correctness by.
	asvabAnswer = regexp.MustCompile(`(?s)data-question-id="(\d+)"\s*/>\s*<label[^>]*_answer_(\d+)"[^>]*>(.*?)</label>`)
	// asvabB64 finds base64 runs that may hold the quiz config. The floor only
	// skips obviously-too-short tokens; the real filter is decode + unmarshal +
	// non-empty questions map in asvabAnswerKey, so a small single-question
	// config still qualifies.
	asvabB64 = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
	// asvabTag strips any HTML tag so a title/label becomes plain text.
	asvabTag = regexp.MustCompile(`<[^>]*>`)
)

// asvabConfig is the slice of the base64-encoded ARI quiz config the normalizer
// reads: the per-answer correctness flag, keyed by question id then answer id.
type asvabConfig struct {
	Pages []struct {
		Questions map[string]struct {
			Answers map[string]struct {
				Correct int `json:"correct"`
			} `json:"answers"`
		} `json:"questions"`
	} `json:"pages"`
}

// asvabAns is one parsed answer: its numeric id (the join key to the config)
// and its display text.
type asvabAns struct {
	id   string
	text string
}

// asvabTestType maps an ASVAB subtest code to the fine-grained taxonomy code.
// Subtests outside the cognitive taxonomy (General Science) return ok=false and
// are skipped rather than forced into a family they do not belong to.
func asvabTestType(code string) (shared.TestTypeCode, bool) {
	switch strings.ToUpper(code) {
	case "WK": // Word Knowledge — synonyms
		return "C3", true
	case "PC": // Paragraph Comprehension — reading comprehension
		return "C1", true
	case "AR": // Arithmetic Reasoning — word-problem calculation
		return "B2", true
	case "MK": // Mathematics Knowledge — calculation
		return "B2", true
	default: // GS (General Science) and anything else: not a cognitive family
		return "", false
	}
}

// ASVABNormalizer maps the official ASVAB sample-question pages into four-option
// multiple-choice items. Each subtest page embeds an ARI Stream Quiz: the
// question stems and answer labels live in the visible HTML, while the correct
// answer is carried by a base64-encoded JSON config joined to the labels on the
// numeric answer id. Pages for subtests outside the cognitive taxonomy (General
// Science) are skipped.
//
// It is a pure transform over the fetched RawItems (one per scraped subtest
// page); the service validates every returned spec through item.NewItem. ASVAB
// ships no per-item norms, so every item takes a fixed mid difficulty band.
func ASVABNormalizer(snap source.Snapshot, raw []ports.RawItem) ([]item.ItemSpec, error) {
	var specs []item.ItemSpec
	var firstErr error
	for _, it := range raw {
		content, ok := it.Raw["content"].(string)
		if !ok || content == "" {
			continue
		}
		pageSpecs, err := asvabPage(snap, content)
		if err != nil {
			// ponytail: one drifted subtest page must not sink the others —
			// remember the first failure but keep the pages that parsed, matching
			// the service's "one bad row must not sink the dataset" contract.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		specs = append(specs, pageSpecs...)
	}
	if len(specs) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, ErrASVABData.WithMessage("no keyable ASVAB questions in the fetched pages")
	}
	return specs, nil
}

// asvabPage turns one subtest page's HTML into item specs. A page with no quiz
// questions (e.g. an index) or an unsupported subtest is skipped (nil, nil); a
// page that has questions but no decodable answer-key config is an error.
func asvabPage(snap source.Snapshot, htmlContent string) ([]item.ItemSpec, error) {
	titles, order := asvabTitles(htmlContent)
	if len(order) == 0 {
		return nil, nil // not a quiz page
	}
	code := asvabSubtestCode(htmlContent)
	testType, ok := asvabTestType(code)
	if !ok {
		return nil, nil // subtest outside the cognitive taxonomy (e.g. GS): skip
	}
	correct, err := asvabAnswerKey(htmlContent)
	if err != nil {
		return nil, err
	}
	answers := asvabAnswers(htmlContent)

	specs := make([]item.ItemSpec, 0, len(order))
	for _, qid := range order {
		options, keyID := asvabOptions(answers[qid], correct[qid])
		if len(options) < 4 || len(options) > 6 || keyID == "" || titles[qid] == "" || !asvabDistinctText(options) {
			// ponytail: text-only normalizer. Skip rather than emit an invalid
			// spec — including questions whose stem is an image (some MK math
			// items render the equation as a figure, leaving an empty title);
			// pull those in when a media-extraction step lands.
			continue
		}
		specs = append(specs, asvabSpec(snap, code, qid, titles[qid], testType, options, keyID))
	}
	return specs, nil
}

// asvabDistinctText reports whether every option carries distinct display text.
// Option ids are always unique ("a"+answerId), so identical labels would slip
// past the domain's id-based uniqueness check and yield a nonsense MC item.
func asvabDistinctText(options []item.Option) bool {
	seen := make(map[string]struct{}, len(options))
	for _, o := range options {
		if _, dup := seen[o.Text]; dup {
			return false
		}
		seen[o.Text] = struct{}{}
	}
	return true
}

// asvabTitles returns each question's cleaned stem keyed by question id, plus
// the ids in document order.
func asvabTitles(htmlContent string) (map[string]string, []string) {
	titles := map[string]string{}
	var order []string
	for _, m := range asvabQuestion.FindAllStringSubmatch(htmlContent, -1) {
		qid := m[1]
		if _, seen := titles[qid]; !seen {
			order = append(order, qid)
		}
		titles[qid] = asvabText(m[2])
	}
	return titles, order
}

// asvabAnswers groups each question's answers (id + text) in document order.
func asvabAnswers(htmlContent string) map[string][]asvabAns {
	out := map[string][]asvabAns{}
	for _, m := range asvabAnswer.FindAllStringSubmatch(htmlContent, -1) {
		qid, aid, text := m[1], m[2], asvabText(m[3])
		out[qid] = append(out[qid], asvabAns{id: aid, text: text})
	}
	return out
}

// asvabOptions builds the option list (stable "a<answerId>" ids) and picks the
// key: the option whose answer id the config flags correct.
func asvabOptions(answers []asvabAns, correctForQ map[string]int) ([]item.Option, string) {
	options := make([]item.Option, 0, len(answers))
	keyID := ""
	for _, a := range answers {
		if a.text == "" {
			continue
		}
		optID := "a" + a.id
		options = append(options, item.Option{ID: optID, Text: a.text})
		if correctForQ[a.id] == 1 {
			keyID = optID
		}
	}
	return options, keyID
}

// asvabSubtestCode returns the two-letter subtest code from the quiz heading, or
// "" when none is present.
func asvabSubtestCode(htmlContent string) string {
	if m := asvabTitleCode.FindStringSubmatch(htmlContent); m != nil {
		return m[1]
	}
	return ""
}

// asvabAnswerKey scans the page's base64 runs for the ARI quiz config and
// returns per-question correctness, keyed by question id then answer id.
func asvabAnswerKey(htmlContent string) (map[string]map[string]int, error) {
	for _, b64 := range asvabB64.FindAllString(htmlContent, -1) {
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			continue
		}
		var cfg asvabConfig
		if err := json.Unmarshal(decoded, &cfg); err != nil {
			continue
		}
		out := map[string]map[string]int{}
		for _, p := range cfg.Pages {
			for qid, q := range p.Questions {
				m := make(map[string]int, len(q.Answers))
				for aid, a := range q.Answers {
					m[aid] = a.Correct
				}
				out[qid] = m
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, ErrASVABData.WithMessage("no decodable ARI quiz answer-key config found on page")
}

// asvabSpec builds one multiple-choice item spec. The item id folds in the
// subtest code so ids stay unique across subtests that reuse question numbers.
func asvabSpec(snap source.Snapshot, code, qid, stem string, tt shared.TestTypeCode,
	options []item.Option, keyID string) item.ItemSpec {
	return item.ItemSpec{
		ID: item.ItemID(fmt.Sprintf("%s-%s-q%s", snap.ID, strings.ToLower(code), qid)),
		Provenance: item.Provenance{
			SourceID:        string(snap.ID),
			Origin:          item.OriginFetched,
			Redistributable: snap.License.Redistributable,
		},
		TestType:     tt,
		Stimulus:     []item.StimulusPart{{Text: stem}},
		AnswerFormat: item.FormatMultipleChoice,
		Options:      options,
		AnswerKey:    item.AnswerKey{OptionID: keyID},
		Difficulty:   item.Difficulty{Band: asvabDefaultBand},
	}
}

// asvabText strips HTML tags, decodes entities and collapses whitespace (Fields
// splits on Unicode spaces, folding the &nbsp; the answer labels pad with).
func asvabText(s string) string {
	s = asvabTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}
