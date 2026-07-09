# ADR-0007: Asynchronous ingest runs as in-memory jobs at the delivery surface

- **Status:** Accepted
- **Date:** 2026-07-09

## Context

`POST /api/sources/{id}/ingest[-llm]` runs fetch → normalize/extract → store
synchronously, mirroring the CLI. A real fetch or LLM extraction can outlive a
comfortable HTTP request, and the operator console needs to trigger a run,
show progress, and not hold a connection open. The CLI and existing tests
still want the synchronous behaviour. There is deliberately no app-level "job"
use-case: no domain flow consumes job state — it exists purely so a browser
can poll a long HTTP action.

Alternatives considered:

1. **Stay synchronous, raise timeouts** — long-held connections, no progress,
   browsers and proxies time out anyway.
2. **A durable job queue** (sqlite table / SQS + a `JobRepository` port) —
   infrastructure and a new port for state nobody re-reads after the run;
   premature before cloud persistence (ROADMAP §2).
3. **Server-push progress (SSE / WebSocket)** — nicer UX, but a second
   transport idiom for one screen; polling a job id is enough.
4. **In-memory job registry in the composition root, opt-in per request** —
   the smallest thing the console actually needs.

## Decision

- Ingest requests gain `"async": true`. When set, the handler registers a
  **job** (`queued → running → done | failed`) in an in-memory registry in
  `cmd/testmaker` and returns `202 Accepted` with the job body; the run
  executes in a background goroutine with its own context and a configured
  timeout (`limits.ingestTimeoutSeconds`), storing the resulting
  `ingest.Report` or error on the job. Without the flag the endpoints stay
  synchronous — CLI and tests unchanged.
- `GET /api/jobs` (newest first) and `GET /api/jobs/{id}` expose the registry
  to the console, which polls.
- Sync and async runs share one **ingest semaphore**
  (`limits.maxConcurrentIngests`, default 1): a synchronous request that cannot
  acquire it is refused `429`; an async job waits in `queued`. One gate bounds
  outbound fetch and LLM spend regardless of path.
- The registry is bounded (oldest completed jobs are pruned) and **lost on
  restart by design** — a job is a view on a run, not a record of it; the
  durable outcome (bank items, report counts) is already persisted by the run
  itself. Timestamps come from the injected `domain/clock`, so job lifecycles
  are deterministic under test.
- **No new port.** The registry is composition-root wiring. If a durable queue
  ever arrives (with §2 cloud persistence), that is the moment a port is
  introduced — not before.

## Consequences

- Zero infrastructure and an honest single-node scope; multi-instance
  deployments will need the durable queue this ADR explicitly defers.
- The console gets progress via cheap polling; SSE remains open as a later
  refinement without changing the job model.
- A crashed server forgets queued/running jobs — the operator re-triggers;
  ingest is idempotent per source (content-addressed extracted ids, replace-on
  -save bank writes), so a re-run is safe.
- The 202 + poll contract is the one the ROADMAP already sketched; shipped
  synchronous semantics remain the default.
