// Command testmaker is a thin composition root demonstrating the source
// catalogue vertical slice: it wires the file loader, the in-memory repository
// and the stub fetcher into the catalogue application service, loads the
// research catalogue and reports on it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/httpfetch"
	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/adapters/native/generate/rulegen"
	"github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat"
	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/sqlitetestdb"
	"github.com/mariotoffia/testmaker/app/authoring"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

func main() {
	path := flag.String("catalog", "data/catalog/sources.json", "path to the source catalogue (json or yaml)")
	testdbDSN := flag.String("testdb", "memory", `TestDb backend: "memory" or a sqlite DSN (a file path or ":memory:")`)
	llmPrompt := flag.String("llm-prompt", "", "if set (and TESTMAKER_LLM_BASE_URL is configured), send this prompt to the LLM backend")
	ingestID := flag.String("ingest", "", "if set to a catalogue source id (e.g. openpsych-viqt), fetch and ingest its items into the bank")
	genType := flag.String("generate", "", "if set to a figural test type (A1, A2, A3 or A4), procedurally generate a small batch of items into the bank")
	authorTest := flag.Bool("author-test", false, "compose a composite, timed, difficulty-ordered test from the bank and store+reload it")
	flag.Parse()

	if err := run(context.Background(), *path, *testdbDSN, *llmPrompt, *ingestID, *genType, *authorTest); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, path, testdbDSN, llmPrompt, ingestID, genType string, authorTest bool) (err error) {
	// --- composition root: choose adapters, wire the service ---
	// One concrete TestDb store backs all three repositories; it is exposed here
	// as the separate ports the app depends on. The default is the dependency-free
	// in-memory store.
	memStore := memorytestdb.NewStore()
	var (
		repo     ports.SourceRepository = memorycatalog.NewStore()
		loader   ports.CatalogLoader    = filecatalog.NewLoader(path)
		fetcher  ports.Fetcher          = stubfetcher.NewFetcher()
		testdb   ports.TestRepository   = memStore
		itembank ports.ItemRepository   = memStore
	)
	// A sqlite DSN swaps in the durable adapter. Its *Store satisfies every
	// TestDb port, so nothing below changes — the only place that knows the
	// concrete backend is right here.
	closeTestDB := func() error { return nil }
	if testdbDSN != "" && testdbDSN != "memory" {
		store, oerr := sqlitetestdb.Open(testdbDSN)
		if oerr != nil {
			return oerr
		}
		testdb, itembank, closeTestDB = store, store, store.Close
	}
	// Surface a close failure (a file-backed store may have unflushed writes)
	// alongside the real error rather than instead of it.
	defer func() {
		if cerr := closeTestDB(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	svc := catalog.NewService(repo, loader)

	n, err := svc.Sync(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Synced %d sources from %s\n\n", n, path)

	all, err := svc.List(ctx, source.SourceFilter{})
	if err != nil {
		return err
	}
	printByCategory(all)

	if err := reportReusability(ctx, svc, fetcher); err != nil {
		return err
	}

	if err := testDbDemo(ctx, testdb); err != nil {
		return err
	}
	if err := itemBankDemo(ctx, itembank); err != nil {
		return err
	}
	if err := ingestDemo(ctx, svc, itembank, ingestID); err != nil {
		return err
	}
	if err := generateDemo(ctx, itembank, genType); err != nil {
		return err
	}
	if err := authorTestDemo(ctx, itembank, testdb, authorTest); err != nil {
		return err
	}
	return llmDemo(ctx, llmPrompt)
}

// reportReusability prints the reuse/generator breakdown of the catalogue and
// exercises the fetch boundary (stub) against one generator source.
func reportReusability(ctx context.Context, svc *catalog.Service, fetcher ports.Fetcher) error {
	reusable, err := svc.Reusable(ctx)
	if err != nil {
		return err
	}
	cond, err := svc.Conditional(ctx)
	if err != nil {
		return err
	}
	gens, err := svc.Generators(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("\nReusable: %d\nConditional (license terms apply): %d\nGenerators: %d\n",
		len(reusable), len(cond), len(gens))

	if len(gens) > 0 && fetcher.Supports(gens[0]) {
		res, ferr := fetcher.Fetch(ctx, ports.FetchRequest{Source: gens[0], Limit: 5})
		if ferr != nil {
			return ferr
		}
		fmt.Printf("\nFetch demo (%s): %s\n", res.SourceID, res.Note)
	}
	return nil
}

// testDbDemo exercises the in-memory TestDb (the default ports.TestRepository)
// with a raw save/reload round-trip, proving the store is wired at the
// composition root. Full test authoring (composing bank items into a timed,
// difficulty-ordered test) is the -author-test demo below.
func testDbDemo(ctx context.Context, testdb ports.TestRepository) error {
	if err := testdb.SaveTest(ctx, testset.TestSnapshot{ID: "demo", Title: "Demo Test"}); err != nil {
		return err
	}
	got, err := testdb.GetTest(ctx, "demo")
	if err != nil {
		return err
	}
	fmt.Printf("\nTestDb demo: stored and reloaded %q (%s)\n", got.Title, got.ID)
	return nil
}

// authorTestDemo is the Block 7 "done when": it composes a composite, timed,
// difficulty-ordered test out of bank items and stores+reloads it. With no
// -author-test flag the step is skipped. It seeds a small logical+numerical item
// set (distinct ids), wires the TestService over the same item bank and test
// repository the rest of the CLI uses, composes a two-section fixed-increasing
// test and reloads it through the store to prove the round-trip.
func authorTestDemo(ctx context.Context, bank ports.ItemRepository, tests ports.TestRepository, authorTest bool) error {
	if !authorTest {
		fmt.Println("\nAuthor test: not requested (pass -author-test); skipping.")
		return nil
	}
	if err := seedAuthoringBank(ctx, bank); err != nil {
		return err
	}

	svc := authoring.NewTestService(bank, tests)
	id, err := svc.Compose(ctx, authoring.ComposeSpec{
		ID:     "demo-composite",
		Title:  "Composite Aptitude (demo)",
		Policy: testset.PolicyFixedIncreasing,
		Timing: testset.Timing{Total: 30 * time.Minute},
		Sections: []authoring.SectionSpec{
			{Title: "Logical", Family: shared.FamilyLogical, Timing: testset.Timing{Total: 10 * time.Minute, PerItem: time.Minute}},
			{Title: "Numerical", Family: shared.FamilyNumerical, Timing: testset.Timing{Total: 8 * time.Minute}},
		},
	})
	if err != nil {
		return err
	}

	got, err := tests.GetTest(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("\nAuthor-test demo: composed and reloaded %q (%s), policy=%s, families=%v\n",
		got.Title, got.ID, got.Policy, got.Families)
	for _, sec := range got.Sections {
		ids := make([]string, len(sec.Items))
		for i, ref := range sec.Items {
			ids[i] = fmt.Sprintf("%s(b%d)", ref.ItemID, ref.Difficulty)
		}
		fmt.Printf("  %-10s %v\n", sec.Family, ids)
	}
	return nil
}

// seedAuthoringBank stores a small composite item set (logical + numerical,
// bands deliberately unsorted) so the author-test demo has something to compose.
func seedAuthoringBank(ctx context.Context, bank ports.ItemRepository) error {
	seeds := []struct {
		id       item.ItemID
		testType shared.TestTypeCode
		band     int
	}{
		{"demo-log-hard", "A1", 3},
		{"demo-log-easy", "A2", 1},
		{"demo-log-mid", "A3", 2},
		{"demo-num-hard", "B1", 2},
		{"demo-num-easy", "B2", 1},
	}
	for _, s := range seeds {
		it, ierr := item.NewItem(item.ItemSpec{
			ID:           s.id,
			Provenance:   item.Provenance{SourceID: "rulegen", Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
			TestType:     s.testType,
			Stimulus:     []item.StimulusPart{{Text: "stem"}},
			AnswerFormat: item.FormatMultipleChoice,
			Options: []item.Option{
				{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
			},
			AnswerKey:  item.AnswerKey{OptionID: "b"},
			Difficulty: item.Difficulty{Band: s.band},
		})
		if ierr != nil {
			return ierr
		}
		if err := bank.SaveItem(ctx, it.Snapshot()); err != nil {
			return err
		}
	}
	return nil
}

// itemBankDemo exercises the ItemRepository: it builds a validated multiple-choice
// item through the aggregate, stores its snapshot, and queries the bank by
// ability family, test type and difficulty band — the Block 4 "done when".
func itemBankDemo(ctx context.Context, bank ports.ItemRepository) error {
	it, ierr := item.NewItem(item.ItemSpec{
		ID:           "omib-1",
		Provenance:   item.Provenance{SourceID: "omib", Origin: item.OriginFetched, Redistributable: shared.RedistConditional},
		TestType:     "A2",
		Stimulus:     []item.StimulusPart{{Text: "which figure continues the series?"}, {MediaKind: item.MediaGrid, MediaRef: "blob://omib-1"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:   item.AnswerKey{OptionID: "c"},
		Explanation: "each step rotates the figure 90 degrees",
		Difficulty:  item.Difficulty{Band: 3},
	})
	if ierr != nil {
		return ierr
	}
	snap := it.Snapshot()
	if err := bank.SaveItem(ctx, snap); err != nil {
		return err
	}
	matches, err := bank.ListItems(ctx, item.ItemFilter{
		Families:      []shared.AbilityFamily{shared.FamilyLogical},
		TestTypes:     []shared.TestTypeCode{"A2"},
		MinDifficulty: 2,
		MaxDifficulty: 5,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Item bank demo: stored %q (%s, family=%s); query by family/type/difficulty matched %d item(s)\n",
		snap.ID, snap.AnswerFormat, snap.Family, len(matches))
	return nil
}

// ingestDemo wires the real fetch → normalize → validate → store pipeline. With
// no -ingest flag the step is skipped. When a source id is given, it looks the
// source up in the catalogue, fetches its artifacts through the httpfetch
// adapter (falling back to the stub for unsupported methods), normalizes them
// into validated bank items and reports the per-stage counts. The composition
// root is the only place that knows the concrete fetchers and per-source
// normalizers.
func ingestDemo(ctx context.Context, cat *catalog.Service, bank ports.ItemRepository, sourceID string) error {
	if sourceID == "" {
		fmt.Println("\nIngest: not requested (pass -ingest <source-id>, e.g. openpsych-viqt); skipping.")
		return nil
	}

	snap, err := cat.Get(ctx, source.SourceID(sourceID))
	if err != nil {
		return err
	}

	// Inject through the port type (like the other adapters at the composition
	// root) so the wiring is an app→ports dependency, not adapter→app.
	var (
		downloader ports.Fetcher = httpfetch.New()
		stub       ports.Fetcher = stubfetcher.NewFetcher()
	)
	svc := ingest.NewService(bank, downloader, stub)
	svc.Register(ingest.VIQTSourceID, ingest.VIQTNormalizer)

	rep, err := svc.Ingest(ctx, snap, 0)
	if err != nil {
		return err
	}
	fmt.Printf("\nIngest demo (%s): fetched %d artifact(s), normalized %d, saved %d, skipped %d — %s\n",
		rep.SourceID, rep.Fetched, rep.Normalized, rep.Saved, rep.Skipped, rep.Note)
	return nil
}

// generateDemo wires the procedural generator (rulegen) through the authoring
// use-case: with no -generate flag the step is skipped. When a figural test type
// is given, it generates a small deterministic batch and stores it in the bank,
// reporting the per-run counts. The composition root is the only place that
// knows the concrete generator.
func generateDemo(ctx context.Context, bank ports.ItemRepository, genType string) error {
	if genType == "" {
		fmt.Println("\nGenerate: not requested (pass -generate <A1|A2|A3|A4>); skipping.")
		return nil
	}

	var gen ports.Generator = rulegen.New()
	svc := authoring.NewService(gen, bank)

	rep, err := svc.Generate(ctx, ports.GenerateSpec{
		TestType:   shared.TestTypeCode(genType),
		Difficulty: 2,
		Count:      3,
		Seed:       1,
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nGenerate demo (%s): generated %d item(s), saved %d into the bank\n",
		rep.TestType, rep.Generated, rep.Saved)
	return nil
}

// llmDemo wires the OpenAI-compatible LLM adapter behind config: with no
// TESTMAKER_LLM_BASE_URL the step is skipped and the CLI still runs. When
// configured, the adapter is used through ports.LLM — the composition root is
// the only place that knows the concrete backend.
func llmDemo(ctx context.Context, userPrompt string) error {
	baseURL := os.Getenv("TESTMAKER_LLM_BASE_URL")
	if baseURL == "" {
		fmt.Println("\nLLM: not configured (set TESTMAKER_LLM_BASE_URL to enable); skipping.")
		return nil
	}

	client, err := openaicompat.New(openaicompat.Config{
		BaseURL:    baseURL,
		APIKey:     os.Getenv("TESTMAKER_LLM_API_KEY"),
		AuthScheme: openaicompat.AuthScheme(os.Getenv("TESTMAKER_LLM_AUTH_SCHEME")),
	})
	if err != nil {
		return err
	}
	if userPrompt == "" {
		fmt.Println("\nLLM: configured; pass -llm-prompt to run a completion.")
		return nil
	}

	var backend ports.LLM = client
	resp, err := backend.Generate(ctx, ports.LLMRequest{
		Model:    os.Getenv("TESTMAKER_LLM_MODEL"),
		Messages: []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: userPrompt}},
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nLLM (%s): %s\n", resp.Model, resp.Content)
	return nil
}

func printByCategory(snaps []source.Snapshot) {
	counts := map[source.Category]int{}
	for _, s := range snaps {
		counts[s.Category]++
	}
	cats := make([]string, 0, len(counts))
	for c := range counts {
		cats = append(cats, string(c))
	}
	sort.Strings(cats)

	fmt.Println("By category:")
	for _, c := range cats {
		fmt.Printf("  %-20s %d\n", c, counts[source.Category(c)])
	}
}
