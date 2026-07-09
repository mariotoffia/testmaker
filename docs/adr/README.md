# Architecture Decision Records

An ADR captures one architecturally significant decision: the context that
forced it, the choice made, and the consequences accepted. They are immutable
once `Accepted` — a decision that changes gets a **new** ADR that supersedes the
old one (link both ways), so history stays readable.

ADRs complement, not replace, the governance docs. `DESIGN.md`, `ARCHITECTURE.md`,
`DDD.md` and `UBIQUITOUS.md` describe the system as it **is**; an ADR records
*why* a specific fork in the road was taken. When a decision is small enough to
live in a doc paragraph, keep it there; promote it to an ADR when it is the kind
of thing a future reader will otherwise re-litigate.

## Index

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-optimistic-concurrency-cas-on-sessionrepository.md) | Optimistic concurrency (compare-and-swap) on `SessionRepository` | Accepted |
| [0002](0002-sqlite-session-version-in-json-with-guarded-write.md) | SQLite session version in the JSON snapshot, enforced by a guarded write | Accepted |
| [0003](0003-content-addressed-blob-store-and-media-offload.md) | Content-addressed blob store, authoring-time media offload, and hardened media serving | Accepted |
| [0004](0004-llm-extraction-provenance-in-report-not-item.md) | LLM-extraction provenance lives in the ingest Report, not on the item | Accepted |
| [0005](0005-embedded-spa-web-ui-served-from-composition-root.md) | Web UI is an embedded React SPA served by the composition root; API re-based under `/api` | Accepted |
| [0006](0006-operator-token-and-hmac-capability-tokens.md) | Access control via a static operator token and stateless HMAC capability tokens | Accepted |
| [0007](0007-async-ingest-jobs-in-memory-at-delivery-surface.md) | Asynchronous ingest runs as in-memory jobs at the delivery surface | Accepted |

## Template

```markdown
# ADR-NNNN: <title>

- **Status:** Proposed | Accepted | Superseded by ADR-MMMM
- **Date:** YYYY-MM-DD

## Context
What forces the decision — the problem, constraints, and the alternatives on the table.

## Decision
The choice, stated plainly.

## Consequences
What this buys, what it costs, and the upgrade path if a constraint changes.
```

Number ADRs sequentially (`NNNN`), never reuse a number, and add a row to the
index in the same change.
