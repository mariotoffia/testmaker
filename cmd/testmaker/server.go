package main

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mariotoffia/testmaker/adapters/native/blob/fsblob"
	"github.com/mariotoffia/testmaker/adapters/native/blob/memoryblob"
	"github.com/mariotoffia/testmaker/adapters/native/generate/rulegen"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/sqlitetestdb"
	"github.com/mariotoffia/testmaker/app/authoring"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/app/execution"
	"github.com/mariotoffia/testmaker/app/ingest"
	llmapp "github.com/mariotoffia/testmaker/app/llm"
	scoringapp "github.com/mariotoffia/testmaker/app/scoring"
	"github.com/mariotoffia/testmaker/domain/clock"
	"github.com/mariotoffia/testmaker/domain/scoring"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

// testDB bundles the three TestDb ports behind one concrete value plus the
// close hook of the backing store, so openTestDB is the single place that knows
// whether the surface is memory- or sqlite-backed.
type testDB struct {
	tests    ports.TestRepository
	items    ports.ItemRepository
	sessions ports.SessionRepository
	close    func() error
}

// openTestDB selects the TestDb backend from a DSN: the dependency-free
// in-memory store by default, or the durable sqlite adapter behind a DSN. One
// concrete *Store satisfies every TestDb port, so the caller wires ports only.
func openTestDB(dsn string) (testDB, error) {
	if dsn == "" || dsn == "memory" {
		mem := memorytestdb.NewStore()
		return testDB{tests: mem, items: mem, sessions: mem, close: func() error { return nil }}, nil
	}
	// A file-backed sqlite db needs its parent directory to exist — the driver
	// creates the file but not the directory. Create it (as fsblob.Open does for
	// blobs) so a config-driven server is self-sufficient when run directly, not
	// only via `make serve`. A bare filename or ":memory:" has dir "." — a no-op.
	if dir := filepath.Dir(dsn); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return testDB{}, fmt.Errorf("create db dir %q: %w", dir, err)
		}
	}
	store, err := sqlitetestdb.Open(dsn)
	if err != nil {
		return testDB{}, err
	}
	return testDB{tests: store, items: store, sessions: store, close: store.Close}, nil
}

// openBlobStore selects the BlobStore backend from a spec: the dependency-free
// in-memory store by default (mirrors the memory TestDb), or the filesystem
// adapter rooted at a directory. It is the Get side of Block 11's media port —
// the renderer resolves an item's figural media ref back to bytes through it.
//
//nolint:ireturn // a composition-root factory for the BlobStore port; handing back the interface is the point.
func openBlobStore(spec string) (ports.BlobStore, error) {
	if spec == "" || spec == "memory" {
		return memoryblob.NewStore(), nil
	}
	return fsblob.Open(spec)
}

// server is the HTTP delivery surface: it wires the authoring, execution and
// scoring use-cases to net/http handlers so a client can author, take and be
// scored on a test. It is the driving side of the hexagon, and — like the CLI
// demo — lives in the composition root because it depends on app use-cases,
// which no adapter is allowed to import.
type server struct {
	gen           *authoring.Service
	author        *authoring.TestService
	exec          ports.Executor
	scorer        ports.Scorer
	cat           *catalog.Service
	ingestSvc     *ingest.Service
	items         ports.ItemRepository
	llm           *llmapp.Service // nil when the deployment has no LLM backend configured
	llmModel      string
	tests         ports.TestRepository
	sessions      ports.SessionRepository
	blobs         ports.BlobStore
	auth          *authenticator // role checks; zero-value AuthConfig ⇒ enforced() false
	log           *slog.Logger   // nil → s.logger() hands back a discard logger
	ingestSem     semaphore      // bounds concurrent ingests; nil when maxIngest ≤ 0 ⇒ ungated
	catalogPath   string         // where POST /api/catalog persists an uploaded catalogue; "" ⇒ upload unsupported
	jobs          *jobRegistry   // recent async ingest jobs; nil ⇒ async ingest disabled (sync only)
	ingestTimeout time.Duration  // background async-run cap; newServer defaults a zero to 10m
}

