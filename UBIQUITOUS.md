# Testmaker — Ubiquitous Language

Authoritative glossary. One term per row. If a concept appears in code, config,
logs or docs, it must use **this** name and **this** meaning. Grouped by bounded
context. Terms marked 🚧 are designed but not yet implemented.

---

## Shared kernel (`domain/shared`)

| Term | Meaning |
|---|---|
| **TestmakerError** | The single structured domain error: `Code`, `Class`, `Message`, `Cause`, `Context`. Matched by `Code` (so `errors.Is` works against sentinels). Builders copy-on-write. |
| **Sentinel** | A package-level `TestmakerError` (e.g. `ErrUnknownSource`) declared beside the model it belongs to; compared via `errors.Is`. |
| **Class** | How a caller should react to an error: `invalid`, `not_found`, `conflict`, `unavailable`, `unsupported`. |
| **Ability family** | Top-level cognitive family: `logical`, `numerical`, `verbal`, `spatial`, `speed`. |
| **Test-type code** | Fine-grained item type `A1..E2` from the taxonomy; its leading letter selects the ability family. |

## Source catalogue (`domain/source`)

| Term | Meaning |
|---|---|
| **Source** | Aggregate root: a catalogued place cognitive-test items can be obtained from (open dataset, generator repo, downloadable PDF, scrapable site, or a vendor whose format may be mirrored). |
| **Snapshot** | The dependency-neutral DTO of a `Source` that crosses ports; never the aggregate itself. |
| **Access class** | How the source is reached: `downloadable-artifact`, `site-scrape`, `dataset-repo`, `api`, `interactive-only`, `generator`. |
| **License** | Value object `{Category, Detail, Redistributable}` describing reuse terms. |
| **Redistributable** | The reuse gate: `yes` (ship items), `conditional` (strings attached), `no` (format-reference only). The single most load-bearing source field. |
| **Extraction** | Value object `{Method, Auth, ItemsAs, Notes}` telling the Fetcher how to obtain items. |
| **Extraction method** | Concrete fetch mechanism: `direct-download`, `scrape-html`, `headless-browser`, `git-clone`, `api`, `generate`, `order-required`, `none` (empty input normalizes to `none`). |
| **Items as** | The shape fetched items arrive in — `grids`, `images`, `interactive`, `mixed`, `text`, `vectors` — routing them to the right normalization path. |
| **Generator (source)** | A source whose `Generator` flag is true: emits unlimited items + documented rules; IP-free backbone of the designer. |
| **Priority / IP risk / Category** | Curation metadata: value for a logic-first bank; verbatim-reuse risk; catalogue grouping (`open-data`, `ml-dataset`, `branded-vendor`, …). |

## Item bank (`domain/item`)

| Term | Meaning |
|---|---|
| **Item** | Aggregate root: one scored test item — stem/stimulus, answer format, key, explanation, difficulty, provenance. |
| **Stimulus** | The item's prompt: an ordered set of parts, each text and/or a figural media reference (image, SVG, matrix grid, figure). |
| **Answer format** | `multiple-choice` (4–6 options), `open-numeric`, or `true-false-cannotsay`. |
| **Answer key** | The correct response, interpreted by answer format: option id, numeric value, or verdict. |
| **Difficulty** | Ordinal band (1..N). IRT parameters deferred to adaptive delivery. |
| **Provenance** | The item's origin: `SourceID` + `fetched`/`generated`/`authored` + inherited redistributability. |
| **Item filter** | Query criteria over the bank: ability family, test type, origin, redistributability, difficulty range. |
| **RawItem** | A loose, unvalidated item pulled by a Fetcher before normalization into an `Item`. |
| **Media ref** | A `Stimulus`/`Option` `MediaRef`: the key that resolves a figural part's bytes. Inline (`data:` URI, self-contained) or a **blob ref** once offloaded; the item shape (`MediaKind` + `MediaRef`) is the same either way. |
| **Blob store** | Infrastructure port `ports.BlobStore` (`Put`/`Get` over a `Blob{Bytes, ContentType}`) that holds figural media out of the item aggregate. **Content-addressed**: the ref is the sha256 of content-type + bytes, so identical media dedupe. Native `memoryblob` (default) and `fsblob` back it; the renderer resolves refs via `GET /media/{ref}`. |

## Test authoring (`domain/testset`)

