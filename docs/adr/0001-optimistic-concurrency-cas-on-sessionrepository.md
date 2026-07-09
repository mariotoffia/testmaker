# ADR-0001: Optimistic concurrency (compare-and-swap) on `SessionRepository`

- **Status:** Accepted
- **Date:** 2026-07-09

## Context

A test-taking session is a single aggregate mutated by more than one in-flight
request: two `Answer`s for the same session, or an `Answer` racing a `Complete`.
Each use-case call in `app/execution` loads the session snapshot, applies a
transition, and saves it back. With a plain last-writer-wins `SaveSession`, two
requests that both load version *v* and both write would clobber each other — the
second silently overwrites the first, and an `Answer` landing after a `Complete`
could **resurrect** a finished attempt. The delivery surface makes
these concurrent calls reachable.

The guard has to hold for *every* backend (memory and sqlite) and *every* driving
surface, not just the HTTP server, and it has to be provable. Alternatives
considered: a per-session application-level mutex (does not survive a
multi-instance deployment, and puts concurrency policy in the app instead of the
store); a database row lock (memory store has no equivalent, breaking store
parity); and doing nothing (accepts silent data loss — rejected on the
"never simplify away error handling that prevents data loss" rule).

## Decision

`SaveSession` is an **optimistic compare-and-swap on `SessionSnapshot.Version`**.
It stores the snapshot only when `Version` is exactly one past the currently
stored version (a never-stored session starts at version 1); otherwise it writes
nothing and returns `session.ErrSessionConflict` (`ClassConflict` → HTTP 409).

The version is a **passthrough field on the `Session` aggregate** — never touched
by a transition, carried through `Snapshot` / `RehydrateFromSnapshot`. It is the
DDD-canonical home for an optimistic-lock token, and keeping it on the aggregate
keeps the snapshot round-trip (`Rehydrate(snap).Snapshot()`, which the memory
store uses to deep-copy) self-consistent. The executor increments it at each
persist: `Start` writes version 1, every later `Answer`/`Complete` writes
`loaded+1`. Version 0 is the never-persisted marker and cannot be stored.

The guarantee is a **store contract**, proven once by the shared
`ports/testdbtest` conformance suite for both stores: a sequential guard check
plus a *contended* test (`ConcurrentSaveAtSameVersionRecordsOnce`) that races N
writers at one version under `-race` and asserts exactly one commit and N−1
conflicts.

## Consequences

- The guard is correct independent of how tightly any caller serializes, so the
  execution use-case can be exposed to a multi-instance deployment without a
  redesign. Within today's single-process surface the CAS rarely fires (the
  executor's presented-item check usually rejects a losing request first), so it
  is proven at the store, deterministically, rather than only through the surface.
- Callers observe conflicts as `session.ErrSessionConflict` and must reload and
  retry; the delivery surface maps it to 409.
- The expected version is **server-derived** (from the executor's own load), not
  client-supplied. Propagating a client `If-Match`/ETag is the documented upgrade
  path if optimistic concurrency ever needs to span a client round-trip.
- How each store enforces the swap atomically is a separate, backend-specific
  decision — see [ADR-0002](0002-sqlite-session-version-in-json-with-guarded-write.md)
  for sqlite.