// serverDeps bundles everything the delivery surface drives: the TestDb-backed
// repositories, the blob store, the catalogue and ingest use-cases, and an
// optional LLM service (nil disables the LLM ingest endpoint). Grouping them keeps
// newServer's signature stable as the surface grows.
type serverDeps struct {
	db            testDB
	blobs         ports.BlobStore
	catalog       *catalog.Service
	ingest        *ingest.Service
	llm           *llmapp.Service
	llmModel      string
	authCfg       AuthConfig  // zero value ⇒ Mode "" ⇒ auth off (what most tests construct)
	clock         clock.Clock // nil → clock.System(); drives invite expiry
	log           *slog.Logger
	maxIngest     int           // > 0 ⇒ bound concurrent ingests with a semaphore; 0 ⇒ ungated
	catalogPath   string        // POST /api/catalog target; "" ⇒ upload returns 501
	jobs          *jobRegistry  // async ingest job registry; nil ⇒ async ingest disabled
	ingestTimeout time.Duration // background async-run cap; 0 ⇒ newServer defaults it to 10m
}

// newServer wires the delivery use-cases over one TestDb backend and one blob
// store. It injects the system clock into the executor and an empty norm book
// into the scorer: norms are deployment configuration (a demo book is
// illustrative), so the API returns raw scores and per-item feedback and leaves
// the normed band unset until a deployment supplies real norms. The blob store
// backs both the offload side (authoring rewrites inline media to content refs)
// and the resolve side (GET /media/{ref}).
func newServer(d serverDeps) *server {
	// Bind the generator to its port before injection so the composition wiring
	// reads as ports-only (mirrors main.go and keeps the arch graph clean: the
	// rulegen adapter never appears to depend on app).
	var gen ports.Generator = rulegen.New()
	clk := d.clock
	if clk == nil {
		clk = clock.System()
	}
	// Bound concurrent ingests only when configured (> 0); a nil semaphore leaves
	// the ingest handlers ungated, which is what the zero-value test server wants.
	var sem semaphore
	if d.maxIngest > 0 {
		sem = newSemaphore(d.maxIngest)
	}
	return &server{
		gen:         authoring.NewService(gen, d.db.items, d.blobs),
		author:      authoring.NewTestService(d.db.items, d.db.tests),
		exec:        execution.NewService(clock.System(), d.db.items, d.db.sessions, execution.RandomIDs()),
		scorer:      scoringapp.NewService(d.db.items, scoring.NormBook{}),
		cat:         d.catalog,
		ingestSvc:   d.ingest,
		items:       d.db.items,
		llm:         d.llm,
		llmModel:    d.llmModel,
		tests:       d.db.tests,
		sessions:    d.db.sessions,
		blobs:       d.blobs,
		auth:        newAuthenticator(d.authCfg, clk),
		log:         d.log,
		ingestSem:   sem,
		catalogPath: d.catalogPath,
		jobs:        d.jobs,
		// A zero timeout would make every background run's context expire on
		// creation; default it so a serverDeps that omits it (most tests) is safe.
		ingestTimeout: cmp.Or(d.ingestTimeout, 10*time.Minute),
	}
}

