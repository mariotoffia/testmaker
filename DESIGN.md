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
  only mirrored as a format. The app service exposes `Reusable()` for the safe
  set. This encodes the central IP rule of the project directly in the model.
- **Families are derived, never trusted from input.** `DeriveFamilies` maps
  test-type codes to ability families, so the two can't drift.
- **Vocabulary is closed-set.** Every enum (`AccessClass`, `LicenseCategory`,
  `ExtractionMethod`, …) validates against a fixed set; unknown values are
  rejected at ingestion, turning catalogue quality into a compile-time-ish
  guarantee.
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
validation happens at normalization via `item.NewItem`.

---

## 6. Cross-cutting design rules

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

## 7. Open design questions (resolve per block)

- Taxonomy home: `domain/shared` vs a dedicated `domain/taxonomy` package.
- Blob/media storage port shape (local FS vs S3) and item media addressing.
- IRT vs classical difficulty for the first adaptive implementation.
- Norm-table representation and where population norms are sourced/stored.
- Generator integration: shell out to external engines vs port Go rule logic.

These are recorded here so the relevant implementation block can settle them with
context rather than up front.
