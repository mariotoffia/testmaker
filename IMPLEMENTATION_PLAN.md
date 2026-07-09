# Testmaker — Implementation Plan

Work is partitioned into independent **blocks**. Each block is a vertical or
horizontal slice small enough to design, implement, lint and test on its own;
we pick them up one at a time. This is a *map*, not a detailed spec — each block
gets its own design pass when we start it.

Each block lists: **Goal**, **Touches** (packages/ports), **Depends on**, and
**Done when**. Status: ✅ done · ▶ next-ready · 🚧 blocked/later.

Recommended order follows the dependencies; **Blocks 1–3 are the "TestDb" the
CLAUDE.md mission calls out and are the natural next step.**

---

## Block 0 — Source catalogue ✅ (done)

**Goal:** catalogue external sources and load the research catalogue.
**Touches:** `domain/source`, `ports.SourceRepository/CatalogLoader/Fetcher`, `app/catalog`, `adapters/native/source/{memorycatalog,filecatalog}`, `adapters/native/fetch/stubfetcher`, `cmd/testmaker`.
**Done:** implemented end-to-end with the 81-source seed catalogue, conformance suite, and a CLI that loads and reports. Fetcher is a stub (real fetchers = Block 5).

---

## Block 1 — TestDb repository port interfaces ✅ (done)

**Goal:** firm up the persistence contracts that later blocks depend on:
`TestRepository` (composed tests), `ItemRepository` (bank items), and
`SessionRepository`. Settle method sets, filter objects, and the Snapshot DTOs
that cross them (today these are scaffold shells).
**Touches:** `ports/repositories.go`, the DTO shells in `domain/{item,testset,session}`.
**Depends on:** Block 0 (pattern), a first cut of the item/test/session Snapshots (Block 4/7 refine them).
**Done:** the three repository interfaces are documented with per-context
sentinels (`Err{Unknown,Invalid}{Item,Test,Session}`) and proven by the
`ports/testdbtest` conformance suites. The Snapshot/Filter DTOs stay minimal
shells (they refine in Blocks 4/7); the method sets are firm.

## Block 2 — In-memory TestDb ✅ (done)

**Goal:** `adapters/native/testdb/memorytestdb` implementing `TestRepository`
(and likely `ItemRepository`/`SessionRepository`), mirroring `memorycatalog`:
map-backed, deep-copy on read, concurrency-safe.
**Touches:** new adapter module + `ports/testdbtest` conformance suite.
**Depends on:** Block 1.
**Done:** one `memorytestdb.Store` satisfies all three TestDb ports and passes
`RunTestRepositoryTests` / `RunItemRepositoryTests` / `RunSessionRepositoryTests`
under `-race`; it is the default store wired into the CLI composition root.

## Block 3 — SQLite TestDb ✅ (done)

**Goal:** `adapters/native/testdb/sqlitetestdb` implementing the **same**
repositories against `modernc.org/sqlite` (pure-Go, no cgo), with all driver
code isolated in `acl_*.go` files and schema migrations.
**Touches:** new adapter module (vendor `sqlite` in `.go-arch-lint.yml`).
**Depends on:** Blocks 1–2 (runs the identical conformance suite).
**Done:** one `sqlitetestdb.Store` (`Open(dsn)` / `Close`) satisfies all three
TestDb ports from a single database and passes `RunTestRepositoryTests` /
`RunItemRepositoryTests` / `RunSessionRepositoryTests` under `-race` against both
`:memory:` and a file DB — proving memory and sqlite are interchangeable. Driver
and SQL live in `acl_sqlite.go`; a `PRAGMA user_version` migration runner is the
upgrade path for the snapshot fields Blocks 4/7/8 add. Wired into the CLI behind
`-testdb`.

## Block 4 — Item bank domain + repository ✅

**Goal:** model the `Item` aggregate (stimulus, options, key, difficulty,
provenance, explanation) and its invariants; promote the A1..E2 taxonomy to a
shared package; back it with the memory + sqlite `ItemRepository`.
**Touches:** `domain/item`, `domain/shared` (taxonomy), `ports.ItemRepository`, testdb adapters.
**Depends on:** Blocks 1–3.
**Done when:** items can be created (validated), stored, queried by family/type/difficulty, and carry provenance + redistributability.

