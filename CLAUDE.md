# Testmaker

## Mission

Build a library of cognitive aptitude / IQ tests — the kind testttalent.com ("Test The Talent") sells — by (a) gathering items from public-domain / open-licensed sources and (b) inventing original items. The project also architects and builds two systems:

1. **Test designer / generator** — authors and procedurally generates test items and full test sets.
2. **Renderer / test executor** — administers tests (timing, navigation, scoring, feedback).

Stack: Go 1.25+, AWS Go SDK v2.

## Scope

**Full ability suite, logic-first.** Cover all five ability families below, but treat **logical / abstract / inductive reasoning and Mensa-style figure reasoning as the primary focus**. **Cognitive ability only** — objective right/wrong items with scoring. Personality / behavioral profiling (DISC, Big Five, Thomas PPA, Garuda profiles) is **out of scope**.

## Targeted test types

### A. Logical / Abstract / Inductive  ← PRIMARY FOCUS
- **Figure series** — pick the next figure that logically continues a sequence (SHL inductive style).
- **Matrices** — 3×3 (sometimes 2×2) grid with one missing tile; choose from 4–6 options. Transformation rules to cover: colour change, count change, rotation, reflection/mirroring, movement/rearrangement, folding/symmetry (Matrigma / Raven's-progressive-matrices style).
- **Mensa-style Figure Reasoning** — homogeneous figural-logic items, increasing difficulty, strictly speeded (reference: ~45 items / 20 min).
- **Odd-one-out / classification** (figural).
- **Logical deduction / syllogisms** — assumptions + conclusion → true / false / cannot say (overlaps with Verbal).

### B. Numerical
- **Data interpretation** — read a table or graph, compute, choose from 4–5 options (SHL).
- **Calculation / equations** — solve, find X (PI Cognitive, IST).
- **Number series** — next number in a sequence.
- **Arithmetic signs** — insert + − × ÷ to make an equation true (IST).
- **Number speed & accuracy** — e.g. which value is furthest from the middle number (Thomas GIA).

### C. Verbal
- **Reading comprehension** — 80–300-word passage, answer true / false / cannot say (SHL, Cubiks, Saville).
- **Analogies** — A is to B as C is to ?
- **Antonyms** (opposite words) and **synonyms / similarities** — including pick-the-matching-pair and odd-word-out (IST similarities, GIA word meaning).
- **Sentence completion** (IST).

### D. Spatial / Figural
- **Mental rotation** — rotated / mirrored letters or shapes; judge whether they match (Thomas GIA spatial visualization).
- **Cube / 3D reasoning** — rotate or fold cube nets to a target (IST cubes).
- **Figure composition** — assemble fragments into one of the answer figures (IST).

### E. Speed & Working Memory
- **Perceptual speed** — rapid matching, e.g. upper/lower-case letter pairs (Thomas GIA).
- **Working-memory reasoning** — hold a stated fact, then answer a question about it (Thomas GIA reasoning).

## Branded systems to mirror (formats only — see IP note)

SHL · People Test Logic · ACE Cognitive · AON / cut-e · Eligo · Garuda (logic part only) · PI Cognitive Assessment (PICA / PLI) · IST 2000-R (Hogrefe) · Thomas GIA · Talogy / Cubiks · Saville (Swift) · Matrigma (Assessio — Classic + Adaptive) · Mensa.

## Test mechanics (requirements for designer / generator / renderer)

- **Item formats:** multiple choice (4–6 options), open numeric answer, true/false/cannot-say.
- **Timing:** support strict global limits and per-item limits (e.g. adaptive Matrigma 60 s/item; GIA 6 min/section). Speed is a first-class scoring dimension.
- **Difficulty:** support both fixed *increasing-difficulty* ordering and **adaptive** delivery (next item's difficulty depends on the previous answer).
- **Composite tests:** a single test may combine several families into timed sections (IST, PI).
- **Scoring & feedback:** raw score, percentile / normal-distribution band, and IQ-style scaled score; per-item explanations after completion.