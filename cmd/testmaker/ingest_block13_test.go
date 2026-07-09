package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/apifetch"
	"github.com/mariotoffia/testmaker/adapters/native/fetch/scrapefetch"
	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// wkFixturePage renders a minimal Word-Knowledge ARI-quiz page with one keyed
// question and the base64 answer-key config the normalizer decodes. It is padded
// so the config's base64 run clears the scanner's length floor.
func wkFixturePage() string {
	cfg := `{"quizId":1,"pages":[{"questions":{"9":{"answers":` +
		`{"33":{"correct":0},"34":{"correct":0},"35":{"correct":1},"36":{"correct":0}}}}}],` +
		`"pad":"` + strings.Repeat("x", 220) + `"}`
	b64 := base64.StdEncoding.EncodeToString([]byte(cfg))
	var b strings.Builder
	b.WriteString(`<html><body><h2 class="quiz-title">Word Knowledge (WK)</h2>`)
	b.WriteString(`<div class="quiz-question" data-question-id="9">`)
	b.WriteString(`<div class="quiz-question-title"><u>Antagonize</u> most nearly means</div>`)
	for _, a := range [][2]string{{"33", "embarrass."}, {"34", "struggle."}, {"35", "provoke."}, {"36", "worship."}} {
		fmt.Fprintf(&b, `<input class="ari-checkbox" id="asq_x_answer_%s" value="%s" data-question-id="9" />`, a[0], a[0])
		fmt.Fprintf(&b, `<label for="asq_x_answer_%s">%s</label>`, a[0], a[1])
	}
	b.WriteString(`</div>`)
	fmt.Fprintf(&b, `<script>var c="%s";</script></body></html>`, b64)
	return b.String()
}

// TestIngestASVABOffline proves the scrape → normalize → validate → store
// pipeline end-to-end against a canned ASVAB page served locally, with no
// network. It is the offline mirror of the Block 13 "done when".
func TestIngestASVABOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(wkFixturePage()))
	}))
	defer srv.Close()

	ctx := context.Background()
	snap := source.Snapshot{
		ID:         ingest.ASVABSourceID,
		License:    source.License{Redistributable: shared.RedistYes},
		URLs:       []string{srv.URL + "/word-knowledge-wk/"},
		Extraction: source.Extraction{Method: source.MethodScrapeHTML},
	}

	bank := memorytestdb.NewStore()
	svc := ingest.NewService(bank,
		scrapefetch.New(scrapefetch.WithHTTPClient(srv.Client())),
		stubfetcher.NewFetcher())
	svc.Register(ingest.ASVABSourceID, ingest.ASVABNormalizer)

	rep, err := svc.Ingest(ctx, snap, 0)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if rep.Saved != 1 {
		t.Fatalf("saved %d items, want 1", rep.Saved)
	}

	items, err := bank.ListItems(ctx, item.ItemFilter{Families: []shared.AbilityFamily{shared.FamilyVerbal}})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("stored %d verbal items, want 1", len(items))
	}
	got := items[0]
	if got.AnswerFormat != item.FormatMultipleChoice || got.AnswerKey.OptionID != "a35" {
		t.Errorf("item %s: format=%q key=%q, want multiple-choice/a35", got.ID, got.AnswerFormat, got.AnswerKey.OptionID)
	}
}