## Block 5 — Fetch pipeline + ingestion ✅ (`direct-download` + `app/ingest` ✅; scrape-html / api / headless / generate 🚧)

**Goal:** replace the stub with real `Fetcher` adapters routed by
`source.Extraction.Method` (direct-download, scrape-html, api first; headless
later) and an ingestion use-case that normalizes `RawItem` → `item.Item` —
optionally via an LLM extraction step (Block 12) for unstructured payloads.
**Touches:** `adapters/native/fetch/*`, `app/ingest`, `domain/item`.
**Depends on:** Blocks 0, 4.
**Done when:** at least one real source (a redistributable open dataset, e.g. OMIB) is fetched and ingested into the bank with keys and difficulty.
**Done:** `adapters/native/fetch/httpfetch` implements the `direct-download`
`Fetcher` (stdlib `net/http` + `archive/zip`, size-capped, ctx-honoured);
`app/ingest` routes a source to its fetcher and normalizes `RawItem` →
`item.Item` via a source-keyed registry. The **openpsych-viqt** public-domain
vocabulary set (45 items) is fetched from its data zip and ingested as
synonym multiple-choice items with keys (from the codebook) and difficulty
bands (p-values from the 12k-row response CSV). `go run ./cmd/testmaker
-ingest openpsych-viqt` runs it. The `scrape-html`, `api`, `headless-browser`,
`git-clone`, and `generate` branches remain 🚧 (no ingested source needs them
yet); `stubfetcher` stays as the unsupported-method fallback.

## Block 6 — Designer / generator ✅ (`rulegen` native figural generator + `app/authoring` ✅; external rule-engine adapters not needed)

**Goal:** the procedural generator — `Generator` port + an adapter that emits
items with ground-truth keys and rule metadata; plus a manual authoring path.
**Touches:** `ports.Generator`, `adapters/native/generate/rulegen`, `app/authoring`, `domain/item`.
**Depends on:** Block 4.
**Done when:** the generator produces valid, keyed, difficulty-tagged items for the primary figural families and stores them in the bank. ✅

Built as `adapters/native/generate/rulegen`, a native Go rule engine covering
the primary figural families (A1 figure-series, A2 matrix, A3 → series, A4
odd-one-out). It derives each correct answer from the same rules that build the
stimulus (keys are ground-truth by construction) and renders figures to
self-contained SVG data-URIs, so it needs no external engine and no blob store.
This resolves DESIGN open question #299 toward native rules rather than wrapping
Sandia SGMT / matRiks / RAVEN-family / Bongard-LOGO (their IP and process
overhead bought nothing the native engine did not). `app/authoring` stores a
generated batch and also exposes a manual `Author` path onto the same bank.

## Block 7 — Test authoring ✅

**Goal:** the `Test` aggregate — sections, timing, delivery policy
(fixed-increasing vs adaptive), composite multi-family tests — plus an authoring
service that composes bank/generated items into tests and persists them via
`TestRepository`.
**Touches:** `domain/testset`, `app/authoring`, `ports.TestRepository`.
**Depends on:** Blocks 2–4.
**Done when:** a composite, timed, difficulty-ordered test can be authored, stored and reloaded.

