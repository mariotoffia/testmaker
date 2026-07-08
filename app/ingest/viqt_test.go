package ingest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// viqtSnap is the source snapshot the VIQT normalizer maps.
func viqtSnap() source.Snapshot {
	return source.Snapshot{
		ID:      ingest.VIQTSourceID,
		License: source.License{Redistributable: source.RedistYes},
	}
}

// viqtRaw wraps a codebook and csv as the two RawItems the fetcher would return.
func viqtRaw(codebook, csv string) []ports.RawItem {
	return []ports.RawItem{
		{ExternalID: "VIQT_data/codebook.txt", Raw: map[string]any{"content": codebook}},
		{ExternalID: "VIQT_data/VIQT_data.csv", Raw: map[string]any{"content": csv}},
	}
}

// A three-question codebook (plus noise lines the parser must ignore) and a
// response CSV whose Q columns are not first, to exercise header-based lookup.
const (
	viqtCodebook = "This data was collected on-line.\n" +
		"item\tans\twords\n" +
		"Q1\t24\ttiny faded new large big\n" +
		"Q2\t3\talpha beta gamma delta epsilon\n" +
		"Q3\t9\tone two three four five\n" +
		"\n" +
		"3 = 11000\n"

	viqtCSV = "id\tQ1\tQ2\tQ3\n" +
		"u1\t24\t3\t17\n" +
		"u2\t24\t3\t17\n" +
		"u3\t24\t17\t20\n" +
		"u4\t24\t20\t20\n" +
		"u5\t24\t6\t6\n"
)

func findSpec(specs []item.ItemSpec, id item.ItemID) (item.ItemSpec, bool) {
	for _, s := range specs {
		if s.ID == id {
			return s, true
		}
	}
	return item.ItemSpec{}, false
}

func optionText(spec item.ItemSpec, id string) string {
	for _, o := range spec.Options {
		if o.ID == id {
			return o.Text
		}
	}
	return ""
}

func TestVIQTNormalizer(t *testing.T) {
	specs, err := ingest.VIQTNormalizer(viqtSnap(), viqtRaw(viqtCodebook, viqtCSV))
	if err != nil {
		t.Fatalf("VIQTNormalizer: %v", err)
	}
	if len(specs) != 3 {
		t.Fatalf("got %d specs, want 3", len(specs))
	}

	// Every produced spec must be a valid bank item.
	for _, s := range specs {
		if _, verr := item.NewItem(s); verr != nil {
			t.Errorf("spec %s failed NewItem: %v", s.ID, verr)
		}
	}

	// Q1: words "tiny faded new large big", code 24 -> synonyms large(3)+big(4).
	q1, ok := findSpec(specs, "openpsych-viqt-q1")
	if !ok {
		t.Fatalf("missing q1 spec; got %v", specs)
	}
	if q1.AnswerFormat != item.FormatMultipleChoice {
		t.Errorf("q1 format = %q", q1.AnswerFormat)
	}
	if q1.TestType != "C3" {
		t.Errorf("q1 test type = %q, want C3", q1.TestType)
	}
	if len(q1.Options) != 4 {
		t.Fatalf("q1 has %d options, want 4", len(q1.Options))
	}
	if q1.AnswerKey.OptionID != "o4" {
		t.Errorf("q1 key = %q, want o4", q1.AnswerKey.OptionID)
	}
	if optionText(q1, "o4") != "big" {
		t.Errorf("q1 key option text = %q, want big", optionText(q1, "o4"))
	}
	if got := optionText(q1, "o0"); got != "tiny" { // stem (large, idx3) excluded
		t.Errorf("q1 o0 = %q, want tiny", got)
	}
	if strings.Contains(q1.Stimulus[0].Text, "big") {
		t.Errorf("q1 stem must ask about the first synonym, not reveal the key: %q", q1.Stimulus[0].Text)
	}
	if !strings.Contains(q1.Stimulus[0].Text, "large") {
		t.Errorf("q1 stem = %q, want it to mention large", q1.Stimulus[0].Text)
	}
	// p = 5/5 = 1.0 -> easiest band.
	if q1.Difficulty.Band != 1 {
		t.Errorf("q1 band = %d, want 1", q1.Difficulty.Band)
	}
	if q1.Provenance.SourceID != "openpsych-viqt" ||
		q1.Provenance.Origin != item.OriginFetched ||
		q1.Provenance.Redistributable != source.RedistYes {
		t.Errorf("q1 provenance = %+v", q1.Provenance)
	}

	// Q2: code 3 -> synonyms alpha(0)+beta(1); p = 2/5 = 0.4 -> band 3.
	q2, _ := findSpec(specs, "openpsych-viqt-q2")
	if q2.AnswerKey.OptionID != "o1" || optionText(q2, "o1") != "beta" {
		t.Errorf("q2 key = %q (%q), want o1 (beta)", q2.AnswerKey.OptionID, optionText(q2, "o1"))
	}
	if q2.Difficulty.Band != 3 {
		t.Errorf("q2 band = %d, want 3 (p=0.4)", q2.Difficulty.Band)
	}

	// Q3: code 9 -> synonyms one(0)+four(3); p = 0/5 = 0.0 -> hardest band.
	q3, _ := findSpec(specs, "openpsych-viqt-q3")
	if q3.AnswerKey.OptionID != "o3" || optionText(q3, "o3") != "four" {
		t.Errorf("q3 key = %q (%q), want o3 (four)", q3.AnswerKey.OptionID, optionText(q3, "o3"))
	}
	if q3.Difficulty.Band != 5 {
		t.Errorf("q3 band = %d, want 5 (p=0.0)", q3.Difficulty.Band)
	}
}

