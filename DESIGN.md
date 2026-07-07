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

## 2. Item bank 🚧

**Aggregate `item.Item`** — one scored test item.

| Part | Design |
| --- | --- |
| `ID` | stable item id |
| `Provenance` | `SourceID` + origin (`fetched` / `generated` / `authored`) + inherited redistributability |
| `TestType` | A1..E2 taxonomy code (→ family) |
| `Stimulus` | ordered parts: text and/or media refs (image, SVG, matrix grid, figure) |
| `AnswerFormat` | `multiple-choice` (4–6 `Option`s) · `open-numeric` · `true-false-cannotsay` |
| `AnswerKey` | correct option id / numeric value / verdict |
| `Explanation` | shown after completion |
| `Difficulty` | integer band (1..N); optional IRT `a/b/c` params |
| `Norms` | optional item p-value / response-time baseline |

Design decisions:

- **One aggregate for every family.** Figural, numerical, verbal and speed items
  share the same shape; the difference is `TestType`, `Stimulus` media and
  `AnswerFormat`. This keeps the bank, generator and renderer uniform.
- **Media by reference.** Figural items store media references (blob keys / URLs),
  not bytes; a separate blob store adapter resolves them. Keeps the item
  aggregate small and serializable.
- **Provenance carries the license.** An item never loses the redistributability
  of its source, so export/publish paths can filter on it.
- **The taxonomy is promoted to a shared package** (`domain/shared` or
  `domain/taxonomy`) when this block lands, so `source`, `item` and `testset`
  share one definition of families and codes.

---

## 3. Test authoring 🚧

**Aggregate `testset.Test`** — a runnable assessment.

- **Sections** — ordered; each has a `Family`/mix, an item selection (explicit
  ids or a query), and timing.
- **Timing** — global limit and/or per-item limit; per-section limits (e.g. GIA
  6 min/section, adaptive Matrigma 60 s/item). Speed is modeled explicitly, not
  as an afterthought.
- **DeliveryPolicy** — `fixed-increasing` (present items in ascending difficulty)
  or `adaptive` (next item's difficulty is a function of prior correctness).
- **Composite** — a single test may combine families across sections (IST / PI
  style).

Design decision: delivery policy is data on the Test, not code in the renderer,
so the same executor runs fixed and adaptive tests by reading the policy.

---

## 4. Execution & scoring 🚧

**Aggregate `session.Session`** — one attempt, a small state machine:

```
created → in_progress → completed
                     ↘ abandoned (timeout / cancel)
```

It records navigation state, the item currently presented, captured `Response`s
(chosen option / value + **elapsed time**), and — for adaptive tests — the
difficulty path taken. All timing comes from an **injected clock** (deterministic
under test; `forbidigo` bans `time.Now`).

The **`Executor`** driving port drives the machine: `Start` builds the session
from a Test, then delivers items honoring timing and (for adaptive) selecting the
next item's difficulty from the running ability estimate; `Complete` finalizes.

**Scoring** (`scoring` context + `Scorer` port) turns a completed session into:

- **Raw score** (count / weighted correct),
- **Percentile / normal-distribution band** (from norm tables per test),
- **IQ-style scaled score** (mean 100, SD 15 by convention),
- **Per-item feedback** (correct answer + explanation).

Design decision: speed contributes to scoring where the test defines it (e.g.
number-speed, perceptual-speed families), because response time is captured per
item in the session.

---

## 5. Fetch & generation pipeline 🚧 (Fetcher stub ✅)

The `Fetcher` port pulls `RawItem`s from a source; a **router** selects the
concrete fetcher by `source.Extraction.Method` / `AccessClass`:

| Method / access | Fetcher adapter | Items arrive as |
| --- | --- | --- |
| `direct-download` | download fetcher (HTTP GET; PDF/zip) | mixed (text + page images) |
| `scrape-html` | HTML scraper | text (figural = image refs) |
| `headless-browser` | browser driver (JS/interactive) | images / interactive |
| `api` | API client (OSF, Wikimedia, HF) | mixed |
| `git-clone` / `generate` | repo runner / **Generator** | images / grids / vectors |
| `order-required` / `none` | not fetchable — catalogue only | — |

Fetched `RawItem`s are normalized into `item.Item`s (family, format, key,
difficulty, provenance) by an ingestion use-case. The **`Generator`** port is the
generate branch: rule engines (Sandia SGMT, matRiks, RAVEN-family, Bongard-LOGO)
emit unlimited items with ground-truth keys and rule metadata — the IP-free
backbone of the designer/generator subsystem. Today only `stubfetcher` exists,
wiring the boundary end-to-end.

Design decision: fetchers return a loose `RawItem` (id, stem, media refs, raw
map) rather than a validated `Item`, keeping the messy edge out of the domain;
validation happens at normalization via `item.NewItem`. When a source's raw
material is unstructured (PDF text, scraped HTML), the normalization step may
call the `LLM` port (§6) with a JSON schema to lift it into item candidates —
which then pass `item.NewItem` like any other input.

---

## 6. LLM support 🚧 <a name="6-llm-support"></a> (port + prompts + service ✅; backends 🚧)

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

**`adapters/native/llm/openaicompat` 🚧 — the buildable spec:**

- Stdlib only (`net/http`, `encoding/json`); arch component
  `adapter_llm_openaicompat` with `canUse: [_no_external_deps_]`.
- `New(cfg Config) (*Client, error)`. `Config`: `BaseURL` (required, e.g.
  `https://api.openai.com/v1` or `http://localhost:11434/v1`), `APIKey`
  (optional — local servers need none), `HTTPClient *http.Client` (optional
  override; default has a sane timeout). Constructor validates, no lazy init.
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

- Taxonomy home: `domain/shared` vs a dedicated `domain/taxonomy` package.
- Blob/media storage port shape (local FS vs S3) and item media addressing.
- IRT vs classical difficulty for the first adaptive implementation.
- Norm-table representation and where population norms are sourced/stored.
- Generator integration: shell out to external engines vs port Go rule logic.
- LLM: response caching/cost budget, prompt versioning, and an eval harness for
  derived-item quality — settle when the first real LLM step lands (Block 12).

These are recorded here so the relevant implementation block can settle them with
context rather than up front.
