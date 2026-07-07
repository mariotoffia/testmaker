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

## Block 1 — TestDb repository port interfaces ▶

**Goal:** firm up the persistence contracts that later blocks depend on:
`TestRepository` (composed tests), `ItemRepository` (bank items), and
`SessionRepository`. Settle method sets, filter objects, and the Snapshot DTOs
that cross them (today these are scaffold shells).
**Touches:** `ports/repositories.go`, the DTO shells in `domain/{item,testset,session}`.
**Depends on:** Block 0 (pattern), a first cut of the item/test/session Snapshots (Block 4/7 refine them).
**Done when:** interfaces compile, are documented, and have written conformance-suite skeletons (`ports/testdbtest`, etc.) — no adapter yet.

## Block 2 — In-memory TestDb ▶

**Goal:** `adapters/native/testdb/memorytestdb` implementing `TestRepository`
(and likely `ItemRepository`/`SessionRepository`), mirroring `memorycatalog`:
map-backed, deep-copy on read, concurrency-safe.
**Touches:** new adapter module + `ports/testdbtest` conformance suite.
**Depends on:** Block 1.
**Done when:** the conformance suite passes against the memory adapter; it becomes the default store in tests and the CLI.

## Block 3 — SQLite TestDb 🚧

**Goal:** `adapters/native/testdb/sqlitetestdb` implementing the **same**
repositories against `modernc.org/sqlite` (pure-Go, no cgo), with all driver
code isolated in `acl_*.go` files and schema migrations.
**Touches:** new adapter module (vendor `sqlite` in `.go-arch-lint.yml`).
**Depends on:** Blocks 1–2 (runs the identical conformance suite).
**Done when:** the same `Run…Tests` suite passes against sqlite `:memory:` and a file DB — proving memory and sqlite are interchangeable.

## Block 4 — Item bank domain + repository 🚧

**Goal:** model the `Item` aggregate (stimulus, options, key, difficulty,
provenance, explanation) and its invariants; promote the A1..E2 taxonomy to a
shared package; back it with the memory + sqlite `ItemRepository`.
**Touches:** `domain/item`, `domain/shared` (taxonomy), `ports.ItemRepository`, testdb adapters.
**Depends on:** Blocks 1–3.
**Done when:** items can be created (validated), stored, queried by family/type/difficulty, and carry provenance + redistributability.

## Block 5 — Fetch pipeline + ingestion 🚧

**Goal:** replace the stub with real `Fetcher` adapters routed by
`source.Extraction.Method` (direct-download, scrape-html, api first; headless
later) and an ingestion use-case that normalizes `RawItem` → `item.Item` —
optionally via an LLM extraction step (Block 12) for unstructured payloads.
**Touches:** `adapters/native/fetch/*`, `app/ingest`, `domain/item`.
**Depends on:** Blocks 0, 4.
**Done when:** at least one real source (a redistributable open dataset, e.g. OMIB) is fetched and ingested into the bank with keys and difficulty.

## Block 6 — Designer / generator 🚧

**Goal:** the procedural generator — `Generator` port + adapters wrapping/porting
rule engines (Sandia SGMT, matRiks, RAVEN-family, Bongard-LOGO) to emit items
with ground-truth keys and rule metadata; plus a manual authoring path.
**Touches:** `ports.Generator`, `adapters/native/generate/*`, `app/authoring`, `domain/item`.
**Depends on:** Block 4.
**Done when:** the generator produces valid, keyed, difficulty-tagged items for the primary figural families and stores them in the bank.

## Block 7 — Test authoring 🚧

**Goal:** the `Test` aggregate — sections, timing, delivery policy
(fixed-increasing vs adaptive), composite multi-family tests — plus an authoring
service that composes bank/generated items into tests and persists them via
`TestRepository`.
**Touches:** `domain/testset`, `app/authoring`, `ports.TestRepository`.
**Depends on:** Blocks 2–4.
**Done when:** a composite, timed, difficulty-ordered test can be authored, stored and reloaded.

## Block 8 — Renderer / executor 🚧

