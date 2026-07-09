# ADR-0002: SQLite session version in the JSON snapshot, enforced by a guarded write

- **Status:** Accepted
- **Date:** 2026-07-09

## Context

[ADR-0001](0001-optimistic-concurrency-cas-on-sessionrepository.md) makes
`SaveSession` a compare-and-swap on `SessionSnapshot.Version`. The sqlite adapter
persists each aggregate as a single JSON document keyed by id (mirroring the
items and tests tables), so two questions fall out: **where does the version
live**, and **what makes the read-of-current-version and the write atomic** under
concurrency.

The first cut shipped with the version inside the JSON blob and relied on
`SetMaxOpenConns(1)` for atomicity: a `BeginTx` held the sole pooled connection,
so a read-check-write could not interleave. That is correct but *process-local*
and single-connection — it serializes all database access, and two `*Store` over
the same file (or two processes) each get their own max-1 pool and could both
read the same version. Adversarial review flagged that the atomicity rested on
the connection limit, not the transaction, and that no test exercised the sqlite
CAS under a real multi-connection pool.

## Decision

**Version stays in the JSON snapshot — no dedicated column, no migration.** A
`Version` column would duplicate a field the blob already carries and require a
back-fill migration; the JSON1 `json_extract` function reads it directly in a
`WHERE` clause, exactly as the items table already does for its query columns.

**The swap is one guarded conditional statement**, not a read-then-write:

- first save (version 1): `INSERT … ON CONFLICT(id) DO NOTHING` — a pre-existing
  row means another writer won the create race;
- later save (version *v*): `UPDATE … SET snapshot = ? WHERE id = ? AND
  json_extract(snapshot,'$.Version') = ?` bound to `v-1`.

**Zero rows changed means conflict** → `session.ErrSessionConflict`. A single
INSERT/UPDATE holds SQLite's write lock for its whole duration, so the check and
the write are atomic without a transaction. Because the guard lives in the
statement, atomicity no longer depends on serializing the pool.

Accordingly, a **file** database now runs with `journal_mode=WAL`, a
`busy_timeout` (a contended writer waits for the write lock instead of returning
`SQLITE_BUSY`), and a real connection pool. A bare **`:memory:`** database keeps
`SetMaxOpenConns(1)`: each connection to `:memory:` gets its own private
database, so a pool would read empty tables, and WAL is a no-op in memory — the
guarded write is still correct there under the one connection.

## Consequences

- The CAS is correct across connections and across processes sharing one file,
  not merely across goroutines in one `*Store`. The contended
  conformance test (`ConcurrentSaveAtSameVersionRecordsOnce`) exercises the file
  backing through the real pool under `-race`; fault-injecting a blind write makes
  it fail deterministically (16 winners / 0 conflicts), so the guard is genuinely
  covered.
- No schema migration was needed to *add* the field, but an earlier database
  persisted session documents before `Version` existed, so migration 6 backfills
  `$.Version = 1` onto any row still missing it (`json_set … WHERE json_extract
  … IS NULL`). Without it those durable rows would read as a NULL version the
  guarded write can never match — a permanently un-writable session. Existing
  new-shape rows and a fresh table are untouched.
- `:memory:` remains single-connection by necessity, so its guarantee stays
  process-local — inherent to `:memory:`, not to the CAS.
- Per-connection pragmas are applied via modernc.org/sqlite's `_pragma` DSN
  parameters, appended to a file DSN in `openDB`. `busy_timeout` is generous
  (5s); writes are one statement each, so the lock is held only for that
  statement.
- If a query surface over sessions ever lands, the version (and any hot filter
  field) can be promoted to a generated column like the items table, without
  moving it out of the blob.
