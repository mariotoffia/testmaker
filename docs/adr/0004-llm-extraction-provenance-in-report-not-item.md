# ADR-0004: LLM-extraction provenance lives in the ingest Report, not on the item

- **Status:** Accepted
- **Date:** 2026-07-09

## Context

Block 12's LLM library adds an extraction path (`app/ingest.IngestLLM`): a
source's unstructured fetched payload is lifted by a model into structured item
candidates, each validated through `item.NewItem` before it reaches the bank.
An LLM-lifted item is a *model transformation* of source text — which model,
which prompt, and which prompt version produced it are real provenance a future
psychometric-calibration step would want, because an unnormed model output must
be treated differently from a deterministically normalized fetched item.

`item.Provenance` today records `SourceID`, `Origin`, and `Redistributable`. It
has **no** model / prompt fields. The fork in the road: either widen the domain
aggregate now (add `Model`, `PromptID`, `PromptVersion` to `item.Provenance`,
threaded from the LLM result through every constructor and persisted per item),
or record that provenance at the run level and defer the per-item fields until a
consumer exists. Widening a persisted aggregate is hard to reverse, so the
choice is recorded here rather than left implicit in the code.

## Decision

**Per-run, not per-item.** The model and prompt provenance of an extraction run
is written to `ingest.Report.Note` (`"llm extraction: model=… prompt=… v…"`),
sourced from the `ports.LLM` result the `app/llm` service returns. No fields are
added to `item.Provenance`; extracted items are tagged `Origin =
item.OriginGenerated` (the domain already counts LLM output as generated) and
inherit the source's `Redistributable`, exactly like any other generated item.

No consumer reads model/prompt provenance yet — the calibration step that would
is unbuilt (Block-status in [IMPLEMENTATION_PLAN.md](../../IMPLEMENTATION_PLAN.md)).
Adding three persisted, never-read fields to the central item aggregate now
would be speculative surface (YAGNI): every constructor, every snapshot, every
adapter mapping would carry them with nothing consuming them.

To keep re-runs from silently corrupting the bank, extracted item ids are
**content-addressed** — `"{SourceID}-llm-{sha256(stem+options)[:12]}"` — not
positional. Re-extracting the same source rewrites each candidate's own item
(idempotent) instead of a positional `…-llm-3` clobbering an unrelated item that
merely shared an array index. This is recorded here because it is the mitigation
that makes the per-run-provenance decision safe: identity tracks content, so the
absence of per-item run metadata does not make re-runs lossy or ambiguous.

## Consequences

- **The domain aggregate stays lean.** `item.Provenance` keeps three fields and
  one meaning; the LLM path adds no domain surface. The interface-size and
  YAGNI defaults hold.
- **Run provenance is coarse and mutable — accepted.** `Report.Note` is a single
  human-readable string, not queryable, not per-item, and overwritten each run.
  A caller that needs "which model produced *this* item" cannot get it today.
  That caller does not exist yet; when it does, this ADR is the seam to revisit.
- **The documented upgrade path** (also noted inline as a `ponytail:` comment in
  `app/ingest/extract.go` and in [DESIGN.md](../../DESIGN.md) §6): add
  `Model` / `PromptID` / `PromptVersion` to `item.Provenance`, populate them in
  `IngestLLM` from the `llm.Result`, and thread them through the constructor and
  snapshot. Content-addressed ids mean existing extracted items can be
  re-extracted into their same ids to backfill the new fields without duplicating
  bank entries.
- **A different `Origin` was considered and rejected.** Introducing an
  `OriginLLM` distinct from `OriginGenerated` would fork the taxonomy for a
  distinction no consumer draws yet; `OriginGenerated` already carries the
  "unnormed, model-produced" meaning calibration needs.