**Goal:** administer a test — the `Session` state machine, per-item and global
timing (injected clock), navigation, and adaptive next-item selection.
**Touches:** `domain/session`, `ports.Executor/SessionRepository`, `adapters/native/testdb/*`, an execution service.
**Depends on:** Blocks 3, 7.
**Done when:** a session can be started, driven item-by-item under timing (fixed and adaptive), and completed with responses + timings captured.

## Block 9 — Scoring & feedback 🚧

**Goal:** raw score, percentile/normal band, IQ-scaled score, and per-item
explanations; speed as a scoring dimension where the test defines it; norm-table
representation.
**Touches:** `domain/scoring`, `ports.Scorer`, a scoring service.
**Depends on:** Block 8.
**Done when:** a completed session yields a raw score, band, scaled IQ, and feedback for a normed test.

## Block 10 — Delivery surface (CLI / HTTP API) 🚧

**Goal:** expose authoring, execution and scoring over a real interface (grow the
CLI and/or add an `httpapi` module + config), wiring adapters in the composition
root.
**Touches:** `cmd/testmaker`, optional `httpapi`, config.
**Depends on:** Blocks 7–9.
**Done when:** a user can author, take and be scored on a test through the surface.

## Block 11 — Media / blob storage 🚧

**Goal:** a storage port + adapter (local FS first, S3/AWS SDK v2 later) for
figural item media referenced by `Stimulus`.
**Touches:** new `ports.BlobStore`, `adapters/native/blob/*` (and later `adapters/aws/blob/*`).
**Depends on:** Block 4.
**Done when:** figural items resolve their media through the port in the renderer.

## Block 12 — LLM library 🚧 (port + prompts + service + `openaicompat` backend ✅)

**Goal:** back the LLM stack with working adapters and the first consuming
step. Already in place: `ports.LLM`, `domain/prompt` (versioned Go-template
prompts), `ports.PromptRepository`, the hook-running `app/llm.Service`
(prompt auto-application per Purpose, BeforeGenerate/AfterGenerate hooks), and
`adapters/native/llm/openaicompat` ✅ — plain `net/http` + `encoding/json`
against the OpenAI-compatible chat API, covering cloud (OpenAI/Azure) and local
(Ollama `/v1`, vLLM, LM Studio, llama.cpp) via base-URL config, wired in the CLI
behind `TESTMAKER_LLM_*` config.
Remaining: the prompt stores `memoryprompts` (tests) + `fileprompts`
(default, `data/prompts/*.yaml`) validated by a `ports/prompttest` conformance
suite; then the LLM extraction step in `app/ingest` (structured `JSONSchema`
output). Translation and run-time derivation follow inside Blocks 5–8 as
consumers. Design rules in [DESIGN.md](DESIGN.md#6-llm-support) §6.
**Touches:** `adapters/native/llm/{openaicompat,memoryprompts,fileprompts}`, `ports/prompttest`, `app/ingest`, later an LLM-backed `Generator` adapter (optional `adapters/aws/llm/bedrock`).
**Depends on:** Block 5 (first consumer); port, prompt domain and service already in place.
**Done when:** an unstructured fetched payload is lifted into valid, provenance-tagged item candidates through a local (Ollama) and a cloud backend using the same adapter, with the prompt loaded from the file store.

---

## Cross-cutting (fold into blocks as needed)

- **Clock** (`domain/clock` + fake) — introduce with Block 8 (timing/adaptivity); `forbidigo` already bans raw `time.Now`.
- **Observability / logging** — add a `logging` port when the delivery surface (Block 10) needs it.
- **AWS SDK v2 adapters** — DynamoDB `TestDb` and S3 blob store as `adapters/aws/*` when cloud persistence is wanted (own modules, own vendor allow-list).

---

## Dependency sketch

```
0 ✅ ──► 1 ──► 2 ──► 3
             │      └─► 4 ──► 5 ──► 12
             │           ├──► 6
             │           └──► 7 ──► 8 ──► 9 ──► 10
                                     11 ◄─ 4
```

Each block is self-contained: new domain types, a port (or port refinement),
adapter(s) with a conformance suite, a use-case, and wiring — passing
`make check` before the next block starts.
