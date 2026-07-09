package ingest_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// asvabSnap builds a source snapshot matching the ASVAB catalogue entry shape.
func asvabSnap() source.Snapshot {
	return source.Snapshot{
		ID:         ingest.ASVABSourceID,
		License:    source.License{Redistributable: shared.RedistYes},
		Extraction: source.Extraction{Method: source.MethodScrapeHTML},
	}
}

// asvabRaw wraps page HTML the way scrapefetch inlines it.
func asvabRaw(pages ...string) []ports.RawItem {
	raw := make([]ports.RawItem, len(pages))
	for i, p := range pages {
		raw[i] = ports.RawItem{ExternalID: fmt.Sprintf("page-%d", i), Raw: map[string]any{"content": p}}
	}
	return raw
}

// asvabQuestionHTML renders one ARI-quiz question block. correctAID names the
// answer id flagged correct; labels maps answer id -> label HTML (may carry the
// <span>&nbsp;</span> padding the real AR page uses).
func asvabQuestionHTML(qid, title string, labels [][2]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="quiz-question" id="asq_x_question_%s" data-question-id="%s">`, qid, qid)
	fmt.Fprintf(&b, `<div class="quiz-question-title" data-question-index="1">%s</div>`, title)
	b.WriteString(`<div class="quiz-question-answers clearfix">`)
	for _, l := range labels {
		aid, text := l[0], l[1]
		fmt.Fprintf(&b, `<input type="radio" class="ari-checkbox quiz-question-answer-ctrl" `+
			`id="asq_x_answer_%s" value="%s" data-question-id="%s" />`, aid, aid, qid)
		fmt.Fprintf(&b, `<label class="ari-checkbox-label quiz-question-answer-ctrl-lbl" for="asq_x_answer_%s">%s</label>`, aid, text)
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

// asvabPageHTML wraps a subtest heading, question blocks and the base64 config.
// It pads a filler field into the config's top-level object so the encoded run
// clears the scanner's length floor while staying valid JSON (unknown fields
// are ignored on decode).
func asvabPageHTML(subtest, configJSON string, questions ...string) string {
	if i := strings.LastIndex(configJSON, "}"); i >= 0 {
		configJSON = configJSON[:i] + `,"pad":"` + strings.Repeat("x", 220) + `"}` + configJSON[i+1:]
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(configJSON))
	var b strings.Builder
	fmt.Fprintf(&b, `<html><body><h2 class="quiz-title">%s</h2>`, subtest)
	for _, q := range questions {
		b.WriteString(q)
	}
	fmt.Fprintf(&b, `<script>var cfg = "%s";</script></body></html>`, b64)
	return b.String()
}

// wkFixture is a Word-Knowledge page with two keyed questions.
func wkFixture() string {
	q9 := asvabQuestionHTML("9", `<u>Antagonize</u> most nearly means`, [][2]string{
		{"33", "embarrass."}, {"34", "struggle."}, {"35", "provoke."}, {"36", "worship."},
	})
	q10 := asvabQuestionHTML("10", `<u>Concise</u> most nearly means`, [][2]string{
		{"37", "brief."}, {"38", "sharp."}, {"39", "modern."}, {"40", "helpful."},
	})
	cfg := `{"quizId":3,"pages":[{"questions":{` +
		`"9":{"answers":{"33":{"correct":0},"34":{"correct":0},"35":{"correct":1},"36":{"correct":0}}},` +
		`"10":{"answers":{"37":{"correct":1},"38":{"correct":0},"39":{"correct":0},"40":{"correct":0}}}` +
		`}}]}`
	return asvabPageHTML("Word Knowledge (WK)", cfg, q9, q10)
}

func TestASVABNormalizerKeyedItems(t *testing.T) {
	specs, err := ingest.ASVABNormalizer(asvabSnap(), asvabRaw(wkFixture()))
	if err != nil {
		t.Fatalf("ASVABNormalizer: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("got %d specs, want 2", len(specs))
	}

	byID := map[item.ItemID]item.ItemSpec{}
	for _, s := range specs {
		byID[s.ID] = s
	}
	q9, ok := byID["asvab-official-wk-q9"]
	if !ok {
		t.Fatalf("missing item asvab-official-wk-q9; got %v", specsIDs(specs))
	}

	// Every spec must be a valid bank item (the service enforces this too).
	for _, s := range specs {
		if _, err := item.NewItem(s); err != nil {
			t.Errorf("spec %s rejected by item.NewItem: %v", s.ID, err)
		}
	}

	if q9.TestType != "C3" {
		t.Errorf("TestType = %q, want C3 (verbal synonym)", q9.TestType)
	}
	if got := q9.Stimulus[0].Text; got != "Antagonize most nearly means" {
		t.Errorf("stem = %q, want tags stripped", got)
	}
	if q9.AnswerFormat != item.FormatMultipleChoice {
		t.Errorf("format = %q, want multiple-choice", q9.AnswerFormat)
	}
	if len(q9.Options) != 4 {
		t.Fatalf("q9 has %d options, want 4", len(q9.Options))
	}
	// The key must reference answer 35 ("provoke."), the config's correct answer.
	if q9.AnswerKey.OptionID != "a35" {
		t.Errorf("key = %q, want a35", q9.AnswerKey.OptionID)
	}
	if q9.Provenance.Origin != item.OriginFetched || q9.Provenance.Redistributable != shared.RedistYes {
		t.Errorf("provenance = %+v, want fetched/yes", q9.Provenance)
	}
	if q9.Difficulty.Band != 3 {
		t.Errorf("band = %d, want 3 (default; ASVAB is unnormed)", q9.Difficulty.Band)
	}
}

func TestASVABNormalizerFoldsNbspLabels(t *testing.T) {
	// The AR page pads numeric answers with <span>&nbsp;</span>; the normalizer
	// must strip the markup and fold the non-breaking space to a clean value.
	q4 := asvabQuestionHTML("4", `A car travels 276 miles. How far in 2 hours?`, [][2]string{
		{"13", `<span style="display: inline-block;width: 21px">&nbsp;</span>276`},
		{"14", `<span style="width: 9px">&nbsp;</span>5,520`},
		{"15", `8,280`},
		{"16", `16,560`},
	})
	cfg := `{"pages":[{"questions":{"4":{"answers":` +
		`{"13":{"correct":0},"14":{"correct":1},"15":{"correct":0},"16":{"correct":0}}}}}]}`
	page := asvabPageHTML("Arithmetic Reasoning (AR)", cfg, q4)

	specs, err := ingest.ASVABNormalizer(asvabSnap(), asvabRaw(page))
	if err != nil {
		t.Fatalf("ASVABNormalizer: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	s := specs[0]
	if s.TestType != "B2" {
		t.Errorf("TestType = %q, want B2 (numerical calculation)", s.TestType)
	}
	if _, err := item.NewItem(s); err != nil {
		t.Fatalf("AR spec rejected: %v", err)
	}
	var got string
	for _, o := range s.Options {
		if o.ID == "a13" {
			got = o.Text
		}
	}
	if got != "276" {
		t.Errorf("option a13 text = %q, want %q (nbsp folded)", got, "276")
	}
	if s.AnswerKey.OptionID != "a14" {
		t.Errorf("key = %q, want a14", s.AnswerKey.OptionID)
	}
}

func TestASVABNormalizerSkipsUnmappedSubtest(t *testing.T) {
	// General Science is not a cognitive family: its page is skipped, not keyed.
	q1 := asvabQuestionHTML("1", `Water is made of hydrogen and what?`, [][2]string{
		{"1", "oxygen."}, {"2", "carbon."}, {"3", "helium."}, {"4", "iron."},
	})
	cfg := `{"pages":[{"questions":{"1":{"answers":` +
		`{"1":{"correct":1},"2":{"correct":0},"3":{"correct":0},"4":{"correct":0}}}}}]}`
	gs := asvabPageHTML("General Science (GS)", cfg, q1)

	// GS alone yields nothing keyable -> the "all rejected" style error.
	_, err := ingest.ASVABNormalizer(asvabSnap(), asvabRaw(gs))
	if !errors.Is(err, ingest.ErrASVABData) {
		t.Fatalf("err = %v, want ErrASVABData for a page with no supported subtest", err)
	}

	// GS mixed with WK: only the WK items come through.
	specs, err := ingest.ASVABNormalizer(asvabSnap(), asvabRaw(gs, wkFixture()))
	if err != nil {
		t.Fatalf("mixed ingest: %v", err)
	}
	if len(specs) != 2 {
		t.Errorf("got %d specs, want 2 (WK only; GS skipped)", len(specs))
	}
}

func TestASVABNormalizerMissingConfigIsError(t *testing.T) {
	// A page with quiz questions but no decodable config cannot be keyed.
	q9 := asvabQuestionHTML("9", `<u>Antagonize</u> most nearly means`, [][2]string{
		{"33", "embarrass."}, {"34", "struggle."}, {"35", "provoke."}, {"36", "worship."},
	})
	page := `<html><body><h2 class="quiz-title">Word Knowledge (WK)</h2>` + q9 + `</body></html>`

	_, err := ingest.ASVABNormalizer(asvabSnap(), asvabRaw(page))
	if !errors.Is(err, ingest.ErrASVABData) {
		t.Fatalf("err = %v, want ErrASVABData for missing config", err)
	}
}

func TestASVABNormalizerNoQuestions(t *testing.T) {
	// A page with no quiz markup (an index) contributes nothing and, alone,
	// surfaces the empty-result error.
	_, err := ingest.ASVABNormalizer(asvabSnap(), asvabRaw(`<html><body>nothing here</body></html>`))
	if !errors.Is(err, ingest.ErrASVABData) {
		t.Fatalf("err = %v, want ErrASVABData for empty page", err)
	}
}

func specsIDs(specs []item.ItemSpec) []item.ItemID {
	ids := make([]item.ItemID, len(specs))
	for i, s := range specs {
		ids[i] = s.ID
	}
	return ids
}

// asvabPageRawConfig wraps a page with the config base64-encoded verbatim (no
// padding), so a small single-question config exercises the real length floor.
func asvabPageRawConfig(subtest, configJSON string, questions ...string) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(configJSON))
	var b strings.Builder
	fmt.Fprintf(&b, `<html><body><h2 class="quiz-title">%s</h2>`, subtest)
	for _, q := range questions {
		b.WriteString(q)
	}
	fmt.Fprintf(&b, `<script>var cfg = "%s";</script></body></html>`, b64)
	return b.String()
}

func TestASVABNormalizerPartialTolerant(t *testing.T) {
	// A subtest page whose config the scraper cannot read must not sink the
	// pages that parsed cleanly — the good WK items still come through.
	q9 := asvabQuestionHTML("9", `<u>Antagonize</u> most nearly means`, [][2]string{
		{"33", "embarrass."}, {"34", "struggle."}, {"35", "provoke."}, {"36", "worship."},
	})
	broken := `<html><body><h2 class="quiz-title">Paragraph Comprehension (PC)</h2>` + q9 + `</body></html>`

	specs, err := ingest.ASVABNormalizer(asvabSnap(), asvabRaw(broken, wkFixture()))
	if err != nil {
		t.Fatalf("partial ingest errored: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("got %d specs, want 2 (WK survives the broken PC page); ids=%v", len(specs), specsIDs(specs))
	}
	for _, s := range specs {
		if !strings.Contains(string(s.ID), "-wk-") {
			t.Errorf("unexpected spec %s; only WK should survive", s.ID)
		}
	}
}

func TestASVABNormalizerSmallConfigNoPadding(t *testing.T) {
	// A realistic single-question config encodes well under the old 240-char
	// floor; the normalizer must still decode and key it.
	q9 := asvabQuestionHTML("9", `<u>Antagonize</u> most nearly means`, [][2]string{
		{"33", "embarrass."}, {"34", "struggle."}, {"35", "provoke."}, {"36", "worship."},
	})
	cfg := `{"pages":[{"questions":{"9":{"answers":` +
		`{"33":{"correct":0},"34":{"correct":0},"35":{"correct":1},"36":{"correct":0}}}}}]}`
	if len(base64.StdEncoding.EncodeToString([]byte(cfg))) >= 240 {
		t.Fatalf("fixture config is not small enough to prove the floor change")
	}
	page := asvabPageRawConfig("Word Knowledge (WK)", cfg, q9)

	specs, err := ingest.ASVABNormalizer(asvabSnap(), asvabRaw(page))
	if err != nil {
		t.Fatalf("small-config ingest errored: %v", err)
	}
	if len(specs) != 1 || specs[0].AnswerKey.OptionID != "a35" {
		t.Fatalf("got %d specs (key %v), want 1 keyed a35", len(specs), specsIDs(specs))
	}
}

func TestASVABNormalizerSkipsDuplicateOptionText(t *testing.T) {
	// Two options with identical text pass id-based uniqueness but are a broken
	// MC item; the question is skipped rather than emitted.
	dup := asvabQuestionHTML("9", `<u>Antagonize</u> most nearly means`, [][2]string{
		{"33", "provoke."}, {"34", "provoke."}, {"35", "worship."}, {"36", "struggle."},
	})
	good := asvabQuestionHTML("10", `<u>Concise</u> most nearly means`, [][2]string{
		{"37", "brief."}, {"38", "sharp."}, {"39", "modern."}, {"40", "helpful."},
	})
	cfg := `{"pages":[{"questions":{` +
		`"9":{"answers":{"33":{"correct":1},"34":{"correct":0},"35":{"correct":0},"36":{"correct":0}}},` +
		`"10":{"answers":{"37":{"correct":1},"38":{"correct":0},"39":{"correct":0},"40":{"correct":0}}}` +
		`}}]}`
	page := asvabPageHTML("Word Knowledge (WK)", cfg, dup, good)

	specs, err := ingest.ASVABNormalizer(asvabSnap(), asvabRaw(page))
	if err != nil {
		t.Fatalf("ASVABNormalizer: %v", err)
	}
	if len(specs) != 1 || specs[0].ID != "asvab-official-wk-q10" {
		t.Fatalf("got %v, want only the non-duplicate q10", specsIDs(specs))
	}
}