func TestVIQTNormalizerMissingArtifacts(t *testing.T) {
	// No codebook.
	if _, err := ingest.VIQTNormalizer(viqtSnap(), []ports.RawItem{
		{ExternalID: "VIQT_data/VIQT_data.csv", Raw: map[string]any{"content": viqtCSV}},
	}); !errors.Is(err, ingest.ErrVIQTData) {
		t.Errorf("missing codebook: err = %v, want ErrVIQTData", err)
	}
	// No csv.
	if _, err := ingest.VIQTNormalizer(viqtSnap(), []ports.RawItem{
		{ExternalID: "VIQT_data/codebook.txt", Raw: map[string]any{"content": viqtCodebook}},
	}); !errors.Is(err, ingest.ErrVIQTData) {
		t.Errorf("missing csv: err = %v, want ErrVIQTData", err)
	}
}

func TestVIQTNormalizerSkipsItemsWithoutResponses(t *testing.T) {
	// Codebook has Q1 and Q2, but the CSV only carries Q1: Q2 has no column and
	// is skipped (no honest difficulty), Q1 survives.
	cb := "Q1\t24\ttiny faded new large big\nQ2\t3\talpha beta gamma delta epsilon\n"
	csv := "Q1\n24\n24\n"
	specs, err := ingest.VIQTNormalizer(viqtSnap(), viqtRaw(cb, csv))
	if err != nil {
		t.Fatalf("VIQTNormalizer: %v", err)
	}
	if len(specs) != 1 || specs[0].ID != "openpsych-viqt-q1" {
		t.Fatalf("got %v, want only q1", specs)
	}
}

func TestVIQTNormalizerNoUsableResponses(t *testing.T) {
	// Valid codebook, CSV header matches Q1, but every data row is unparseable:
	// no item earns an honest difficulty, so the whole run is malformed, not an
	// empty success.
	cb := "Q1\t24\ttiny faded new large big\n"
	csv := "Q1\nx\n-\n"
	if _, err := ingest.VIQTNormalizer(viqtSnap(), viqtRaw(cb, csv)); !errors.Is(err, ingest.ErrVIQTData) {
		t.Errorf("no usable responses: err = %v, want ErrVIQTData", err)
	}
}
