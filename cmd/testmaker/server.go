package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/mariotoffia/testmaker/adapters/native/blob/fsblob"
	"github.com/mariotoffia/testmaker/adapters/native/blob/memoryblob"
	"github.com/mariotoffia/testmaker/adapters/native/generate/rulegen"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb"
	"github.com/mariotoffia/testmaker/adapters/native/testdb/sqlitetestdb"
	"github.com/mariotoffia/testmaker/app/authoring"
	"github.com/mariotoffia/testmaker/app/execution"
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
	gen      *authoring.Service
	author   *authoring.TestService
	exec     ports.Executor
	scorer   ports.Scorer
	tests    ports.TestRepository
	sessions ports.SessionRepository
	blobs    ports.BlobStore
}

// newServer wires the delivery use-cases over one TestDb backend and one blob
// store. It injects the system clock into the executor and an empty norm book
// into the scorer: norms are deployment configuration (a demo book is
// illustrative), so the API returns raw scores and per-item feedback and leaves
// the normed band unset until a deployment supplies real norms. The blob store
// backs both the offload side (authoring rewrites inline media to content refs)
// and the resolve side (GET /media/{ref}).
func newServer(db testDB, blobs ports.BlobStore) *server {
	// Bind the generator to its port before injection so the composition wiring
	// reads as ports-only (mirrors main.go and keeps the arch graph clean: the
	// rulegen adapter never appears to depend on app).
	var gen ports.Generator = rulegen.New()
	return &server{
		gen:      authoring.NewService(gen, db.items, blobs),
		author:   authoring.NewTestService(db.items, db.tests),
		exec:     execution.NewService(clock.System(), db.items, db.sessions, execution.RandomIDs()),
		scorer:   scoringapp.NewService(db.items, scoring.NormBook{}),
		tests:    db.tests,
		sessions: db.sessions,
		blobs:    blobs,
	}
}

// routes maps the delivery verbs onto the use-cases. The patterns use the Go
// 1.22 method+path router so each verb is explicit and path ids arrive through
// r.PathValue.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /items/generate", s.handleGenerate)
	mux.HandleFunc("POST /tests", s.handleCompose)
	mux.HandleFunc("GET /tests/{id}", s.handleGetTest)
	mux.HandleFunc("POST /tests/{id}/sessions", s.handleStartSession)
	mux.HandleFunc("POST /sessions/{id}/answers", s.handleAnswer)
	mux.HandleFunc("POST /sessions/{id}/complete", s.handleComplete)
	mux.HandleFunc("GET /sessions/{id}/score", s.handleScore)
	mux.HandleFunc("GET /media/{ref}", s.handleMedia)
	return mux
}

// runServer opens the TestDb and blob-store backends and serves the delivery API
// on addr until the process is stopped. Timing values in requests are expressed
// in seconds so the wire format stays clock-free; the executor enforces them
// through the injected clock.
func runServer(addr, dsn, blobsSpec string) (err error) {
	db, err := openTestDB(dsn)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := db.close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	blobs, err := openBlobStore(blobsSpec)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           newServer(db, blobs).routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "testmaker: serving delivery API on %s (testdb=%s, blobs=%s)\n", addr, dsn, blobsSpec)
	if lerr := httpSrv.ListenAndServe(); lerr != nil && !errors.Is(lerr, http.ErrServerClosed) {
		return fmt.Errorf("serve %s: %w", addr, lerr)
	}
	return nil
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
	if !decodeJSON(w, r, &req) {
		return
	}
	rep, err := s.gen.Generate(r.Context(), ports.GenerateSpec{
		TestType:   shared.TestTypeCode(req.TestType),
		Difficulty: req.Difficulty,
		Count:      req.Count,
		Seed:       req.Seed,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *server) handleCompose(w http.ResponseWriter, r *http.Request) {
	var req composeReq
	if !decodeJSON(w, r, &req) {
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
		writeError(w, err)
		return
	}
	snap, err := s.tests.GetTest(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, snap)
}

func (s *server) handleGetTest(w http.ResponseWriter, r *http.Request) {
	snap, err := s.tests.GetTest(r.Context(), testset.TestID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	test, err := s.tests.GetTest(r.Context(), testset.TestID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	d, err := s.exec.Start(r.Context(), test)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var req answerReq
	if !decodeJSON(w, r, &req) {
		return
	}
	d, err := s.exec.Answer(r.Context(), session.SessionID(r.PathValue("id")), req.ItemID, session.Answer{
		OptionID: req.OptionID,
		Numeric:  req.Numeric,
		Verdict:  req.Verdict,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) handleComplete(w http.ResponseWriter, r *http.Request) {
	snap, err := s.exec.Complete(r.Context(), session.SessionID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleScore(w http.ResponseWriter, r *http.Request) {
	snap, err := s.sessions.GetSession(r.Context(), session.SessionID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	score, err := s.scorer.Score(r.Context(), snap)
	if err != nil {
		writeError(w, err)
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
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", blob.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(blob.Bytes)
}

// --- helpers ---

// timing converts seconds on the wire into a domain testset.Timing. Seconds keep
// the JSON free of Go duration strings and clock-adjacent types.
func timing(total, perItem int) testset.Timing {
	return testset.Timing{
		Total:   time.Duration(total) * time.Second,
		PerItem: time.Duration(perItem) * time.Second,
	}
}

// decodeJSON reads the request body into dst, writing a 400 and returning false
// on malformed input so a handler can bail with a single guard.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, fmt.Errorf("%w: %s", shared.ErrInvalid, err))
		return false
	}
	return true
}

// writeJSON encodes v as the response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError maps a domain error class onto an HTTP status (falling back to 500)
// and returns the error message as JSON. It is the single translation point
// between shared.TestmakerError and the transport.
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var terr *shared.TestmakerError
	if errors.As(err, &terr) {
		switch terr.Class {
		case shared.ClassInvalid:
			status = http.StatusBadRequest
		case shared.ClassNotFound:
			status = http.StatusNotFound
		case shared.ClassConflict:
			status = http.StatusConflict
		case shared.ClassUnavailable:
			status = http.StatusServiceUnavailable
		case shared.ClassUnsupported:
			status = http.StatusNotImplemented
		}
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
