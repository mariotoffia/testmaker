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

	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat"
	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/sqlitetestdb"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

func main() {
	path := flag.String("catalog", "data/catalog/sources.json", "path to the source catalogue (json or yaml)")
	testdbDSN := flag.String("testdb", "memory", `TestDb backend: "memory" or a sqlite DSN (a file path or ":memory:")`)
	llmPrompt := flag.String("llm-prompt", "", "if set (and TESTMAKER_LLM_BASE_URL is configured), send this prompt to the LLM backend")
	flag.Parse()

	if err := run(context.Background(), *path, *testdbDSN, *llmPrompt); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, path, testdbDSN, llmPrompt string) (err error) {
	// --- composition root: choose adapters, wire the service ---
	var (
		repo    ports.SourceRepository = memorycatalog.NewStore()
		loader  ports.CatalogLoader    = filecatalog.NewLoader(path)
		fetcher ports.Fetcher          = stubfetcher.NewFetcher()
		testdb  ports.TestRepository   = memorytestdb.NewStore()
	)
	// The default is the dependency-free in-memory store; a sqlite DSN swaps in
	// the durable adapter. Both satisfy ports.TestRepository, so nothing below
	// changes — the only place that knows the concrete backend is right here.
	closeTestDB := func() error { return nil }
	if testdbDSN != "" && testdbDSN != "memory" {
		store, oerr := sqlitetestdb.Open(testdbDSN)
		if oerr != nil {
			return oerr
		}
		testdb, closeTestDB = store, store.Close
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

	// Exercise the fetch boundary (stub) against one generator source.
	if len(gens) > 0 && fetcher.Supports(gens[0]) {
		res, ferr := fetcher.Fetch(ctx, ports.FetchRequest{Source: gens[0], Limit: 5})
		if ferr != nil {
			return ferr
		}
		fmt.Printf("\nFetch demo (%s): %s\n", res.SourceID, res.Note)
	}

	if err := testDbDemo(ctx, testdb); err != nil {
		return err
	}
	return llmDemo(ctx, llmPrompt)
}

// testDbDemo exercises the in-memory TestDb (the default ports.TestRepository)
// with a save/reload round-trip. Test authoring proper arrives in a later block;
// this just proves the store is wired at the composition root.
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
