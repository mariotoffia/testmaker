# Testmaker — Design Specification

Component- and model-level design. [ARCHITECTURE.md](ARCHITECTURE.md) covers the
ring structure and boundaries; this document covers **what each model holds, how
the flows work, and the design decisions** behind them. Nothing here is an
implementation; it is the spec the implementation blocks build against.

Legend: ✅ implemented · 🚧 designed, scaffold only.

---

## 1. Source catalogue ✅

**Aggregate `source.Source`** — a catalogued place items come from. Validated on
construction (`NewSource`), crosses ports as `source.Snapshot`.

Fields: `ID`, `Name`, `Provider`, `URLs`, `AccessClasses`, `Formats`,
`License{Category, Detail, Redistributable}`, `TestTypes` (A1..E2),
`Families` (derived), `ItemCount`, `AnswerKeys`, `NormsDifficulty`, `Languages`,
`Extraction{Method, Auth, ItemsAs, Notes}`, `Generator`, `Priority`, `IPRisk`,
`Category`, `Notes`.

Design decisions:

- **Redistributability is the load-bearing field.** `License.Redistributable`
  (`yes` / `conditional` / `no`) gates whether a source's items may be reused or
  only mirrored as a format. The app service splits the sets: `Reusable()` is
  `yes` only (ingest as-is); `Conditional()` carries license terms (share-alike,
  attribution) that ingestion must record and honour per source. This encodes
  the central IP rule of the project directly in the model.
- **Families are derived, never trusted from input.** `DeriveFamilies` maps
  test-type codes to ability families, so the two can't drift.
- **Vocabulary is closed-set.** Every enum (`AccessClass`, `LicenseCategory`,
  `ExtractionMethod`, `ItemsAs`, …) validates against a fixed set; unknown
  values are rejected at ingestion, turning catalogue quality into a
  compile-time-ish guarantee.
- **`Extraction.Method` + `ItemsAs` drive fetch routing** (§5): they say *how* a
  Fetcher reaches the source and whether items arrive as text, images, grids,
  vectors or need a browser.

Seed data: the 81-source research catalogue at `data/catalog/sources.json`
(schema documented in `../Testmaker/catalog` research output).

---

## 2. Item bank ✅

**Aggregate `item.Item`** — one scored test item.

| Part | Design |
| --- | --- |
| `ID` | stable item id |
| `Provenance` | `SourceID` + origin (`fetched` / `generated` / `authored`) + inherited redistributability |
| `TestType` | A1..E2 taxonomy code (→ family) |
| `Stimulus` | ordered parts: text and/or media refs (image, SVG, matrix grid, figure) |
| `AnswerFormat` | `multiple-choice` (4–6 `Option`s) · `open-numeric` · `true-false-cannotsay` |
| `AnswerKey` | correct option id / numeric value (+ optional grading `Tolerance`) / verdict |
| `Explanation` | shown after completion |
| `Difficulty` | integer band (1..N); IRT `a/b/c` params deferred to adaptive delivery (Block 8) |
| `Norms` | item p-value / response-time baseline deferred to scoring (Block 9) |

Design decisions:

- **One aggregate for every family.** Figural, numerical, verbal and speed items
  share the same shape; the difference is `TestType`, `Stimulus` media and
  `AnswerFormat`. This keeps the bank, generator and renderer uniform.
- **Media by reference.** Figural items store media references (blob keys / URLs),
  not bytes; a separate blob store adapter resolves them. Keeps the item
  aggregate small and serializable.
- **Provenance carries the license.** An item never loses the redistributability
  of its source, so export/publish paths can filter on it.
- **The taxonomy is promoted to a shared package.** It now lives in
  `domain/shared` (`AbilityFamily`, `TestTypeCode`, `DeriveFamilies`, and the
  inherited `Redistributable`), so `source`, `item` and `testset` share one
  definition of families and codes; `domain/source` keeps type aliases so its
  public API is unchanged.

---

## 3. Test authoring ✅

**Aggregate `testset.Test`** — a runnable assessment.

- **Sections** — ordered; each has a `Family`, an item selection (`ItemRef`s that
  carry the item id and its difficulty band), and timing.
