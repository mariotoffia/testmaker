// Command testmaker is a thin composition root demonstrating the source
// catalogue vertical slice: it wires the file loader, the in-memory repository
// and the stub fetcher into the catalogue application service, loads the
// research catalogue and reports on it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat"
	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

func main() {
	path := flag.String("catalog", "data/catalog/sources.json", "path to the source catalogue (json or yaml)")
	llmPrompt := flag.String("llm-prompt", "", "if set (and TESTMAKER_LLM_BASE_URL is configured), send this prompt to the LLM backend")
	flag.Parse()

	if err := run(context.Background(), *path, *llmPrompt); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, path, llmPrompt string) error {
	// --- composition root: choose adapters, wire the service ---
	var (
		repo    ports.SourceRepository = memorycatalog.NewStore()
		loader  ports.CatalogLoader    = filecatalog.NewLoader(path)
		fetcher ports.Fetcher          = stubfetcher.NewFetcher()
	)
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
	return llmDemo(ctx, llmPrompt)
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