// routes maps the delivery verbs onto the use-cases, all under the /api
// prefix (ADR-0005); everything else falls through to the SPA handler. The
// patterns use the Go 1.22 method+path router — registered /api patterns are
// more specific than "GET /", so they always win over the SPA catch-all.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	// Public: proof-of-life, role probe, media resolution.
	mux.HandleFunc("GET /api", s.handleIndex)
	mux.HandleFunc("GET /api/auth/whoami", s.handleWhoami)
	mux.HandleFunc("GET /api/media/{ref}", s.handleMedia)
	// Operator-only: authoring, composition, item bank, sourcing.
	mux.HandleFunc("POST /api/items/generate", s.requireOperator(s.handleGenerate))
	mux.HandleFunc("POST /api/tests", s.requireOperator(s.handleCompose))
	mux.HandleFunc("GET /api/tests", s.requireOperator(s.handleListTests))
	mux.HandleFunc("GET /api/tests/{id}", s.requireOperator(s.handleGetTest))
	mux.HandleFunc("POST /api/tests/{id}/sessions", s.requireOperator(s.handleStartSession))
	mux.HandleFunc("POST /api/tests/{id}/invites", s.requireOperator(s.handleMintInvite))
	mux.HandleFunc("GET /api/sources", s.requireOperator(s.handleListSources))
	mux.HandleFunc("GET /api/sources/{id}", s.requireOperator(s.handleGetSource))
	mux.HandleFunc("POST /api/catalog", s.requireOperator(s.handleUploadCatalog))
	mux.HandleFunc("POST /api/catalog/sync", s.requireOperator(s.handleSyncCatalog))
	mux.HandleFunc("GET /api/items", s.requireOperator(s.handleListItems))
	mux.HandleFunc("GET /api/items/{id}", s.requireOperator(s.handleGetItem))
	mux.HandleFunc("POST /api/sources/{id}/ingest", s.requireOperator(s.handleIngest))
	mux.HandleFunc("POST /api/sources/{id}/ingest-llm", s.requireOperator(s.handleIngestLLM))
	mux.HandleFunc("GET /api/jobs", s.requireOperator(s.handleListJobs))
	mux.HandleFunc("GET /api/jobs/{id}", s.requireOperator(s.handleGetJob))
	// Invite-scoped: a valid invite token (operator token NOT accepted here — an
	// operator starts via POST /api/tests/{id}/sessions).
	mux.HandleFunc("GET /api/invites/preview", s.requireInvite(s.handleInvitePreview))
	mux.HandleFunc("POST /api/invites/start", s.requireInvite(s.handleInviteStart))
	// Session-scoped: the session's own token (or operator) drives these.
	mux.HandleFunc("POST /api/sessions/{id}/answers", s.requireSession(s.handleAnswer))
	mux.HandleFunc("POST /api/sessions/{id}/complete", s.requireSession(s.handleComplete))
	mux.HandleFunc("GET /api/sessions/{id}/score", s.requireSession(s.handleScore))
	// Everything that is not /api is the web app (or the JSON fallback).
	mux.HandleFunc("GET /", s.handleSPA)
	return mux
}

// handleIndex serves the JSON service index at GET /api: proof of life plus
// the endpoint list. It is also the GET / fallback body when no UI is built.
func (s *server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "testmaker",
		"status":  "ok",
		"endpoints": []string{
			"GET /api", "GET /api/auth/whoami",
			"GET /api/sources", "GET /api/sources/{id}",
			"POST /api/catalog", "POST /api/catalog/sync",
			"GET /api/items", "GET /api/items/{id}",
			"POST /api/sources/{id}/ingest", "POST /api/sources/{id}/ingest-llm",
			"GET /api/jobs", "GET /api/jobs/{id}",
			"POST /api/items/generate", "POST /api/tests", "GET /api/tests", "GET /api/tests/{id}",
			"POST /api/tests/{id}/sessions", "POST /api/tests/{id}/invites",
			"GET /api/invites/preview", "POST /api/invites/start",
			"POST /api/sessions/{id}/answers",
			"POST /api/sessions/{id}/complete", "GET /api/sessions/{id}/score",
			"GET /api/media/{ref}",
		},
	})
}