| Term | Meaning |
|---|---|
| **Test** | Aggregate root: a runnable assessment — ordered sections, timing, delivery policy. |
| **Section** | An ordered part of a test with one ability family, its item references and timing. |
| **Item reference** | A bank item placed in a section: its id plus the difficulty band (so a section can order and validate without the item context). |
| **Timing** | Global and/or per-item and per-section limits. Speed is first-class. |
| **Delivery policy** | `fixed-increasing` (ascending difficulty) or `adaptive` (next difficulty depends on prior answer). |
| **Composite test** | A test combining several ability families across sections. |

## Test execution (`domain/session`, `domain/clock`, `app/execution`) ✅

| Term | Meaning |
|---|---|
| **Session** | Aggregate root: one attempt at a test — a clock-free state machine `created → in-progress → completed \| abandoned`. |
| **Plan** | The session's own copy of the test structure it runs: `PlanSection`s of `PlanItem`s (plain-string item ids + difficulty), snapshotted at start so a later test edit never mutates a running attempt. |
| **Presented** | The item currently in front of the taker (id, difficulty, section, delivered-at); an empty item id means none is presented. |
| **Response** | A captured answer for the presented item, with elapsed time and graded correctness. |
| **Answer** | The taker's raw answer, interpreted by the item's format: OptionID (MC), Numeric (open), Verdict (T/F/cannot-say). |
| **Executor** | The driving port (`app/execution.Service`) that administers a test: `Start`, `Answer` (grade + advance), `Complete`. |
| **Delivery** | What the executor returns per step: the session snapshot, the presented item's content, and the advisory per-item deadline. |
| **Clock** | `domain/clock.Clock` — the injected time source (`System()` in production, `Fake` in tests); the aggregate never reads the wall clock itself. |
| **Global deadline** | `startedAt + total budget`; the executor abandons an attempt once `now` passes it. |
| **Adaptive path** | The sequence of difficulties taken through an adaptive test: a classical up/down staircase (climb on correct, descend on wrong). |
| **Version** | The session snapshot's optimistic-concurrency token: a passthrough field on the aggregate, incremented by the executor at each persist. `SaveSession` stores only when it is exactly one past the stored version (a compare-and-swap). |
| **Session conflict** | `session.ErrSessionConflict` (`conflict` class) — a `SaveSession` whose `Version` lost the compare-and-swap race; the losing concurrent `Answer`/`Complete` is rejected instead of clobbering the winner. |

## Scoring (`domain/scoring`) ✅

| Term | Meaning |
|---|---|
| **Score** | Value result: raw score, adaptive ability, percentile/normal band, IQ-scaled score, speed, per-item feedback. Carries no identity. |
| **Norm table** | A test's parametric normal norm (`Mean`, `SD`) of its scored dimension; maps a raw/ability value to a percentile and an IQ. |
| **Norm book** | A deployment's map of test id → norm table, supplied at the composition root. A test with no entry scores raw-only. |
| **Band** | Qualitative classification of a scaled IQ (Wechsler-style: extremely-low … very-superior; `unnormed` when no norm applied). |
| **Scaled IQ** | The scored dimension mapped to an IQ-style scale (mean 100, SD 15): `round(100 + 15·z)`. |
| **Ability** | The adaptive scored dimension in difficulty-band units: the mean band at the staircase **reversal** points (the transformed up/down estimate). Consumes the delivery order. |
| **Speed** | The response-time scoring dimension (total, mean per item, correct-per-minute); reported, not folded into the scaled IQ. |
| **Item feedback** | Per-item post-completion explanation: the taker's answer, the correct answer, and why. |
| **Scorer** | The driving port (`app/scoring.Service`) that turns a completed session into a Score. |

## LLM prompts (`domain/prompt`) ✅

| Term | Meaning |
|---|---|
| **Prompt** | Aggregate root: a stored, versioned prompt template. The template is a Go `text/template` (`{{.name}}` placeholders); it must parse on construction. |
| **Purpose** | The LLM step a prompt auto-applies to — closed set: `extraction`, `translation`, `derivation`, `generation`. |
| **Render** | Fills the template with caller values; a missing placeholder is an error (`missingkey=error`), never silent. |
| **Prompt application** | The built-in first step of `GenerateFor`: look up the purpose's prompt, render it, prepend it as the system message. |
| **PromptRepository** | Driven port storing prompts. `ByPurpose` = highest `Version`, ties by smallest ID — deterministic across adapters (memory, file, sqlite, …). |

## Delivery & access control (`cmd/testmaker`)