- **Timing** — global limit and/or per-item limit; per-section limits (e.g. GIA
  6 min/section, adaptive Matrigma 60 s/item). Speed is modeled explicitly, not
  as an afterthought.
- **DeliveryPolicy** — `fixed-increasing` (present items in ascending difficulty)
  or `adaptive` (next item's difficulty is a function of prior correctness; an
  adaptive section must span at least two difficulty bands so it has room to
  adapt).
- **Composite** — a single test may combine families across sections (IST / PI
  style).

Design decision: delivery policy is data on the Test, not code in the renderer,
so the same executor runs fixed and adaptive tests by reading the policy.

The `app/authoring.TestService` composes a test from bank items: per section it
queries the bank (`ItemRepository`) by family/difficulty, orders matches by
ascending difficulty band and builds the `Test` through `NewTest` before
persisting the snapshot via `TestRepository`. A fixed-increasing test therefore
satisfies its non-decreasing-difficulty invariant by construction.

---

## 4. Execution & scoring — execution ✅ · scoring ✅

**Aggregate `session.Session`** ✅ — one attempt, a small clock-free state machine:

```
created → in-progress → completed
                     ↘ abandoned (timeout / cancel)
```

It records navigation state, the item currently presented, captured `Response`s
(chosen option / value + **elapsed time** + graded correctness), and — for
adaptive tests — the difficulty path taken. The aggregate holds no clock: the
executor owns time and passes a `now time.Time` into every transition
(`Begin`/`Record`/`Complete`/`Abandon`), so an attempt is deterministic under
test. The real reading comes from **`domain/clock.System()`** (`forbidigo` bans
raw `time.Now`); `clock.Fake` drives the tests.

The **`Executor`** driving port (`app/execution.Service`) drives the machine:
`Start` builds the session from a Test snapshot and presents the first item;
`Answer` grades the taker's answer against the item's key, records it and
presents the next item — for adaptive tests, the undelivered item whose band is
closest to a running target that climbs one band per correct answer and descends
one per wrong (a classical up/down staircase; IRT selection is deferred to
scoring). It abandons the attempt when the global budget is exhausted.
`Complete` finalizes. Attempt state lives only in the persisted
`SessionSnapshot` (a rich JSON blob in both testdb backends), so the service is
stateless and resumable. Grading is by answer format: option-id or verdict
equality, and for open-numeric `|answer − key| ≤ AnswerKey.Tolerance` (an
absolute epsilon, default 0 = exact); answer *presence* needs no flag because an
unanswered item is never recorded and `AnswerFormat` is the key's presence
discriminator.

**Scoring** (`scoring` context + `Scorer` **driving** port, backed by
`app/scoring.Service`) ✅ turns a completed session into:

- **Raw score** — the count of correct responses, read from the **frozen** grades
  captured at administration (`Response.Correct`), never re-graded against the
  live bank, so a score is reproducible and immune to later bank drift/deletion.
  The tradeoff is deliberate: a grade fixed at administration is *not* retroactively
  correctable — fixing a bad answer key means re-administering, not re-scoring — in
  exchange for a score that never silently changes under a taker after the fact.
  An attempt that answered nothing (a completed session with zero responses) has
  no data to norm and is rejected with `ErrNotScorable` rather than stamped with a
  confident low IQ. The raw denominator is the *answered* count (a power-test
  convention: unanswered = wrong is not assumed); a norm-derived score therefore
  assumes a full administration, which the executor always produces — it answers
  every planned item, so answered == planned on the normal path.
- **Percentile / normal-distribution band** — from a per-test **norm table**, a
  parametric normal model (`NormTable{Mean, SD}` of the scored dimension). The
  `NormBook` (test id → table) is provided at the composition root. A test with
  no norm scores raw-only (`Normed == false`, `Band == unnormed`).
- **IQ-style scaled score** (mean 100, SD 15): `IQ = round(100 + 15·z)`,
  `percentile = 100·Φ(z)` with `Φ` via `math.Erfc`.
- **Per-item feedback** (correct answer + explanation), read from the item bank
  in delivery order. An item removed since the attempt is not an error — the
  frozen grade already scored it, so its feedback text just degrades to blank.

