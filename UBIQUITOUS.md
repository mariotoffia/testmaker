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

## Item bank (`domain/item`) 🚧

| Term | Meaning |
|---|---|
| **Item** | Aggregate root: one scored test item — stem/stimulus, answer format, key, explanation, difficulty, provenance. |
| **Stimulus** | The item's prompt: text and/or figural media (image, SVG, matrix grid), by reference. |
| **Answer format** | `multiple-choice` (4–6 options), `open-numeric`, or `true-false-cannotsay`. |
| **Answer key** | The correct response for an item. |
| **Difficulty** | Ordinal band (1..N) and optional IRT parameters. |
| **Provenance** | The item's origin: `SourceID` + `fetched`/`generated`/`authored` + inherited redistributability. |
| **RawItem** | A loose, unvalidated item pulled by a Fetcher before normalization into an `Item`. |

## Test authoring (`domain/testset`) 🚧

| Term | Meaning |
|---|---|
| **Test** | Aggregate root: a runnable assessment — ordered sections, timing, delivery policy. |
| **Section** | An ordered part of a test with its own family mix, item selection and timing. |
| **Timing** | Global and/or per-item and per-section limits. Speed is first-class. |
| **Delivery policy** | `fixed-increasing` (ascending difficulty) or `adaptive` (next difficulty depends on prior answer). |
| **Composite test** | A test combining several ability families across sections. |

## Test execution (`domain/session`) 🚧

| Term | Meaning |
|---|---|
| **Session** | Aggregate root: one attempt at a test — a state machine `created → in_progress → completed \| abandoned`. |
| **Response** | A captured answer for a delivered item, with elapsed time. |
| **Executor** | The driving port that administers a test (deliver, time, adapt, complete). |
| **Adaptive path** | The sequence of difficulties taken through an adaptive test. |

## Scoring (`domain/scoring`) 🚧

| Term | Meaning |
|---|---|
| **Score** | Value result: raw score, percentile/normal band, IQ-scaled score, per-item feedback. |
| **Band** | Percentile / normal-distribution classification of a raw score. |
| **Scaled IQ** | Raw score mapped to an IQ-style scale (mean 100, SD 15 by convention). |
| **Scorer** | The driven port that turns a completed session into a Score. |

## LLM prompts (`domain/prompt`) ✅

| Term | Meaning |
|---|---|
| **Prompt** | Aggregate root: a stored, versioned prompt template. The template is a Go `text/template` (`{{.name}}` placeholders); it must parse on construction. |
| **Purpose** | The LLM step a prompt auto-applies to — closed set: `extraction`, `translation`, `derivation`, `generation`. |
| **Render** | Fills the template with caller values; a missing placeholder is an error (`missingkey=error`), never silent. |
| **Prompt application** | The built-in first step of `GenerateFor`: look up the purpose's prompt, render it, prepend it as the system message. |
| **PromptRepository** | Driven port storing prompts. `ByPurpose` = highest `Version`, ties by smallest ID — deterministic across adapters (memory, file, sqlite, …). |

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