// runServer opens the TestDb and blob-store backends and serves the delivery API
// on addr until the process is stopped. Timing values in requests are expressed
// in seconds so the wire format stays clock-free; the executor enforces them
// through the injected clock.
func runServer(addr string, cfg Config) (err error) {
	// Structured logging: JSON to stderr at the configured level (unknown → Info).
	// The request-log middleware wraps the whole mux and measures duration through
	// the injected clock, so no handler reads the wall clock directly.
	level := slog.LevelInfo
	_ = level.UnmarshalText([]byte(cfg.Log.Level)) // unknown → Info (level unchanged)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	handler, closeFn, err := buildDeliveryHandler(cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := closeFn(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "testmaker: serving delivery API on %s (auth=%s, testdb=%s, blobs=%s)\n", addr, cfg.Auth.Mode, cfg.TestDB, cfg.Blobs)
	if lerr := httpSrv.ListenAndServe(); lerr != nil && !errors.Is(lerr, http.ErrServerClosed) {
		return fmt.Errorf("serve %s: %w", addr, lerr)
	}
	return nil
}

// buildDeliveryHandler opens the backends, wires the use-cases, and assembles the
// full middleware chain (auth guards in routes, then rate limit, security headers,
// request log) into one http.Handler — everything runServer needs short of binding
// the port. Split out so a test can drive the exact production wiring without a
// live socket. The returned close func releases the TestDb backend; callers defer
// it.
func buildDeliveryHandler(cfg Config, logger *slog.Logger) (http.Handler, func() error, error) {
	db, err := openTestDB(cfg.TestDB)
	if err != nil {
		return nil, nil, err
	}
	blobs, err := openBlobStore(cfg.Blobs)
	if err != nil {
		return nil, nil, errors.Join(err, db.close())
	}
	cat, ing, llmSvc, llmModel, err := wireSourcing(db.items, cfg.Catalog, cfg.Prompts, cfg.LLM)
	if err != nil {
		return nil, nil, errors.Join(err, db.close())
	}
	srv := newServer(serverDeps{
		db: db, blobs: blobs, catalog: cat, ingest: ing, llm: llmSvc, llmModel: llmModel, log: logger,
		maxIngest:     cfg.Limits.MaxConcurrentIngests,
		authCfg:       cfg.Auth, // enforce the configured auth mode on the real -serve path
		catalogPath:   cfg.Catalog,
		jobs:          newJobRegistry(clock.System(), 100, nil),
		ingestTimeout: time.Duration(cfg.Limits.IngestTimeoutSeconds) * time.Second,
	})
	// Per-IP token-bucket rate limit on /api (0 rps in config ⇒ off). Nested
	// inside the security headers so an over-limit 429 still carries them.
	var limiter *rateLimiter
	if cfg.Limits.RequestsPerSecond > 0 {
		limiter = newRateLimiter(cfg.Limits.RequestsPerSecond, cfg.Limits.Burst, clock.System())
	}
	handler := withRequestLog(withSecurityHeaders(withRateLimit(srv.routes(), limiter)), logger, clock.System())
	return handler, db.close, nil
}

// --- request bodies (seconds for timing; ids arrive on the path) ---

type generateReq struct {
	TestType   string `json:"testType"`
	Difficulty int    `json:"difficulty"`
	Count      int    `json:"count"`
	Seed       int64  `json:"seed"`
}

type sectionReq struct {
	Title          string `json:"title"`
	Family         string `json:"family"`
	TotalSeconds   int    `json:"totalSeconds"`
	PerItemSeconds int    `json:"perItemSeconds"`
	MinDifficulty  int    `json:"minDifficulty"`
	MaxDifficulty  int    `json:"maxDifficulty"`
}

type composeReq struct {
	ID             string       `json:"id"`
	Title          string       `json:"title"`
	Policy         string       `json:"policy"`
	TotalSeconds   int          `json:"totalSeconds"`
	PerItemSeconds int          `json:"perItemSeconds"`
	Sections       []sectionReq `json:"sections"`
}