// TestFetchWikimediaOffline proves the api fetch → figure parse path end-to-end
// against a canned MediaWiki imageinfo response served locally.
func TestFetchWikimediaOffline(t *testing.T) {
	const body = `{"query":{"pages":{"77":{"pageid":77,"title":"File:Raven.svg",` +
		`"imageinfo":[{"url":"https://upload.wikimedia.org/x/Raven.svg","mime":"image/svg+xml",` +
		`"extmetadata":{"LicenseShortName":{"value":"Public domain"}}}]}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	snap := source.Snapshot{
		ID:         ingest.WikimediaSourceID,
		URLs:       []string{srv.URL + "/w/api.php?format=json"},
		Extraction: source.Extraction{Method: source.MethodAPI},
	}
	var api ports.Fetcher = apifetch.New(apifetch.WithHTTPClient(srv.Client()))
	res, err := api.Fetch(context.Background(), ports.FetchRequest{Source: snap})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	figures, err := ingest.WikimediaFigures(res.Items)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(figures) != 1 || figures[0].Title != "File:Raven.svg" || figures[0].License != "Public domain" {
		t.Fatalf("figures = %+v, want one Raven.svg (Public domain)", figures)
	}
}

// TestIngestASVABLive fetches and keys the real ASVAB sample subtests over the
// network. Env-gated (skips under -short and unless TESTMAKER_FETCH_LIVE=1) so
// `make test` stays green offline; it asserts shape, not generated content.
func TestIngestASVABLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live ingest under -short")
	}
	if os.Getenv("TESTMAKER_FETCH_LIVE") == "" {
		t.Skip("set TESTMAKER_FETCH_LIVE=1 to run the live ASVAB ingest")
	}
	ctx := context.Background()

	cat := catalog.NewService(memorycatalog.NewStore(), filecatalog.NewLoader("../../data/catalog/sources.json"))
	if _, err := cat.Sync(ctx); err != nil {
		t.Fatalf("sync catalogue: %v", err)
	}
	snap, err := cat.Get(ctx, ingest.ASVABSourceID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}

	bank := memorytestdb.NewStore()
	svc := ingest.NewService(bank, scrapefetch.New(), stubfetcher.NewFetcher())
	svc.Register(ingest.ASVABSourceID, ingest.ASVABNormalizer)

	rep, err := svc.Ingest(ctx, snap, 0)
	if err != nil {
		t.Fatalf("live ASVAB ingest failed: %v", err)
	}
	// Four keyed subtests, four questions each = 16 items on the site today. With
	// partial-tolerant ingest, a single drifted page would drop ~4 items — so
	// require most of them AND at least one item per mapped subtest, which catches
	// a 2-of-4 regression that a bare count threshold would miss.
	if rep.Saved < 12 {
		t.Fatalf("saved %d items, want >= 12 keyed ASVAB items", rep.Saved)
	}
	items, err := bank.ListItems(ctx, item.ItemFilter{})
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	seen := map[string]bool{}
	for _, it := range items {
		for _, code := range []string{"wk", "pc", "ar", "mk"} {
			if strings.Contains(string(it.ID), "-"+code+"-") {
				seen[code] = true
			}
		}
		if it.AnswerFormat == item.FormatMultipleChoice && it.AnswerKey.OptionID == "" {
			t.Errorf("item %s has no answer key", it.ID)
		}
	}
	for _, code := range []string{"wk", "pc", "ar", "mk"} {
		if !seen[code] {
			t.Errorf("no items for subtest %q — a subtest page likely drifted", code)
		}
	}
}

// TestFetchWikimediaLive fetches the real Wikimedia Commons imageinfo endpoint
// and parses figures. Env-gated like the ASVAB live test.
func TestFetchWikimediaLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live fetch under -short")
	}
	if os.Getenv("TESTMAKER_FETCH_LIVE") == "" {
		t.Skip("set TESTMAKER_FETCH_LIVE=1 to run the live Wikimedia fetch")
	}
	ctx := context.Background()

	cat := catalog.NewService(memorycatalog.NewStore(), filecatalog.NewLoader("../../data/catalog/sources.json"))
	if _, err := cat.Sync(ctx); err != nil {
		t.Fatalf("sync catalogue: %v", err)
	}
	snap, err := cat.Get(ctx, ingest.WikimediaSourceID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}

	var api ports.Fetcher = apifetch.New()
	res, err := api.Fetch(ctx, ports.FetchRequest{Source: snap, Limit: 10})
	if err != nil {
		t.Fatalf("live Wikimedia fetch failed: %v", err)
	}
	figures, err := ingest.WikimediaFigures(res.Items)
	if err != nil {
		t.Fatalf("parse figures: %v", err)
	}
	if len(figures) == 0 {
		t.Fatal("no figures parsed from live Wikimedia response")
	}
}
