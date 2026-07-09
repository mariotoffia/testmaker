package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/apifetch"
	"github.com/mariotoffia/testmaker/adapters/native/fetch/httpfetch"
	"github.com/mariotoffia/testmaker/adapters/native/fetch/scrapefetch"
	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/adapters/native/llm/fileprompts"
	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/app/ingest"
	llmapp "github.com/mariotoffia/testmaker/app/llm"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// --- request bodies for the sourcing / ingest endpoints (all fields optional) ---

type ingestReq struct {
	Limit int  `json:"limit"`
	Async bool `json:"async"` // true ⇒ 202 + a poll-able job instead of a synchronous Report
}

type ingestLLMReq struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"maxTokens"`
	Limit     int    `json:"limit"`
	// TestType overrides the taxonomy code the extracted items are tagged with.
	// Empty falls back to the source's first test type — but a multi-family source
	// would then mislabel every item, so a caller should set this per run.
	TestType string `json:"testType"`
	Async    bool   `json:"async"` // true ⇒ 202 + a poll-able job instead of a synchronous Report
}

// errLLMUnconfigured marks an LLM-ingest call on a deployment with no LLM backend
// wired — a deployment-capability gap (503), not a client error.
var errLLMUnconfigured = &shared.TestmakerError{
	Code:    "server.llm_unconfigured",
	Class:   shared.ClassUnavailable,
	Message: "LLM ingest is unavailable: no LLM backend configured (set TESTMAKER_LLM_BASE_URL)",
}

// wireSourcing builds the catalogue, ingest, and optional LLM services the sourcing
// endpoints drive, loading the catalogue into an in-memory repository at startup.
// It is the composition root for the front half of the pipeline, mirroring the CLI
// wiring in main.go: the same fetchers, the same per-source normalizers, and the
// same env-driven LLM backend (nil when TESTMAKER_LLM_BASE_URL is unset). The LLM
// model id is read once here and carried on the server.
func wireSourcing(bank ports.ItemRepository, catalogPath, promptsDir string, llmCfg LLMConfig) (*catalog.Service, *ingest.Service, *llmapp.Service, string, error) {
	// Bind every adapter to its port before injection (as main.go does) so the
	// composition wiring reads as app -> ports, never adapter -> app: go-arch-lint
	// forbids an adapter type flowing into an app constructor at the call site.
	var (
		repo   ports.SourceRepository = memorycatalog.NewStore()
		loader ports.CatalogLoader    = filecatalog.NewLoader(catalogPath)
	)
	cat := catalog.NewService(repo, loader)
	// Tolerate a missing catalogue file (a fresh install before it is seeded):
	// serve an empty catalogue rather than failing to boot. POST /catalog/sync
	// loads it once the file exists.
	if _, statErr := os.Stat(catalogPath); statErr == nil {
		if _, err := cat.Sync(context.Background()); err != nil {
			return nil, nil, nil, "", err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, nil, nil, "", fmt.Errorf("stat catalogue %s: %w", catalogPath, statErr)
	} else {
		fmt.Fprintf(os.Stderr, "testmaker: catalogue %q not found; serving an empty catalogue\n", catalogPath)
	}

	var (
		downloader ports.Fetcher = httpfetch.New()
		scraper    ports.Fetcher = scrapefetch.New()
		api        ports.Fetcher = apifetch.New()
		stub       ports.Fetcher = stubfetcher.NewFetcher()
	)
	ing := ingest.NewService(bank, downloader, scraper, api, stub)
	ing.Register(ingest.VIQTSourceID, ingest.VIQTNormalizer)
	ing.Register(ingest.ASVABSourceID, ingest.ASVABNormalizer)

	backend, ok, err := newLLMBackendFrom(llmCfg)
	if err != nil {
		return nil, nil, nil, "", err
	}
	var llmSvc *llmapp.Service
	if ok {
		store, perr := fileprompts.Open(promptsDir)
		if perr != nil {
			return nil, nil, nil, "", perr
		}
		var (
			llmBackend ports.LLM              = backend
			prompts    ports.PromptRepository = store
		)
		// Clamp caller-controlled spend server-side: cap MaxTokens and gate models
		// (DESIGN.md §7.4). The composition root is the only place that registers a
		// BeforeGenerate hook; steps never do.
		llmSvc = llmapp.NewService(llmBackend, prompts,
			llmapp.WithBeforeGenerate(llmClampHook(llmCfg.MaxTokensCap, llmCfg.AllowedModels)))
	}
	model := llmCfg.Model
	if model == "" {
		model = os.Getenv("TESTMAKER_LLM_MODEL")
	}
	return cat, ing, llmSvc, model, nil
}

// --- catalogue handlers ---

// handleListSources returns the catalogue, narrowed by optional query filters
// (generators, category, family, testType, redistributable) mapped onto a
// source.SourceFilter.
func (s *server) handleListSources(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := source.SourceFilter{GeneratorsOnly: q.Get("generators") == "true"}
	if v := q.Get("category"); v != "" {
		filter.Categories = []source.Category{source.Category(v)}
	}
	if v := q.Get("family"); v != "" {
		filter.Families = []shared.AbilityFamily{shared.AbilityFamily(v)}
	}
	if v := q.Get("testType"); v != "" {
		filter.TestTypes = []shared.TestTypeCode{shared.TestTypeCode(v)}
	}
	if v := q.Get("redistributable"); v != "" {
		filter.Redistributable = []shared.Redistributable{shared.Redistributable(v)}
	}
	sources, err := s.cat.List(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	limit, offset, ok := s.pageParams(w, r, q)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, paginate(sources, limit, offset))
}

// handleGetSource returns one catalogue source; an unknown id is a 404 via the
// catalogue's ErrUnknownSource.
func (s *server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	snap, err := s.cat.Get(r.Context(), source.SourceID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// handleSyncCatalog reloads the catalogue from its loader and reports the count.
func (s *server) handleSyncCatalog(w http.ResponseWriter, r *http.Request) {
	n, err := s.cat.Sync(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"synced": n})
}

// --- item-bank handlers ---

// handleListItems queries the item bank, narrowed by optional query filters
// (family, testType, minDifficulty, maxDifficulty) mapped onto an item.ItemFilter.
//
// Security note: the returned ItemSnapshot carries AnswerKey and Explanation, so
// this is an authoring/operator view of the bank, not a taker-safe one. The whole
// surface is unauthenticated single-tenant by design; a taker-facing deployment
// needs authentication and a key-redacted item projection first (ROADMAP §1).
func (s *server) handleListItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := item.ItemFilter{}
	if v := q.Get("family"); v != "" {
		filter.Families = []shared.AbilityFamily{shared.AbilityFamily(v)}
	}
	if v := q.Get("testType"); v != "" {
		filter.TestTypes = []shared.TestTypeCode{shared.TestTypeCode(v)}
	}
	minD, ok := s.intParam(w, r, q, "minDifficulty")
	if !ok {
		return
	}
	maxD, ok := s.intParam(w, r, q, "maxDifficulty")
	if !ok {
		return
	}
	filter.MinDifficulty, filter.MaxDifficulty = minD, maxD

	items, err := s.items.ListItems(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	limit, offset, ok := s.pageParams(w, r, q)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, paginate(items, limit, offset))
}

// handleGetItem returns one bank item; an unknown id is a 404 via ErrUnknownItem.
func (s *server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	snap, err := s.items.GetItem(r.Context(), item.ItemID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// --- ingest handlers ---

// handleIngest runs the deterministic fetch -> normalize -> validate -> store
// pipeline for a catalogue source and returns the per-stage Report. An unknown
// source is a 404; a source with no fetcher/normalizer is a 501; a mapping that
// rejects every spec is a 400 — all via the ingest service's own error classes.
func (s *server) handleIngest(w http.ResponseWriter, r *http.Request) {
	var req ingestReq
	if !s.decodeOptionalJSON(w, r, &req) {
		return
	}
	snap, err := s.cat.Get(r.Context(), source.SourceID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	// Async: hand back a 202 + job now and run on a background context. Branched
	// before the sync semaphore gate — a queued job waits for its slot inside the
	// runner, so the caller is never blocked by another ingest in progress.
	if req.Async && s.jobs != nil {
		j := s.jobs.create("ingest", string(snap.ID))
		go s.runIngestJob(j.ID, snap, req.Limit)
		writeJSON(w, http.StatusAccepted, j)
		return
	}
	// Bound concurrent ingests: a full gate is a 429 rather than an unbounded fan
	// of outbound fetches. Acquired after validation so a 404 never burns a slot.
	if s.ingestSem != nil {
		if !s.ingestSem.tryAcquire() {
			writeAuthError(w, http.StatusTooManyRequests, "limit.ingest", "another ingest is in progress")
			return
		}
		defer s.ingestSem.release()
	}
	rep, err := s.ingestSvc.Ingest(r.Context(), snap, req.Limit)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleIngestLLM lifts a source's unstructured payload into validated bank items
// with the LLM extraction step. It is 503 when no LLM backend is configured; the
// request may name a model (falling back to the server's configured model) and a
// fetch limit. The extracted items are tagged with the source's first test type.
func (s *server) handleIngestLLM(w http.ResponseWriter, r *http.Request) {
	if s.llm == nil {
		s.writeError(w, r, errLLMUnconfigured)
		return
	}
	var req ingestLLMReq
	if !s.decodeOptionalJSON(w, r, &req) {
		return
	}
	snap, err := s.cat.Get(r.Context(), source.SourceID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	model := req.Model
	if model == "" {
		model = s.llmModel
	}
	// Prefer the caller's explicit test type; fall back to the source's first code
	// only when unset. A multi-family source needs the override to avoid tagging
	// every extracted item with one family.
	testType := shared.TestTypeCode(req.TestType)
	if testType == "" && len(snap.TestTypes) > 0 {
		testType = snap.TestTypes[0]
	}
	extractReq := ingest.LLMExtractRequest{
		Source:    snap,
		LLM:       s.llm,
		TestType:  testType,
		Model:     model,
		MaxTokens: req.MaxTokens,
		Limit:     req.Limit,
	}
	// Async: 202 + a background run, mirroring the deterministic ingest path.
	if req.Async && s.jobs != nil {
		j := s.jobs.create("ingest-llm", string(snap.ID))
		go s.runIngestLLMJob(j.ID, extractReq)
		writeJSON(w, http.StatusAccepted, j)
		return
	}
	// Same concurrency gate as the deterministic ingest; here it also bounds paid
	// LLM spend by capping how many extractions run at once.
	if s.ingestSem != nil {
		if !s.ingestSem.tryAcquire() {
			writeAuthError(w, http.StatusTooManyRequests, "limit.ingest", "another ingest is in progress")
			return
		}
		defer s.ingestSem.release()
	}
	rep, err := s.ingestSvc.IngestLLM(r.Context(), extractReq)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// recoverJob turns a panic in a background ingest runner into a failed job
// rather than a process-wide crash. net/http recovers a panic in a synchronous
// handler goroutine into a 500; these runners execute in their own goroutines,
// which net/http does not shield, so without this a panicking fetcher/normalizer
// on the async path would take the whole server down (the sync path survives).
func (s *server) recoverJob(id string) {
	if p := recover(); p != nil {
		s.jobs.finish(id, nil, fmt.Errorf("ingest job panicked: %v", p))
	}
}

// runIngestJob executes a deterministic ingest on a background context and
// records the outcome on the job. It acquires the shared ingest semaphore
// (blocking, so a queued job waits its turn) and honours the configured timeout.
// The request context is gone by the time this runs, so it uses its own.
func (s *server) runIngestJob(id string, snap source.Snapshot, limit int) {
	defer s.recoverJob(id)
	ctx, cancel := context.WithTimeout(context.Background(), s.ingestTimeout)
	defer cancel()
	if s.ingestSem != nil {
		if aerr := s.ingestSem.acquire(ctx); aerr != nil {
			s.jobs.finish(id, nil, aerr)
			return
		}
		defer s.ingestSem.release()
	}
	s.jobs.start(id)
	rep, err := s.ingestSvc.Ingest(ctx, snap, limit)
	if err != nil {
		s.jobs.finish(id, nil, err)
		return
	}
	s.jobs.finish(id, &rep, nil)
}

// runIngestLLMJob is runIngestJob for the LLM extraction path: same background
// context, timeout, and semaphore discipline, calling IngestLLM instead.
func (s *server) runIngestLLMJob(id string, req ingest.LLMExtractRequest) {
	defer s.recoverJob(id)
	ctx, cancel := context.WithTimeout(context.Background(), s.ingestTimeout)
	defer cancel()
	if s.ingestSem != nil {
		if aerr := s.ingestSem.acquire(ctx); aerr != nil {
			s.jobs.finish(id, nil, aerr)
			return
		}
		defer s.ingestSem.release()
	}
	s.jobs.start(id)
	rep, err := s.ingestSvc.IngestLLM(ctx, req)
	if err != nil {
		s.jobs.finish(id, nil, err)
		return
	}
	s.jobs.finish(id, &rep, nil)
}

// The decodeOptionalJSON/intParam helpers moved to server_http.go (as *server
// methods, so a decode/parse failure routes through the same safe s.writeError).
