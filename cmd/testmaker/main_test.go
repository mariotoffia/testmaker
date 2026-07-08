package main

import (
	"context"
	"os"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/httpfetch"
	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
)

// TestIngestVIQTLive proves the fetch → normalize → validate → store pipeline
// against the real OpenPsychometrics VIQT dataset over the network. It is
// env-gated (skips under -short and unless TESTMAKER_FETCH_LIVE is set) so
// `make test` stays green offline. It asserts shape, never generated content:
// a real dataset's item texts are not fixed here.
func TestIngestVIQTLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live ingest under -short")
	}
	if os.Getenv("TESTMAKER_FETCH_LIVE") == "" {
		t.Skip("set TESTMAKER_FETCH_LIVE=1 to run the live VIQT ingest (downloads a few MiB)")
	}

	ctx := context.Background()

	// Load the real catalogue so the source's refined direct-download entry is
	// exercised end-to-end.
	cat := catalog.NewService(memorycatalog.NewStore(), filecatalog.NewLoader("../../data/catalog/sources.json"))
	if _, err := cat.Sync(ctx); err != nil {
		t.Fatalf("sync catalogue: %v", err)
	}
	snap, err := cat.Get(ctx, ingest.VIQTSourceID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}

	bank := memorytestdb.NewStore()
	svc := ingest.NewService(bank, httpfetch.New())
	svc.Register(ingest.VIQTSourceID, ingest.VIQTNormalizer)

	rep, err := svc.Ingest(ctx, snap, 0)
	if err != nil {
		// The env gate is an explicit opt-in: a real failure here (moved URL,
		// malformed dataset) must be red, not silently skipped.
		t.Fatalf("live VIQT ingest failed: %v", err)
	}

	// Shape assertions only.
	if rep.Fetched < 2 {
		t.Errorf("fetched %d artifacts, want >= 2 (codebook + csv)", rep.Fetched)
	}
	if rep.Saved < 1 {
		t.Fatalf("saved %d items, want >= 1", rep.Saved)
	}

	items, err := bank.ListItems(ctx, item.ItemFilter{Families: []shared.AbilityFamily{shared.FamilyVerbal}})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("no verbal items stored")
	}
	got := items[0]
	if got.AnswerFormat != item.FormatMultipleChoice {
		t.Errorf("format = %q, want multiple-choice", got.AnswerFormat)
	}
	if len(got.Options) < 4 {
		t.Errorf("item %s has %d options, want >= 4", got.ID, len(got.Options))
	}
	if got.AnswerKey.OptionID == "" {
		t.Errorf("item %s has no answer key", got.ID)
	}
	keyed := false
	for _, o := range got.Options {
		if o.ID == got.AnswerKey.OptionID {
			keyed = true
		}
	}
	if !keyed {
		t.Errorf("item %s key %q does not reference an option", got.ID, got.AnswerKey.OptionID)
	}
	if got.Difficulty.Band < 1 {
		t.Errorf("item %s difficulty band = %d, want >= 1", got.ID, got.Difficulty.Band)
	}
}
