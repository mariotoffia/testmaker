package filecatalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// Compile-time proof the loader satisfies the driving port.
var _ ports.CatalogLoader = (*filecatalog.Loader)(nil)

const sampleJSON = `{
  "meta": {"note": "test fixture"},
  "sources": [
    {
      "id": "omib", "name": "Open Matrices Item Bank", "provider": "Koch et al.",
      "urls": ["https://osf.io/4km79/"], "access_class": ["dataset-repo"], "formats": ["png","csv"],
      "license": {"category": "open-source", "detail": "GPLv3", "redistributable": "conditional"},
      "test_types": ["A2"], "item_count": "220", "answer_keys": "yes", "norms_difficulty": "yes",
      "languages": ["en"], "extraction": {"method": "api", "items_as": "images"},
      "generator": false, "priority": "high", "ip_risk": "low", "category": "open-data", "notes": ""
    },
    {
      "id": "indiabix", "name": "IndiaBIX", "provider": "IndiaBIX",
      "urls": ["https://www.indiabix.com/"], "access_class": ["site-scrape"], "formats": ["html"],
      "license": {"category": "commercial-paid", "detail": "(c) IndiaBIX", "redistributable": "no"},
      "test_types": ["A2","B3","C1"], "item_count": "thousands", "answer_keys": "yes", "norms_difficulty": "no",
      "languages": ["en"], "extraction": {"method": "scrape-html", "items_as": "mixed"},
      "generator": false, "priority": "high", "ip_risk": "medium", "category": "gov-standardized", "notes": ""
    }
  ]
}`

func TestLoadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.json")
	if err := os.WriteFile(path, []byte(sampleJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	snaps, err := filecatalog.NewLoader(path).Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("want 2 sources, got %d", len(snaps))
	}

	byID := map[source.SourceID]source.Snapshot{}
	for _, s := range snaps {
		byID[s.ID] = s
	}
	// families are derived from test types, not read from the file
	if got := len(byID["indiabix"].Families); got != 3 {
		t.Fatalf("indiabix families = %v, want 3 (logical/numerical/verbal)", byID["indiabix"].Families)
	}
	if byID["omib"].License.Redistributable != source.RedistConditional {
		t.Fatalf("omib redistributable = %q", byID["omib"].License.Redistributable)
	}
}

func TestParseJSONValidatesRecords(t *testing.T) {
	good := []byte(`{"sources":[{"id":"s1","name":"S1","urls":["https://x"],"access_class":["dataset-repo"],` +
		`"license":{"category":"public-domain","redistributable":"yes"},"test_types":["A2"],` +
		`"answer_keys":"yes","norms_difficulty":"no","priority":"high","ip_risk":"low","category":"open-data"}]}`)
	snaps, err := filecatalog.ParseJSON(good)
	if err != nil {
		t.Fatalf("ParseJSON(good): %v", err)
	}
	if len(snaps) != 1 || snaps[0].ID != "s1" {
		t.Fatalf("snaps = %+v", snaps)
	}
	// An invalid record (empty id) is rejected — the same source.NewSource gate Load applies.
	bad := []byte(`{"sources":[{"name":"no id"}]}`)
	if _, err := filecatalog.ParseJSON(bad); err == nil {
		t.Fatal("ParseJSON must reject an invalid record")
	}
	// Malformed JSON is an error, not a panic or empty success.
	if _, err := filecatalog.ParseJSON([]byte("{not json")); err == nil {
		t.Fatal("ParseJSON must reject malformed JSON")
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	bad := `{"sources":[{"id":"x","name":"X","urls":["https://x"],"access_class":["dataset-repo"],` +
		`"license":{"category":"nope","redistributable":"no"},"test_types":["A2"],` +
		`"answer_keys":"yes","norms_difficulty":"no","priority":"high","ip_risk":"low","category":"open-data"}]}`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := filecatalog.NewLoader(path).Load(context.Background()); err == nil {
		t.Fatal("expected error for invalid license category")
	}
}