Access control, jobs and the web app are **delivery-surface concepts**: they
live entirely in the composition root and never appear in `domain`, `ports` or
`app` (see [DDD.md §1](DDD.md), [ADR-0006](docs/adr/0006-operator-token-and-hmac-capability-tokens.md)).

| Term | Meaning |
|---|---|
| **Operator** | The authoring/administration principal: full API access — catalogue, ingest, the un-redacted item bank (answer keys), generation, composition, invites, jobs. |
| **Taker** | The principal sitting a test: may preview/start a session from an invite and drive only that session (answer, complete, read its score). |
| **Role** | The authorization level the auth middleware resolves from a request's bearer token: `operator`, `taker`, or anonymous. |
| **Operator token** | The static bearer credential for the operator role (`auth.operatorToken`), generated into the config file on first run. |
| **Auth secret** | The server's HMAC-SHA256 key (`auth.secret`, generated on first run) that signs invite and session tokens. |
| **Invite** | A signed, expiring capability an operator mints for one test (`POST /api/tests/{id}/invites`): grants preview + start-session for that test id until expiry. Stateless, so not single-use. |
| **Session token** | The HMAC capability returned when a session starts; authorizes exactly that session's verbs. The operator token is also accepted on session verbs. |
| **Auth mode** | `token` (default — roles enforced) or `none` (explicit opt-out for trusted-localhost development and tests). |
| **Console** (operator console) | The operator face of the web app: catalogue browser, ingest with job progress, item-bank browser/preview, generate, compose, invites. |
| **Player** (test player) | The taker face of the web app: invite preview → timed one-item-at-a-time administration (global + per-item countdowns, keyboard-first) → score report with per-item feedback. |
| **Web app** (`webui`) | The embedded React SPA: source in `web/`, built into `cmd/testmaker/webui/dist`, embedded via `go:embed`, served by the delivery surface with an SPA fallback. Absent a build, the server falls back to the JSON index. |
| **API prefix** | Every JSON endpoint lives under `/api`; everything else falls through to the SPA handler. Middleware (auth, rate limit) targets the prefix. |
| **Job** | One asynchronous ingest run tracked at the delivery surface: `queued → running → done \| failed`, with the run's `ingest.Report` or error. In-memory, bounded, lost on restart by design ([ADR-0007](docs/adr/0007-async-ingest-jobs-in-memory-at-delivery-surface.md)). |
| **Ingest semaphore** | The shared concurrency gate (`limits.maxConcurrentIngests`) over sync and async ingest runs; bounds outbound fetches and LLM spend. |
| **Page envelope** | The paginated collection response `{items, total, limit, offset}` returned by the list endpoints (`/api/items`, `/api/sources`, `/api/tests`, `/api/jobs`). |

## Cross-cutting

| Term | Meaning |
|---|---|
| **Port** | An interface in `ports/` — the hexagon boundary. **Driven** = core calls out; **driving** = drives the core. |
| **Adapter** | A concrete port implementation at the edge; one Go module + one arch-lint component per technology. |
| **LLM (port)** | The one driven port for language-model completion (`ports.LLM.Generate`). Used by extraction, translation and derivation steps; backends (OpenAI-compatible HTTP, Ollama, Bedrock) are adapters. |
| **LLM service** | `app/llm.Service` — wraps a backend + `PromptRepository`; applies the stored prompt per Purpose and runs hooks around every call. Satisfies `ports.LLM` itself. |
| **Hook** | A function the composition root registers on the LLM service: **BeforeGenerate** (mutate the request — model defaults, caps, redaction) or **AfterGenerate** (inspect/mutate the `Result` — provenance, validation, metering). Runs in registration order; an error aborts the call. |
| **Result (LLM)** | `LLMResponse` + `PromptID`/`PromptVersion` provenance of the applied prompt. |
| **Effort** | Backend-neutral reasoning-effort hint on an `LLMRequest` (`low` / `medium` / `high`); adapters map or ignore it. |
| **LLM-derived item** | An item produced or translated by an LLM. Untrusted until it passes `item.NewItem`; carries provenance (model, prompt hash) and counts as unnormed until calibrated. |
| **Conformance suite** | A reusable `Run…Tests(t, …)` function that every adapter for a port runs, guaranteeing behavioural parity. |
| **Composition root** | `cmd/testmaker` — the only place adapters are chosen and wired. |
| **Delivery surface** | The HTTP API (`cmd/testmaker -serve`, stdlib `net/http`) that exposes the authoring, execution and scoring use-cases so a user can author, take and be scored on a test. Lives in the composition root (it drives `app`, which no adapter may import), not a separate adapter module. |