type answerReq struct {
	ItemID   string  `json:"itemId"`
	OptionID string  `json:"optionId"`
	Numeric  float64 `json:"numeric"`
	Verdict  string  `json:"verdict"`
}

// --- handlers ---

func (s *server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req generateReq
	if !s.decodeJSON(w, r, &req) {
		return
	}
	rep, err := s.gen.Generate(r.Context(), ports.GenerateSpec{
		TestType:   shared.TestTypeCode(req.TestType),
		Difficulty: req.Difficulty,
		Count:      req.Count,
		Seed:       req.Seed,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *server) handleCompose(w http.ResponseWriter, r *http.Request) {
	var req composeReq
	if !s.decodeJSON(w, r, &req) {
		return
	}
	sections := make([]authoring.SectionSpec, len(req.Sections))
	for i, ss := range req.Sections {
		sections[i] = authoring.SectionSpec{
			Title:         ss.Title,
			Family:        shared.AbilityFamily(ss.Family),
			Timing:        timing(ss.TotalSeconds, ss.PerItemSeconds),
			MinDifficulty: ss.MinDifficulty,
			MaxDifficulty: ss.MaxDifficulty,
		}
	}
	id, err := s.author.Compose(r.Context(), authoring.ComposeSpec{
		ID:       testset.TestID(req.ID),
		Title:    req.Title,
		Policy:   testset.DeliveryPolicy(req.Policy),
		Timing:   timing(req.TotalSeconds, req.PerItemSeconds),
		Sections: sections,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	snap, err := s.tests.GetTest(r.Context(), id)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, snap)
}

// handleListTests returns the composed-test catalogue, paginated (C5). TestFilter
// is currently empty (no criteria), so this lists all tests; the repository sorts
// by id, so pages are stable.
func (s *server) handleListTests(w http.ResponseWriter, r *http.Request) {
	tests, err := s.tests.ListTests(r.Context(), testset.TestFilter{})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	limit, offset, ok := s.pageParams(w, r, r.URL.Query())
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, paginate(tests, limit, offset))
}

func (s *server) handleGetTest(w http.ResponseWriter, r *http.Request) {
	snap, err := s.tests.GetTest(r.Context(), testset.TestID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	test, err := s.tests.GetTest(r.Context(), testset.TestID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.startAndRespond(w, r, test)
}

func (s *server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var req answerReq
	if !s.decodeJSON(w, r, &req) {
		return
	}
	d, err := s.exec.Answer(r.Context(), session.SessionID(r.PathValue("id")), req.ItemID, session.Answer{
		OptionID: req.OptionID,
		Numeric:  req.Numeric,
		Verdict:  req.Verdict,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) handleComplete(w http.ResponseWriter, r *http.Request) {
	snap, err := s.exec.Complete(r.Context(), session.SessionID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleScore(w http.ResponseWriter, r *http.Request) {
	snap, err := s.sessions.GetSession(r.Context(), session.SessionID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	score, err := s.scorer.Score(r.Context(), snap)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, score)
}

// handleMedia resolves a figural media ref back to its bytes through the blob
// store — the renderer side of Block 11. An item's Stimulus/Option MediaRef
// (rewritten to a content ref at authoring time) is fetched here and served with
// its stored content type; an unknown ref surfaces as 404 via writeError.
//
// Stored media is served with a hardened posture: the content type comes from an
// item producer and figural media is SVG (script-executable in a browser), so
// nosniff pins the declared type and a locked-down CSP sandbox neutralizes any
// script an SVG carries — the endpoint cannot become a stored-XSS vector on the
// assessment origin even once caller-supplied media can be offloaded.
func (s *server) handleMedia(w http.ResponseWriter, r *http.Request) {
	blob, err := s.blobs.Get(r.Context(), r.PathValue("ref"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", blob.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(blob.Bytes)
}

// timing (wire seconds → domain testset.Timing) lives in server_http.go with the
// other transport helpers, keeping server.go under the per-file cap.