The **scored dimension** is the raw count for a fixed-increasing attempt and the
**staircase ability estimate** for an adaptive one, so an adaptive score reflects
the delivery *path* taken, not just the count correct. Ability is the classical
transformed up/down estimator — the mean difficulty band at the **reversal
points** (direction changes correct↔wrong), which consumes the delivery order.

`Scorer` is a **driving** port (not driven): like `Executor` it orchestrates a
driven port — it reads the item bank to render feedback and resolves the norm
book — so it is a use-case, not a pure adapter. The psychometric math lives in
`domain/scoring`; the service only maps a `SessionSnapshot` onto that model.

Design decision: speed is reported as a first-class dimension (`Speed{Total,
Mean, CorrectPerMinute}`, exercised end-to-end by
`TestScoreFixedNormedWithFeedback`) but is not folded into the scaled IQ — a
speed-weighted composite needs a per-family speed norm no test carries yet.

The reported IQ and percentile are **clamped** to a defensible range
(`[40, 160]` and `[0.1, 99.9]`): a thin parametric norm extrapolated past ~±4 SD
produces figures ("IQ 210", "percentile 100.0") no fixed-form test can support,
so the tails are pinned rather than reported literally.

---

## 5. Fetch & generation pipeline 🚧 (`direct-download` fetcher + `app/ingest` ✅; `generate` via `rulegen` + `app/authoring` ✅)

The `Fetcher` port pulls `RawItem`s from a source; a **router** selects the
concrete fetcher by `source.Extraction.Method` / `AccessClass`:

| Method / access | Fetcher adapter | Items arrive as |
| --- | --- | --- |
| `direct-download` | `adapters/native/fetch/httpfetch` (HTTP GET; zip/text) ✅ | one `RawItem` per file/zip member (text inlined, binary as media ref) |
| `scrape-html` | HTML scraper 🚧 | text (figural = image refs) |
| `headless-browser` | browser driver (JS/interactive) 🚧 | images / interactive |
| `api` | API client (OSF, Wikimedia, HF) 🚧 | mixed |
| `git-clone` / `generate` | repo runner / **Generator** ✅ (`generate` via `rulegen`) | images / grids / vectors |
| `order-required` / `none` | not fetchable — catalogue only | — |

Fetched `RawItem`s are normalized into `item.Item`s (family, format, key,
difficulty, provenance) by the **`app/ingest`** use-case: it routes a source
`Snapshot` to the first `Fetcher` that `Supports` it, then hands the raw
material to a source-keyed **Normalizer** that emits `item.NewItem` specs.
The **openpsych-viqt** normalizer is the first real one — it parses the
codebook for keys and the response CSV for p-value difficulty bands, turning a
"pick the 2 synonyms among 5 words" vocabulary set into 4-option synonym
multiple-choice items. The **`Generator`** port is the generate branch, now
implemented by **`adapters/native/generate/rulegen`** ✅: a native Go rule
engine that emits figural items on demand (A1 figure-series, A2 matrix, A3 →
series, A4 odd-one-out) with ground-truth keys derived from the same rules that
build each stimulus, an honest effective difficulty band, and rule metadata in
the item `Explanation`. Figures render to self-contained SVG data-URIs — a
deliberate temporary bridge so a generated item needs no external engine and no
blob store; when the Block 11 blob store lands, the composition root swaps the
data-URI for a blob key and the item shape (`MediaKind` + `MediaRef`) is
unchanged. This resolves open question #299 toward native rules rather than
shelling out to Sandia SGMT / matRiks / RAVEN-family / Bongard-LOGO. The
**`app/authoring`** use-case stores a generated batch and also exposes a manual
`Author` path onto the same item bank.

Design decision: fetchers return a loose `RawItem` (id, stem, media refs, raw
map) rather than a validated `Item`, keeping the messy edge out of the domain;
validation happens at normalization via `item.NewItem`. When a source's raw
material is unstructured (PDF text, scraped HTML), the normalization step may
call the `LLM` port (§6) with a JSON schema to lift it into item candidates —
which then pass `item.NewItem` like any other input.

---

## 6. LLM support 🚧 <a name="6-llm-support"></a> (port + prompts + service ✅; `openaicompat` backend ✅; prompt stores 🚧)

