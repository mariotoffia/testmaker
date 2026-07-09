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
	"strings"
	"time"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/httpfetch"
	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/adapters/native/generate/rulegen"
	"github.com/mariotoffia/testmaker/adapters/native/llm/fileprompts"
	"github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat"
	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/app/authoring"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/app/execution"
	"github.com/mariotoffia/testmaker/app/ingest"
	llmapp "github.com/mariotoffia/testmaker/app/llm"
	scoringapp "github.com/mariotoffia/testmaker/app/scoring"
	"github.com/mariotoffia/testmaker/domain/clock"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/scoring"
	"github.com/mariotoffia/testmaker/domain/session"
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
	ingestLLMID := flag.String("ingest-llm", "", "if set to a catalogue source id (and TESTMAKER_LLM_BASE_URL is configured), fetch its payload and lift items with the LLM extraction step")
	promptsDir := flag.String("prompts", "data/prompts", "directory of the file-backed prompt store (one YAML per prompt)")
	genType := flag.String("generate", "", "if set to a figural test type (A1, A2, A3 or A4), procedurally generate a small batch of items into the bank")
	authorTest := flag.Bool("author-test", false, "compose a composite, timed, difficulty-ordered test from the bank and store+reload it")
	runTest := flag.Bool("run-test", false, "administer a composed test end-to-end (fixed + adaptive) under timing, grading answers and reporting the score")
	serve := flag.String("serve", "", "if set to a listen address (e.g. :8080), run the HTTP delivery API (author/take/score) instead of the demo")
	blobsSpec := flag.String("blobs", "memory", `blob store backend for figural media: "memory" or a directory path (filesystem)`)
	flag.Parse()

	if *serve != "" {
		if err := runServer(*serve, *testdbDSN, *blobsSpec); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background(), runConfig{
		path: *path, testdbDSN: *testdbDSN, blobsSpec: *blobsSpec, llmPrompt: *llmPrompt,
		promptsDir: *promptsDir, ingestID: *ingestID, ingestLLMID: *ingestLLMID,
		genType: *genType, authorTest: *authorTest, runTest: *runTest,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// runConfig carries the parsed flags into run so the composition root's signature
// stays readable as the demo surface grows.
type runConfig struct {
	path, testdbDSN, blobsSpec, llmPrompt, promptsDir, ingestID, ingestLLMID, genType string
	authorTest, runTest                                                               bool
}

func run(ctx context.Context, cfg runConfig) (err error) {
	path := cfg.path
	// --- composition root: choose adapters, wire the service ---
	// One concrete TestDb store backs all three repositories (memory by default,
	// sqlite behind a DSN); openTestDB is the only place that knows the backend.
	var (
		repo    ports.SourceRepository = memorycatalog.NewStore()
		loader  ports.CatalogLoader    = filecatalog.NewLoader(path)
		fetcher ports.Fetcher          = stubfetcher.NewFetcher()
	)
	db, err := openTestDB(cfg.testdbDSN)
	if err != nil {
		return err
	}
	testdb, itembank, sessions := db.tests, db.items, db.sessions
	// Surface a close failure (a file-backed store may have unflushed writes)
	// alongside the real error rather than instead of it.
	defer func() {
		if cerr := db.close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	blobs, err := openBlobStore(cfg.blobsSpec)
	if err != nil {
		return err
	}

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
	if err := ingestDemo(ctx, svc, itembank, cfg.ingestID); err != nil {
		return err
	}
	if err := ingestLLMDemo(ctx, svc, itembank, cfg.promptsDir, cfg.ingestLLMID); err != nil {
		return err
	}
	if err := generateDemo(ctx, itembank, blobs, cfg.genType); err != nil {
		return err
	}
	if err := authorTestDemo(ctx, itembank, testdb, cfg.authorTest); err != nil {
		return err
	}
	if err := runTestDemo(ctx, itembank, testdb, sessions, cfg.runTest); err != nil {
		return err
	}
	return llmDemo(ctx, cfg.llmPrompt)
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

// runTestDemo is the Block 8 "done when": it administers a composed test
// end-to-end under timing. It reuses the authoring bank, composes a fixed and an
// adaptive test, then drives each attempt through the execution service (real
// clock, grading against each item's answer key) to a completed, scored session.
// With no -run-test flag the step is skipped. The execution service is stateless
// and reads/writes attempt state only through the session repository wired here.
func runTestDemo(
	ctx context.Context,
	bank ports.ItemRepository, tests ports.TestRepository, sessions ports.SessionRepository,
	runTest bool,
) error {
	if !runTest {
		fmt.Println("\nRun test: not requested (pass -run-test); skipping.")
		return nil
	}
	if err := seedAuthoringBank(ctx, bank); err != nil {
		return err
	}
	author := authoring.NewTestService(bank, tests)
	exec := execution.NewService(clock.System(), bank, sessions, execution.RandomIDs())
	// Demo norms: the composition root carries a deployment's norm book (test id →
	// normal norm of the scored dimension). The fixed test norms the raw count;
	// the adaptive test norms the staircase ability (bands 1..3), so its IQ
	// reflects the path taken. ponytail: illustrative mean/SD, not published norms.
	scorer := scoringapp.NewService(bank, scoring.NormBook{
		"run-fixed":    {Mean: 2, SD: 1.5},
		"run-adaptive": {Mean: 2, SD: 1},
	})

	fixedID, err := author.Compose(ctx, authoring.ComposeSpec{
		ID:     "run-fixed",
		Title:  "Run Demo (fixed)",
		Policy: testset.PolicyFixedIncreasing,
		Timing: testset.Timing{Total: 15 * time.Minute, PerItem: time.Minute},
		Sections: []authoring.SectionSpec{
			{Title: "Logical", Family: shared.FamilyLogical, Timing: testset.Timing{Total: 8 * time.Minute, PerItem: time.Minute}},
			{Title: "Numerical", Family: shared.FamilyNumerical, Timing: testset.Timing{Total: 5 * time.Minute}},
		},
	})
	if err != nil {
		return err
	}
	fixedTest, err := tests.GetTest(ctx, fixedID)
	if err != nil {
		return err
	}
	if err := driveAttempt(ctx, exec, scorer, fixedTest, "fixed"); err != nil {
		return err
	}

	// An adaptive logical pool (bands 1..3) shows the executor climbing and
	// descending the staircase as answers are graded.
	adaptID, err := author.Compose(ctx, authoring.ComposeSpec{
		ID:       "run-adaptive",
		Title:    "Run Demo (adaptive)",
		Policy:   testset.PolicyAdaptive,
		Sections: []authoring.SectionSpec{{Title: "Logical", Family: shared.FamilyLogical}},
	})
	if err != nil {
		return err
	}
	adaptTest, err := tests.GetTest(ctx, adaptID)
	if err != nil {
		return err
	}
	return driveAttempt(ctx, exec, scorer, adaptTest, "adaptive")
}

// driveAttempt starts a session for the test, answers each presented item and
// completes it, then scores the completed session and prints the delivery path
// and score. It alternates correct/incorrect answers so an adaptive attempt
// exercises both directions of the staircase; the seeded items all key option
// "b", so "a" is a deterministic wrong answer.
func driveAttempt(ctx context.Context, exec *execution.Service, scorer ports.Scorer, test testset.TestSnapshot, label string) error {
	d, err := exec.Start(ctx, test)
	if err != nil {
		return err
	}
	id := d.Session.ID
	var path []string
	for step := 0; d.Session.Presented.ItemID != ""; step++ {
		presented := d.Session.Presented
		path = append(path, fmt.Sprintf("%s(b%d)", presented.ItemID, presented.Difficulty))
		ans := session.Answer{OptionID: "a"}
		if step%2 == 0 {
			ans.OptionID = "b"
		}
		if d, err = exec.Answer(ctx, id, presented.ItemID, ans); err != nil {
			return err
		}
	}
	final, err := exec.Complete(ctx, id)
	if err != nil {
		return err
	}
	score, err := scorer.Score(ctx, final)
	if err != nil {
		return err
	}
	fmt.Printf("\nRun-test demo (%s): administered %q\n  path:  %v\n  state: %s\n%s",
		label, final.TestID, path, final.State, formatScore(score))
	return nil
}

// formatScore renders a scoring.Score for the CLI demo: the raw count, the
// norm-derived band / scaled IQ / percentile (or a raw-only note when the test
// is unnormed), the speed dimension, how many per-item explanations are
// available and — if any — how many degraded because their item was removed.
func formatScore(s scoring.Score) string {
	norm := "unnormed (raw only)"
	if s.Normed {
		norm = fmt.Sprintf("band %s, IQ %d, %.1f percentile", s.Band, s.ScaledIQ, s.Percentile)
	}
	ability := ""
	if s.Ability != 0 {
		ability = fmt.Sprintf(", ability %.2f", s.Ability)
	}
	degraded := ""
	if s.DegradedFeedback > 0 {
		degraded = fmt.Sprintf(" (%d degraded: item removed)", s.DegradedFeedback)
	}
	return fmt.Sprintf("  score: %d/%d correct%s\n  norm:  %s\n  speed: %v total, %v/item, %.1f correct/min\n  feedback: %d item explanation(s)%s\n",
		s.Raw, s.Max, ability, norm, s.Speed.Total, s.Speed.Mean, s.Speed.CorrectPerMinute, len(s.Items), degraded)
}

// itemBankDemo exercises the ItemRepository: it builds a validated multiple-choice
// item through the aggregate, stores its snapshot, and queries the bank by
// ability family, test type and difficulty band — the Block 4 "done when".
func itemBankDemo(ctx context.Context, bank ports.ItemRepository) error {
	it, ierr := item.NewItem(item.ItemSpec{
		ID:           "omib-1",
		Provenance:   item.Provenance{SourceID: "omib", Origin: item.OriginFetched, Redistributable: shared.RedistConditional},
		TestType:     "A2",
		Stimulus:     []item.StimulusPart{{Text: "which figure continues the series?"}, {MediaKind: item.MediaGrid, MediaRef: "https://cdn.example.test/omib/1.svg"}},
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

// ingestLLMDemo is the Block 12 "done when": it lifts a source's unstructured
// fetched payload into validated, provenance-tagged item candidates with the LLM
// extraction step. The prompt is loaded from the file-backed store; the backend
// is the same openaicompat adapter used for local (Ollama) or cloud endpoints,
// selected purely by TESTMAKER_LLM_BASE_URL — the composition root is the only
// place that knows either the concrete backend or the prompt store.
func ingestLLMDemo(ctx context.Context, cat *catalog.Service, bank ports.ItemRepository, promptsDir, sourceID string) error {
	if sourceID == "" {
		fmt.Println("\nIngest (LLM): not requested (pass -ingest-llm <source-id> with TESTMAKER_LLM_BASE_URL set); skipping.")
		return nil
	}
	backend, ok, err := newLLMBackend()
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("\nIngest (LLM): TESTMAKER_LLM_BASE_URL not set; skipping.")
		return nil
	}

	store, err := fileprompts.Open(promptsDir)
	if err != nil {
		return err
	}
	snap, err := cat.Get(ctx, source.SourceID(sourceID))
	if err != nil {
		return err
	}
	testType := shared.TestTypeCode("")
	if len(snap.TestTypes) > 0 {
		testType = snap.TestTypes[0]
	}

	// Inject through the port types (like the other adapters at the composition
	// root) so the wiring is an app→ports dependency, not adapter→app.
	var (
		llmBackend ports.LLM              = backend
		prompts    ports.PromptRepository = store
		downloader ports.Fetcher          = httpfetch.New()
		stub       ports.Fetcher          = stubfetcher.NewFetcher()
	)
	svc := ingest.NewService(bank, downloader, stub)

	rep, err := svc.IngestLLM(ctx, ingest.LLMExtractRequest{
		Source:   snap,
		LLM:      llmapp.NewService(llmBackend, prompts),
		TestType: testType,
		Model:    os.Getenv("TESTMAKER_LLM_MODEL"),
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nIngest (LLM) demo (%s): fetched %d artifact(s), extracted %d, saved %d, skipped %d — %s\n",
		rep.SourceID, rep.Fetched, rep.Normalized, rep.Saved, rep.Skipped, rep.Note)
	return nil
}

// generateDemo wires the procedural generator (rulegen) through the authoring
// use-case: with no -generate flag the step is skipped. When a figural test type
// is given, it generates a small deterministic batch and stores it in the bank,
// reporting the per-run counts. The authoring service offloads each item's inline
// figural media (data: URIs) into the blob store, so the demo then reloads one
// item and resolves its media ref back through the same port — the Get side of
// Block 11 — to prove the round-trip. The composition root is the only place that
// knows the concrete generator and blob store.
func generateDemo(ctx context.Context, bank ports.ItemRepository, blobs ports.BlobStore, genType string) error {
	if genType == "" {
		fmt.Println("\nGenerate: not requested (pass -generate <A1|A2|A3|A4>); skipping.")
		return nil
	}

	var gen ports.Generator = rulegen.New()
	svc := authoring.NewService(gen, bank, blobs)

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

	return resolveMediaDemo(ctx, bank, blobs, shared.TestTypeCode(genType))
}

// resolveMediaDemo reloads the just-generated items and resolves the first blob
// ref it finds through the store, reporting the byte length — the renderer does
// the same when serving GET /media/{ref}.
func resolveMediaDemo(ctx context.Context, bank ports.ItemRepository, blobs ports.BlobStore, testType shared.TestTypeCode) error {
	items, err := bank.ListItems(ctx, item.ItemFilter{TestTypes: []shared.TestTypeCode{testType}})
	if err != nil {
		return err
	}
	ref := firstOffloadedRef(items)
	if ref == "" {
		fmt.Println("Media demo: no figural media in this batch to resolve.")
		return nil
	}
	blob, err := blobs.Get(ctx, ref)
	if err != nil {
		return err
	}
	fmt.Printf("Media demo: resolved ref %s… → %d bytes (%s) via the blob store\n",
		ref[:min(12, len(ref))], len(blob.Bytes), blob.ContentType)
	return nil
}

// firstOffloadedRef returns the first offloaded media ref across the items, or ""
// when none carry figural media.
func firstOffloadedRef(items []item.ItemSnapshot) string {
	for _, it := range items {
		if refs := mediaRefs(it); len(refs) > 0 {
			return refs[0]
		}
	}
	return ""
}

// mediaRefs collects the offloaded media refs of an item (skipping inline data
// URIs and empty refs) so the demo can resolve one through the blob store.
func mediaRefs(snap item.ItemSnapshot) []string {
	var refs []string
	for _, p := range snap.Stimulus {
		if p.MediaRef != "" && !strings.HasPrefix(p.MediaRef, "data:") {
			refs = append(refs, p.MediaRef)
		}
	}
	for _, o := range snap.Options {
		if o.MediaRef != "" && !strings.HasPrefix(o.MediaRef, "data:") {
			refs = append(refs, o.MediaRef)
		}
	}
	return refs
}

// newLLMBackend builds the openaicompat backend from TESTMAKER_LLM_* config. The
// bool reports whether a backend is configured (TESTMAKER_LLM_BASE_URL set); when
// false the caller skips its LLM step and the CLI still runs.
func newLLMBackend() (*openaicompat.Client, bool, error) {
	baseURL := os.Getenv("TESTMAKER_LLM_BASE_URL")
	if baseURL == "" {
		return nil, false, nil
	}
	client, err := openaicompat.New(openaicompat.Config{
		BaseURL:    baseURL,
		APIKey:     os.Getenv("TESTMAKER_LLM_API_KEY"),
		AuthScheme: openaicompat.AuthScheme(os.Getenv("TESTMAKER_LLM_AUTH_SCHEME")),
	})
	if err != nil {
		return nil, false, err
	}
	return client, true, nil
}

// llmDemo wires the OpenAI-compatible LLM adapter behind config: with no
// TESTMAKER_LLM_BASE_URL the step is skipped and the CLI still runs. When
// configured, the adapter is used through ports.LLM — the composition root is
// the only place that knows the concrete backend.
func llmDemo(ctx context.Context, userPrompt string) error {
	backend, ok, err := newLLMBackend()
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("\nLLM: not configured (set TESTMAKER_LLM_BASE_URL to enable); skipping.")
		return nil
	}
	if userPrompt == "" {
		fmt.Println("\nLLM: configured; pass -llm-prompt to run a completion.")
		return nil
	}

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