**Done:** `testset.Test` aggregate (ordered `Section`s, `Timing`, `DeliveryPolicy`,
`ItemRef` carrying the item's difficulty band, families derived from sections);
`NewTest` invariant gate; `Snapshot`/`RehydrateFromSnapshot` DTO round-trip.
`app/authoring.TestService.Compose` queries the bank per section, orders matches
by ascending difficulty (satisfying fixed-increasing), builds the test through
the gate and persists it. sqlite migration 4 stores tests as a JSON blob (the
old `(id,title)` rows are quarantined into `tests_v1_legacy`). Proven end-to-end
by the `-author-test` CLI demo (memory + sqlite) and the shared `TestRepository`
conformance suite.

## Block 8 — Renderer / executor ✅

**Goal:** administer a test — the `Session` state machine, per-item and global
timing (injected clock), navigation, and adaptive next-item selection.
**Touches:** `domain/clock`, `domain/session`, `ports.Executor/SessionRepository`, `adapters/native/testdb/*`, `app/execution`, `cmd/testmaker -run-test`.
**Depends on:** Blocks 3, 7.
**Done when:** a session can be started, driven item-by-item under timing (fixed and adaptive), and completed with responses + timings captured. ✅ — `app/execution.Service` over an injected `clock.Clock`; sessions persist as a rich JSON snapshot in both testdb backends; `-run-test` administers a fixed and an adaptive attempt end-to-end.

## Block 9 — Scoring & feedback ✅

**Goal:** raw score, percentile/normal band, IQ-scaled score, and per-item
explanations; speed as a scoring dimension where the test defines it; norm-table
representation.
**Touches:** `domain/scoring`, `ports.Scorer`, a scoring service.
**Depends on:** Block 8.
**Done when:** a completed session yields a raw score, band, scaled IQ, and feedback for a normed test. ✅
**Delivered:**
- `domain/scoring` (pure, stdlib-only): `Score`/`Speed`/`ItemFeedback`/`Outcome`
  values; `NormTable{Mean,SD}` parametric normal norm + `NormBook` by test id
  (`percentile = 100·Φ(z)`, `IQ = round(100+15·z)`, `Φ` via `math.Erfc`);
  Wechsler-style `Band` + `Classify`; `AbilityFromStaircase` reversal-mean
  estimator; sentinel `ErrNotScorable`.
- `app/scoring.Service` implements `ports.Scorer` (reclassified driven →
  **driving**, mirroring `Executor`): maps a `SessionSnapshot` onto the model,
  reads the bank for feedback, resolves the norm book. Fixed attempts norm the
  raw count; adaptive attempts norm the staircase ability.
- Wired into `cmd/testmaker -run-test` (demo norm book; prints band/IQ/percentile,
  speed and feedback count for both fixed and adaptive attempts).
**Inherited from Block 8 — resolution:**
- **Freeze the answer key** — RESOLVED for scoring: the scorer reads the *frozen*
  grades captured at administration (`session.Response.Correct`), never re-grading
  against the live bank, so a score is drift-immune; the bank is consulted only
  for feedback text, which degrades to blank if an item was deleted. Freezing the
  key into the *execution plan* (so live grading is also reproducible) remains a
  Block-10 execution-hardening concern.
- **Numeric answer tolerance** — RESOLVED: `item.AnswerKey.Tolerance` (an absolute
  epsilon, default 0 = exact) is validated for open-numeric keys and honoured by
  `app/execution.graded` (`|answer − key| ≤ tolerance`), proven by
  `TestAnswerGradesNumericWithinTolerance`. Existing keys keep exact equality.
- **Numeric answer presence** — RESOLVED by decision (no bit): `AnswerFormat` is the
  presence discriminator (0 is a valid open-numeric answer, e.g. "5 − 5"), and the
  session never records an unanswered item, so a matched zero is a real answer, not a
  skip. Documented on `item.AnswerKey` and `execution.graded`.
- **Consume adaptive delivery order** — RESOLVED: `AbilityFromStaircase` is the
  reversal-mean estimator, which consumes the *order* of correct/wrong outcomes.
  Two attempts with the same items and the same count correct but a different
  sequence get different abilities (proven by
  `TestScoreAdaptiveConsumesDeliveryOrder`), so "adaptive" is no longer cosmetic.

## Block 10 — Delivery surface (CLI / HTTP API) ✅

**Goal:** expose authoring, execution and scoring over a real interface (grow the
CLI and/or add an `httpapi` module + config), wiring adapters in the composition
root.
**Touches:** `cmd/testmaker`, optional `httpapi`, config.
**Depends on:** Blocks 7–9.
**Done when:** a user can author, take and be scored on a test through the surface. ✅
**Delivered:**
- HTTP delivery surface in `cmd/testmaker/server.go` (stdlib `net/http` only,
  Go 1.22 method+path router), reached with `testmaker -serve <addr>`. Seven
  endpoints cover the whole author → take → score path: `POST /items/generate`,
  `POST /tests`, `GET /tests/{id}`, `POST /tests/{id}/sessions`,
  `POST /sessions/{id}/answers`, `POST /sessions/{id}/complete`,
  `GET /sessions/{id}/score`.
- It lives in the composition root, **not** a new adapter module: the surface is
  the driving side of the hexagon and depends on the `app` use-cases, which no
  adapter is allowed to import. `openTestDB` is the single backend switch
  (memory default / sqlite DSN), shared by the CLI demo and the server. A
  `shared.TestmakerError` → HTTP status map (invalid→400, not_found→404,
  conflict→409, unavailable→503, unsupported→501) is the one transport
  translation point. Request timing is expressed in seconds so the wire format
  stays clock-free.
- Proven end-to-end by `cmd/testmaker/server_test.go` (`httptest`): the full
  flow returns a completed, scorable session; malformed input is 400 and an
  unknown test is 404.
**Inherited from Block 8 — resolution:**
- **Optimistic concurrency on `SessionRepository`** — RESOLVED. `SaveSession` is
  now a compare-and-swap on `SessionSnapshot.Version`: a snapshot stores only
  when its `Version` is exactly one past the stored version, otherwise the store
  returns `session.ErrSessionConflict` (`ClassConflict` → 409). The version is a
  passthrough field on the `Session` aggregate (carried through
  `Snapshot`/`RehydrateFromSnapshot`), which the executor increments at each
  persist. Two concurrent `Answer`s (or an `Answer` racing a `Complete`) on one
  session id no longer last-writer-wins or resurrect a completed attempt: the
  first writer wins and the rest get a conflict. The guard is proven for **both**
  stores by the shared `ports/testdbtest` conformance suite
  (`OptimisticConcurrency`) and end-to-end by the delivery surface's
  concurrent-answers-record-once test (under `-race`). Client-supplied
  ETags/`If-Match` are a documented upgrade path; the server derives the expected
  version from its own load today. In sqlite the swap is a guarded conditional
  write (`json_extract` on the version in the JSON blob), so a file database runs
  with WAL + `busy_timeout` + a real connection pool and the guarantee holds
  across connections and processes. Recorded as
  [ADR-0001](docs/adr/0001-optimistic-concurrency-cas-on-sessionrepository.md) /
  [ADR-0002](docs/adr/0002-sqlite-session-version-in-json-with-guarded-write.md).

## Block 11 — Media / blob storage ✅

**Goal:** a storage port + adapter (local FS first, S3/AWS SDK v2 later) for
figural item media referenced by `Stimulus`.
**Touches:** new `ports.BlobStore`, `adapters/native/blob/*` (and later `adapters/aws/blob/*`).
**Depends on:** Block 4.
**Done when:** figural items resolve their media through the port in the renderer. ✅
**Delivered:**
- `ports.BlobStore` (2 methods): `Put(Blob) (ref, err)` and `Get(ref) (Blob, err)`,
  where `Blob{Bytes, ContentType}`. The store is **content-addressed**: the ref is
  the sha256 of content-type + bytes, so identical media dedupe and the same bytes
  under a different MIME never collide. Unknown ref → `shared.ErrNotFound`; empty
  input → `shared.ErrInvalid`. It is an infrastructure port with no bounded
  context (like `ports.RawItem`), so it needs no `domain/blob` package. Every
  adapter is proven by the `ports/blobtest.RunBlobStoreTests` conformance suite.
- Two native, stdlib-only adapters, each its own module:
  `adapters/native/blob/memoryblob` (map + RWMutex + deep copy; the zero-config
  runtime default, mirroring `memorytestdb`) and `adapters/native/blob/fsblob`
  (`Open(dir)`, one file per blob as `<content-type>\n<bytes>`; the plan's "local
  FS first"). S3 is a later `adapters/aws/blob/*` behind the same port.
- **Put side (offload):** `app/authoring.Service` gained an optional `ports.BlobStore`.
  The generator (`rulegen`) still emits self-contained `data:` SVG URIs, so an item
  is viewable with no store; when a blob store is wired, `Generate`/`Author`
  offload those inline bytes to content refs before persisting (`offloadMedia`),
  keeping stored items small. A nil store keeps items inline — offload is a no-op —
  and a non-`data:` ref (an external URL or an already-offloaded ref) passes
  through untouched, so offload is idempotent.
- **Get side (renderer):** the HTTP delivery surface serves `GET /media/{ref}`,
  resolving an item's figural media ref back to bytes through the same port and
  writing them with the stored content type. `openBlobStore` is the single backend
  switch (memory default / directory → fsblob), and a `-blobs` flag selects it for
  both the server and the CLI generate demo, which resolves one generated item's
  ref through the store to prove the round-trip.
- Proven end-to-end by `cmd/testmaker/server_test.go`
  (`TestMediaEndpointRoundTrip`): generating A2 matrix items offloads their SVG to
  the store and `GET /media/{ref}` returns the bytes with `image/svg+xml` (pinned
  with `nosniff` + a sandbox CSP); an unknown ref is a 404. Offload itself is
  unit-tested in `app/authoring` against a fake store (inline media rewritten, nil
  store left inline, Put errors aborting the run, non-`data:` refs untouched).
- The load-bearing choices — content addressing, the authoring-time offload seam,
  and the hardened same-origin media serving — are recorded in
  [ADR-0003](docs/adr/0003-content-addressed-blob-store-and-media-offload.md).
  `fsblob` writes atomically (temp file + rename) so a crash can never leave a
  truncated blob under a content ref; the `/media/{ref}` route validates fsblob
  refs as 64-hex so it is not path-traversable. Accepted gaps (no read-time
  verification, no blob GC/`Delete`, refs the renderer must tell apart from inline
  `data:` URIs) are documented in the ADR.

## Block 12 — LLM library ✅

**Goal:** back the LLM stack with working adapters and the first consuming
step. Already in place: `ports.LLM`, `domain/prompt` (versioned Go-template
prompts), `ports.PromptRepository`, the hook-running `app/llm.Service`
(prompt auto-application per Purpose, BeforeGenerate/AfterGenerate hooks), and
`adapters/native/llm/openaicompat` ✅ — plain `net/http` + `encoding/json`
against the OpenAI-compatible chat API, covering cloud (OpenAI/Azure) and local
(Ollama `/v1`, vLLM, LM Studio, llama.cpp) via base-URL config, wired in the CLI
behind `TESTMAKER_LLM_*` config.
Delivered: the prompt stores `memoryprompts` (tests) + `fileprompts`
(default, `data/prompts/*.yaml`) validated by a `ports/prompttest` conformance
suite; the LLM extraction step `app/ingest.IngestLLM` (structured `JSONSchema`
output, `item.NewItem`-gated, `OriginGenerated`-tagged) seeded by
`data/prompts/extract-items.yaml` and wired in the CLI behind `-ingest-llm`.
Translation and run-time derivation follow inside Blocks 5–8 as
consumers. Design rules in [DESIGN.md](DESIGN.md#6-llm-support) §6.
**Touches:** `adapters/native/llm/{openaicompat,memoryprompts,fileprompts}`, `ports/prompttest`, `app/ingest`, later an LLM-backed `Generator` adapter (optional `adapters/aws/llm/bedrock`).
**Depends on:** Block 5 (first consumer); port, prompt domain and service already in place.
**Done when:** an unstructured fetched payload is lifted into valid, provenance-tagged item candidates through a local (Ollama) and a cloud backend using the same adapter, with the prompt loaded from the file store.

---

## Cross-cutting (fold into blocks as needed)

- **Clock** (`domain/clock` + fake) — introduced with Block 8 ✅ (timing/adaptivity); `forbidigo` bans raw `time.Now`, so `System()` is the sanctioned real reading and `Fake` drives tests deterministically.
- **Observability / logging** — add a `logging` port when the delivery surface (Block 10) needs it.
- **AWS SDK v2 adapters** — DynamoDB `TestDb` and S3 blob store as `adapters/aws/*` when cloud persistence is wanted (own modules, own vendor allow-list).

---

## Dependency sketch

```
0 ✅ ──► 1 ✅ ──► 2 ✅ ──► 3 ✅
              │      └─► 4 ──► 5 ──► 12
              │           ├──► 6
              │           └──► 7 ──► 8 ──► 9 ──► 10
                                     11 ◄─ 4
```

Each block is self-contained: new domain types, a port (or port refinement),
adapter(s) with a conformance suite, a use-case, and wiring — passing
`make check` before the next block starts.