Three pieces, innermost-out:

1. **`ports.LLM`** ✅ — the backend boundary. One method:

   ```go
   type LLM interface {
       Generate(ctx context.Context, req LLMRequest) (LLMResponse, error)
   }
   ```

   `LLMRequest` carries the per-call knobs — `Model`, `Messages`, `MaxTokens`,
   `ContextLength`, `Temperature`, `Effort` (low/medium/high), and an optional
   `JSONSchema` for structured output. Zero values mean backend defaults; hints
   a backend cannot honour are ignored, never an error.

2. **`domain/prompt` + `ports.PromptRepository`** ✅ — prompts are data, not
   string literals in code. `prompt.Prompt{ID, Version, Purpose, Template,
   Params, Notes}` is a validated aggregate: the `Template` is a **Go
   `text/template`** (`{{.name}}` placeholders) that must parse on
   construction; `Render(values)` fails on missing placeholders
   (`missingkey=error`). `Purpose` is the closed set of auto-apply steps:
   `extraction`, `translation`, `derivation`, `generation` — a new purpose
   arrives with the block that consumes it. The repository resolves
   `ByPurpose` deterministically (highest `Version`, ties by smallest ID).

3. **`app/llm.Service`** ✅ — the library every step receives. It wraps the
   backend + prompt store and runs hooks around every call. It satisfies
   `ports.LLM` itself, so port-typed consumers get the full behaviour
   transparently.

### Hook points

| Hook point | Signature | When | Typical use |
| --- | --- | --- | --- |
| **Prompt application** | built into `GenerateFor(purpose, values, req)` | first — looks up `ByPurpose`, renders, prepends as system message | per-step system prompts, versioned + provenance-tracked |
| **BeforeGenerate** | `func(ctx, *LLMRequest) error` | before the backend, registration order | per-purpose model defaults, token/cost caps, PII redaction |
| **AfterGenerate** | `func(ctx, req, *Result) error` | after the backend, registration order | provenance recording (prompt id/version, model, tokens), JSON-shape validation, usage metering, cache write |

Order: prompt application → BeforeGenerate hooks (they see the final request)
→ backend → AfterGenerate hooks. Any hook error aborts the call; error policy
(retry, fallback to bank) stays with the caller. Hooks are registered **only
in the composition root** via functional options
(`llm.WithBeforeGenerate/WithAfterGenerate`); steps never register their own.
`Result` = `LLMResponse` + `PromptID`/`PromptVersion`, so provenance is
available to after-hooks and callers without a second lookup.

### Prompt persistence tiers

| Adapter | Backing | Use |
| --- | --- | --- |
| `adapters/native/llm/memoryprompts` 🚧 | in-memory map | tests + conformance baseline |
| `adapters/native/llm/fileprompts` 🚧 | one YAML per prompt under `data/prompts/` (`id`, `version`, `purpose`, `params`, `template`, `notes`); read/write | the default store — prompts are reviewable, diffable seed data |
| sqlite (with Block 3 TestDb) 🚧 | table in the same database file | single-file deployments |
| `adapters/aws/llm/*` 🚧 | DynamoDB via AWS SDK v2 | cloud persistence, if/when wanted |

Both first adapters are validated by one `ports/prompttest` conformance suite
(the memorycatalog/filecatalog pattern). Response **caching** is a separate
later concern (§8), not a persistence tier.

### Backends

One OpenAI-compatible HTTP adapter covers cloud (OpenAI, Azure)
and local (Ollama `/v1`, vLLM, LM Studio, llama.cpp server) — same wire API,
different base URL/key, chosen in the composition root. Optional later:
`adapters/aws/llm/bedrock` (AWS SDK v2) and a native Ollama adapter only if
model-management APIs are needed.

**`adapters/native/llm/openaicompat` ✅ — the buildable spec:**

- Stdlib only (`net/http`, `encoding/json`); arch component
  `adapter_llm_openaicompat` with `canUse: [_no_external_deps_]`.
- `New(cfg Config) (*Client, error)`. `Config`: `BaseURL` (required, e.g.
  `https://api.openai.com/v1` or `http://localhost:11434/v1`), `APIKey`
  (optional — local servers need none), `AuthScheme` (optional — zero value
  sends `Authorization: Bearer <key>`; `AuthSchemeAPIKey` sends Azure's
  `api-key: <key>` header), `HTTPClient *http.Client` (optional override;
  default has a sane timeout). Constructor validates, no lazy init.
- Request mapping to `POST {BaseURL}/chat/completions`: `Model`, `Messages`
  (roles as-is), `MaxTokens`, `Temperature` map directly, zero values
  omitted from the wire; `JSONSchema` → `response_format:
  {"type":"json_schema", …}`; `Effort` → `reasoning_effort`;
  `ContextLength` has no wire field — ignored silently (the port contract:
  hints are best-effort, never an error).
- Response mapping: first choice's message content; `model` as served;
  `usage` token counts, `0` when the backend omits usage.
- Errors: non-2xx and malformed bodies wrap into the adapter's
  `shared.TestmakerError` sentinels (matched by `Code` via `errors.Is`);
  response bodies read via `io.LimitReader` and always closed;
  `context.Context` cancellation honoured end-to-end.
- Wired in `cmd/testmaker` behind config — absent LLM config means the step
  is skipped, the CLI still runs.

Design rules:

- **LLM output is untrusted input.** Anything generated must pass the domain
  constructors (`item.NewItem`, key present, difficulty tagged) before it
  reaches a bank or an examinee; derivation failures fall back to the item
  bank — a session never blocks on a model.
- **Provenance is recorded.** LLM-produced/translated items carry origin
  metadata (model, prompt id + version) so psychometric calibration can treat
  them as unnormed until validated.
- **Determinism in tests.** Unit tests use a fake `LLM` (canned responses);
  real backends are integration-only, consistent with the no-network rule in
  [TESTS.md](TESTS.md).
- **Injection, not construction.** Only `cmd/` builds the service and its
  backend; steps receive `ports.LLM` (usually the service). An adapter needing
  LLM help (e.g. the derivation generator) takes the port in its constructor —
  sibling adapters still never import each other; they meet only at the port.

---

## 7. Cross-cutting design rules

- **Snapshots at boundaries.** Aggregates never cross a port; a `Snapshot` DTO
  does. Adapters store/return deep copies so internal state can't leak.
- **Constructors validate; rehydration trusts.** `New…` enforces invariants and
  returns `*shared.TestmakerError`; `RehydrateFromSnapshot` skips validation for
  data already known-good.
- **Determinism.** Randomness (generation, item order) and time (timing,
  adaptivity) are injected, so tests are reproducible.
- **Conformance suites define behaviour.** A repository's contract is the
  `…test.Run…Tests` suite, run against every adapter (see [TESTS.md](TESTS.md)).

---

## 8. Open design questions (resolve per block)

- Taxonomy home: **resolved (Block 4)** — promoted to `domain/shared`, not a
  dedicated `domain/taxonomy` package.
- Blob/media storage port shape (local FS vs S3) and item media addressing.
- IRT vs classical difficulty for the first adaptive implementation —
  **resolved (Block 9)**: classical staircase for both delivery (Block 8) and
  scoring. The adaptive ability estimate is the transformed up/down (reversal-mean)
  estimator over `Difficulty.Band`; IRT/MLE theta is the upgrade path once the
  bank is calibrated with item parameters.
- Norm-table representation and where population norms are sourced/stored —
  **resolved (Block 9)**: a parametric normal model `NormTable{Mean, SD}` of the
  scored dimension, keyed per test in a `NormBook` provided at the composition
  root. Percentile = `100·Φ(z)`, IQ = `round(100 + 15·z)`; no per-point table is
  needed. Durable norm persistence (a repository/adapter) and empirical/piecewise
  tables are deferred until a test ships published norms.
- Generator integration: shell out to external engines vs port Go rule logic —
  **resolved (Block 6)**: native Go rule logic (`adapters/native/generate/rulegen`).
  No external engine earned its IP and process overhead for the figural families;
  keys are ground-truth by construction and figures render to SVG data-URIs.
- LLM: response caching/cost budget, prompt versioning, and an eval harness for
  derived-item quality — settle when the first real LLM step lands (Block 12).

These are recorded here so the relevant implementation block can settle them with
context rather than up front.
