package sqlitetestdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	// modernc.org/sqlite is a pure-Go (no cgo) SQLite driver; the blank import
	// registers it under the "sqlite" driver name. Keeping it here — and nowhere
	// else — is what isolates the driver to this anti-corruption layer.
	_ "modernc.org/sqlite"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/testset"
)

// driverName is the database/sql driver registered by modernc.org/sqlite.
const driverName = "sqlite"

// schemaMigrations returns the ordered schema migrations. Each entry is a set of
// statements applied together in one transaction; its index+1 becomes the
// database's PRAGMA user_version once applied.
//
// Append-only: never edit a shipped migration, add a new one. This is the
// upgrade path for the snapshot fields per block: item (Block 4) and test
// (Block 7) migrated their scaffold tables to JSON documents below; session
// (Block 8) is the next expected entry.
func schemaMigrations() [][]string {
	return [][]string{
		{
			`CREATE TABLE tests (
				id    TEXT PRIMARY KEY,
				title TEXT NOT NULL
			) STRICT`,
			`CREATE TABLE items (
				id        TEXT PRIMARY KEY,
				source_id TEXT NOT NULL,
				stem      TEXT NOT NULL
			) STRICT`,
			`CREATE TABLE sessions (
				id      TEXT PRIMARY KEY,
				test_id TEXT NOT NULL
			) STRICT`,
		},
		// Block 4 (item bank): the item aggregate outgrew the scaffold's flat
		// (source_id, stem) columns — a snapshot now carries nested value objects
		// (provenance, stimulus parts, options, key, difficulty). Persist it as a
		// single JSON document keyed by id; querying by family/type/difficulty is
		// done in Go via ItemFilter.Matches, so no query column is duplicated in
		// SQL yet. Upgrade path if the bank outgrows a full scan: promote the hot
		// filter fields to real columns + a WHERE clause.
		//
		// The old (source_id, stem) shape cannot be transformed into a valid
		// ItemSnapshot (which requires provenance, test type, answer format and
		// key), so there is no correct in-place conversion. But SaveItem was a
		// public file-backed writer in the scaffold, so a caller *could* have
		// persisted rows in the old shape. Rather than DROP (silent data loss), we
		// quarantine the old table by renaming it to items_v1_legacy: any rows are
		// preserved for manual recovery and a fresh database just carries an empty
		// legacy table. A future data-preserving migration should use
		// rename→create→INSERT…SELECT(transform)→drop.
		{
			`ALTER TABLE items RENAME TO items_v1_legacy`,
			`CREATE TABLE items (
				id       TEXT PRIMARY KEY,
				snapshot TEXT NOT NULL
			) STRICT`,
		},
		// Block 4 (item bank), query columns: expose the hot ItemFilter fields as
		// indexed columns so ListItems filters in SQL (a WHERE clause) instead of
		// unmarshalling and scanning every row in Go. They are VIRTUAL GENERATED
		// columns computed from the JSON snapshot with json_extract (SQLite JSON1),
		// so they are by definition a projection of the blob and can never drift
		// from it — a raw INSERT of just (id, snapshot) still gets correct columns,
		// and pre-existing v2 rows need no back-fill. SQLite can only ADD virtual
		// (not stored) generated columns via ALTER, and indexes them fine. The
		// low-cardinality origin/redistributable columns are exposed for filtering
		// but left unindexed — three distinct values index poorly; add an index
		// only if a query proves hot.
		{
			`ALTER TABLE items ADD COLUMN test_type       TEXT    GENERATED ALWAYS AS (json_extract(snapshot, '$.TestType')) VIRTUAL`,
			`ALTER TABLE items ADD COLUMN family          TEXT    GENERATED ALWAYS AS (json_extract(snapshot, '$.Family')) VIRTUAL`,
			`ALTER TABLE items ADD COLUMN origin          TEXT    GENERATED ALWAYS AS (json_extract(snapshot, '$.Provenance.Origin')) VIRTUAL`,
			`ALTER TABLE items ADD COLUMN redistributable TEXT    GENERATED ALWAYS AS (json_extract(snapshot, '$.Provenance.Redistributable')) VIRTUAL`,
			`ALTER TABLE items ADD COLUMN difficulty_band INTEGER GENERATED ALWAYS AS (json_extract(snapshot, '$.Difficulty.Band')) VIRTUAL`,
			`CREATE INDEX idx_items_family ON items(family)`,
			`CREATE INDEX idx_items_test_type ON items(test_type)`,
			`CREATE INDEX idx_items_difficulty_band ON items(difficulty_band)`,
		},
		// Block 7 (test authoring): the Test aggregate outgrew the scaffold's flat
		// (id, title) columns — a snapshot now carries ordered sections, item refs,
		// timing, a delivery policy and derived families. Persist it as a single
		// JSON document keyed by id, exactly like items (migration 2). No query
		// column is added: TestFilter has no consumer yet and tests are few, so
		// ListTests full-scans. Upgrade path if a test-query surface (Block 10)
		// lands: add generated columns like the items table.
		//
		// The old (id, title) shape cannot be transformed into a valid TestSnapshot
		// (which now requires sections/policy), so there is no correct in-place
		// conversion — the same reason migration 2 quarantined items. Rename the old
		// table to tests_v1_legacy: any rows are preserved for manual recovery and a
		// fresh database just carries an empty legacy table.
		{
			`ALTER TABLE tests RENAME TO tests_v1_legacy`,
			`CREATE TABLE tests (
				id       TEXT PRIMARY KEY,
				snapshot TEXT NOT NULL
			) STRICT`,
		},
		// Block 8 (renderer / executor): the session aggregate outgrew the
		// scaffold's flat (id, test_id) columns — a snapshot now carries the plan
		// (sections, item refs, timing), the lifecycle state, timing anchors, the
		// presented item and every captured response. Persist it as a single JSON
		// document keyed by id, exactly like items (migration 2) and tests
		// (migration 4). No query column is added: SessionFilter has no consumer
		// yet and GetSession is a point lookup. Upgrade path if a session-query
		// surface (Block 9 scoring) lands: add generated columns like the items
		// table.
		//
		// The old (id, test_id) shape cannot be transformed into a valid
		// SessionSnapshot (which now requires a plan, state and timing anchors), so
		// there is no correct in-place conversion — the same reason migrations 2
		// and 4 quarantined their scaffold tables. Rename the old table to
		// sessions_v1_legacy: any rows are preserved for manual recovery and a
		// fresh database just carries an empty legacy table.
		{
			`ALTER TABLE sessions RENAME TO sessions_v1_legacy`,
			`CREATE TABLE sessions (
				id       TEXT PRIMARY KEY,
				snapshot TEXT NOT NULL
			) STRICT`,
		},
	}
}

// openDB opens the database at dsn and brings its schema up to date.
//
// ponytail: a single connection keeps a ":memory:" database alive across the
// database/sql pool and serializes writes (SQLite allows one writer at a time),
// which is all a test DB needs. Upgrade path if durability under concurrency
// ever matters: WAL journal + busy_timeout + a real connection pool.
func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, ErrStore.WithMessagef("open %q", dsn).Wrap(err)
	}
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, ErrStore.WithMessagef("open %q", dsn).Wrap(err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// migrate applies every not-yet-applied migration inside its own transaction and
// advances PRAGMA user_version, so a reopened database resumes where it left off.
func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return ErrStore.WithMessage("read schema version").Wrap(err)
	}

	migrations := schemaMigrations()
	// A database file is untrusted input: a corrupt header or one written by a
	// newer build can report a version this binary cannot service. Reject it
	// instead of panicking on a negative index or silently running an
	// unrecognised schema.
	if version < 0 || version > len(migrations) {
		return ErrStore.WithMessagef(
			"database schema version %d is not supported (this build knows %d migrations)",
			version, len(migrations))
	}
	for v := version; v < len(migrations); v++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return ErrStore.WithMessagef("begin migration %d", v+1).Wrap(err)
		}
		for _, stmt := range migrations[v] {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				_ = tx.Rollback()
				return ErrStore.WithMessagef("apply migration %d", v+1).Wrap(err)
			}
		}
		// PRAGMA user_version does not accept a bound parameter; v+1 is an int we
		// fully control, so the formatted value is not attacker-influenced.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			_ = tx.Rollback()
			return ErrStore.WithMessagef("bump schema version to %d", v+1).Wrap(err)
		}
		if err := tx.Commit(); err != nil {
			return ErrStore.WithMessagef("commit migration %d", v+1).Wrap(err)
		}
	}
	return nil
}

// --- tests table ---

// The Test aggregate is persisted as a single JSON document (see migration 4),
// mirroring the items table. encoding/json round-trips every snapshot field and
// preserves the nil-vs-empty slice distinction, so a snapshot read back is
// reflect.DeepEqual to the one saved — the parity the conformance suite asserts
// against the memory store. Unmarshalling allocates fresh slices, so a returned
// snapshot never aliases stored bytes or caller input.

func (s *Store) saveTestRow(ctx context.Context, snap testset.TestSnapshot) error {
	blob, err := json.Marshal(snap)
	if err != nil {
		return ErrStore.WithMessagef("marshal test %q", snap.ID).Wrap(err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tests (id, snapshot) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET snapshot = excluded.snapshot`,
		string(snap.ID), string(blob))
	if err != nil {
		return ErrStore.WithMessagef("save test %q", snap.ID).Wrap(err)
	}
	return nil
}

func unmarshalTest(id testset.TestID, blob string) (testset.TestSnapshot, error) {
	var snap testset.TestSnapshot
	if err := json.Unmarshal([]byte(blob), &snap); err != nil {
		return testset.TestSnapshot{}, ErrStore.WithMessagef("unmarshal test %q", id).Wrap(err)
	}
	// The id column is authoritative; a blob whose embedded id disagrees means a
	// corrupted or hand-edited row. Reject it rather than return a snapshot under
	// the wrong key (mirrors unmarshalItem).
	if snap.ID != id {
		return testset.TestSnapshot{}, ErrStore.WithMessagef("test row %q holds a snapshot with id %q", id, snap.ID)
	}
	return snap, nil
}

func (s *Store) getTestRow(ctx context.Context, id testset.TestID) (testset.TestSnapshot, bool, error) {
	var blob string
	err := s.db.QueryRowContext(ctx, `SELECT snapshot FROM tests WHERE id = ?`, string(id)).Scan(&blob)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return testset.TestSnapshot{}, false, nil
	case err != nil:
		return testset.TestSnapshot{}, false, ErrStore.WithMessagef("get test %q", id).Wrap(err)
	}
	snap, err := unmarshalTest(id, blob)
	if err != nil {
		return testset.TestSnapshot{}, false, err
	}
	return snap, true, nil
}

func (s *Store) listTestRows(ctx context.Context) ([]testset.TestSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, snapshot FROM tests ORDER BY id`)
	if err != nil {
		return nil, ErrStore.WithMessage("list tests").Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]testset.TestSnapshot, 0)
	for rows.Next() {
		var id, blob string
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, ErrStore.WithMessage("scan test").Wrap(err)
		}
		snap, err := unmarshalTest(testset.TestID(id), blob)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrStore.WithMessage("iterate tests").Wrap(err)
	}
	return out, nil
}

func (s *Store) deleteTestRow(ctx context.Context, id testset.TestID) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM tests WHERE id = ?`, string(id)); err != nil {
		return ErrStore.WithMessagef("delete test %q", id).Wrap(err)
	}
	return nil
}

// --- items table ---

// The item aggregate is persisted as a single JSON document (see migration 2).
// The filterable query columns (migration 3) are VIRTUAL generated columns
// computed from that document, so there is one source of truth: writes touch
// only the blob and the columns follow automatically. encoding/json round-trips
// every snapshot field and preserves the nil-vs-empty slice distinction
// (null↔nil, []↔non-nil-empty), so a snapshot read back is reflect.DeepEqual to
// the one saved — the parity the conformance suite asserts against the memory
// store. Unmarshalling always allocates fresh slices, so a returned snapshot
// never aliases stored bytes or caller input.

func (s *Store) saveItemRow(ctx context.Context, snap item.ItemSnapshot) error {
	blob, err := json.Marshal(snap)
	if err != nil {
		return ErrStore.WithMessagef("marshal item %q", snap.ID).Wrap(err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO items (id, snapshot) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET snapshot = excluded.snapshot`,
		string(snap.ID), string(blob))
	if err != nil {
		return ErrStore.WithMessagef("save item %q", snap.ID).Wrap(err)
	}
	return nil
}

func unmarshalItem(id item.ItemID, blob string) (item.ItemSnapshot, error) {
	var snap item.ItemSnapshot
	if err := json.Unmarshal([]byte(blob), &snap); err != nil {
		return item.ItemSnapshot{}, ErrStore.WithMessagef("unmarshal item %q", id).Wrap(err)
	}
	// The id column is authoritative; a blob whose embedded id disagrees means a
	// corrupted or hand-edited row. Reject it rather than return a snapshot under
	// the wrong key.
	if snap.ID != id {
		return item.ItemSnapshot{}, ErrStore.WithMessagef("item row %q holds a snapshot with id %q", id, snap.ID)
	}
	return snap, nil
}

func (s *Store) getItemRow(ctx context.Context, id item.ItemID) (item.ItemSnapshot, bool, error) {
	var blob string
	err := s.db.QueryRowContext(ctx,
		`SELECT snapshot FROM items WHERE id = ?`, string(id)).Scan(&blob)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return item.ItemSnapshot{}, false, nil
	case err != nil:
		return item.ItemSnapshot{}, false, ErrStore.WithMessagef("get item %q", id).Wrap(err)
	}
	snap, err := unmarshalItem(id, blob)
	if err != nil {
		return item.ItemSnapshot{}, false, err
	}
	return snap, true, nil
}

// itemListQuery builds the SELECT that lists items matching filter, mapping each
// ItemFilter dimension to a predicate over the migration-3 query columns. It
// mirrors item.ItemFilter.Matches exactly: multi-value fields become IN clauses
// (OR within), all predicates are ANDed, and a zero difficulty bound is omitted
// (unbounded). The shared conformance suite runs identical filters against this
// store and the in-Go memory store and asserts identical results, so any
// divergence between this SQL and Matches fails the build.
//
// Multi-value fields are de-duplicated (see toAnyStrings), so the bind count
// tracks the number of distinct values, not the raw slice length. Real callers
// filter over the closed vocabularies (≤5 families, ≤19 test types, ≤3 origins,
// ≤3 redist ⇒ ~30 binds), far under SQLite's variable limit. The enum types are
// string-backed and technically open, so only a caller inventing thousands of
// distinct junk codes could approach the limit — and those match no valid row.
func itemListQuery(f item.ItemFilter) (string, []any) {
	var conds []string
	var args []any
	in := func(col string, vals []any) {
		if len(vals) == 0 {
			return
		}
		conds = append(conds, col+" IN ("+strings.TrimSuffix(strings.Repeat("?,", len(vals)), ",")+")")
		args = append(args, vals...)
	}
	in("family", toAnyStrings(f.Families))
	in("test_type", toAnyStrings(f.TestTypes))
	in("origin", toAnyStrings(f.Origins))
	in("redistributable", toAnyStrings(f.Redistributable))
	// COALESCE mirrors Go: ItemFilter.Matches runs on the unmarshalled snapshot,
	// where a missing Difficulty.Band is the zero value 0, not absent. A store row
	// always carries Band (>=1, NewItem-enforced; json.Marshal always emits it),
	// so this only affects a hand-edited blob — but it keeps the generated column
	// an exact behavioural projection of the blob even there.
	if f.MinDifficulty > 0 {
		conds = append(conds, "COALESCE(difficulty_band, 0) >= ?")
		args = append(args, f.MinDifficulty)
	}
	if f.MaxDifficulty > 0 {
		conds = append(conds, "COALESCE(difficulty_band, 0) <= ?")
		args = append(args, f.MaxDifficulty)
	}
	query := `SELECT id, snapshot FROM items`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	return query + " ORDER BY id", args
}

// toAnyStrings converts a slice of string-kinded values to the []any of bind
// parameters an IN clause needs, dropping duplicates (first occurrence wins) so
// the placeholder and bind count track the number of distinct values, not the
// raw slice length, regardless of how many repeats a caller passes. IN is a set
// test, so de-duplication never changes the result; it mirrors the slices.Contains
// set semantics of item.ItemFilter.Matches.
func toAnyStrings[T ~string](vs []T) []any {
	if len(vs) == 0 {
		return nil
	}
	// Pre-size to the vocabulary, not len(vs): the output holds only distinct
	// values, so a filter padded with many repeats must not reserve for the raw
	// length. capHint exceeds every closed vocabulary (largest is ~19 test types).
	const capHint = 32
	seen := make(map[T]struct{}, min(len(vs), capHint))
	out := make([]any, 0, min(len(vs), capHint))
	for _, v := range vs {
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, string(v))
	}
	return out
}

func (s *Store) listItemRows(ctx context.Context, filter item.ItemFilter) ([]item.ItemSnapshot, error) {
	query, args := itemListQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, ErrStore.WithMessage("list items").Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]item.ItemSnapshot, 0)
	for rows.Next() {
		var id, blob string
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, ErrStore.WithMessage("scan item").Wrap(err)
		}
		snap, err := unmarshalItem(item.ItemID(id), blob)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrStore.WithMessage("iterate items").Wrap(err)
	}
	return out, nil
}

// --- sessions table ---

// The Session aggregate is persisted as a single JSON document (see migration
// 5), mirroring the items and tests tables. encoding/json round-trips every
// snapshot field and preserves the nil-vs-empty slice distinction, and the
// aggregate normalizes every timestamp to UTC in Snapshot, so a snapshot read
// back is reflect.DeepEqual to the one saved — the parity the conformance suite
// asserts against the memory store. Unmarshalling allocates fresh slices, so a
// returned snapshot never aliases stored bytes or caller input.

func (s *Store) saveSessionRow(ctx context.Context, snap session.SessionSnapshot) error {
	blob, err := json.Marshal(snap)
	if err != nil {
		return ErrStore.WithMessagef("marshal session %q", snap.ID).Wrap(err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, snapshot) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET snapshot = excluded.snapshot`,
		string(snap.ID), string(blob))
	if err != nil {
		return ErrStore.WithMessagef("save session %q", snap.ID).Wrap(err)
	}
	return nil
}

func unmarshalSession(id session.SessionID, blob string) (session.SessionSnapshot, error) {
	var snap session.SessionSnapshot
	if err := json.Unmarshal([]byte(blob), &snap); err != nil {
		return session.SessionSnapshot{}, ErrStore.WithMessagef("unmarshal session %q", id).Wrap(err)
	}
	// The id column is authoritative; a blob whose embedded id disagrees means a
	// corrupted or hand-edited row. Reject it rather than return a snapshot under
	// the wrong key (mirrors unmarshalItem / unmarshalTest).
	if snap.ID != id {
		return session.SessionSnapshot{}, ErrStore.WithMessagef("session row %q holds a snapshot with id %q", id, snap.ID)
	}
	return snap, nil
}

func (s *Store) getSessionRow(ctx context.Context, id session.SessionID) (session.SessionSnapshot, bool, error) {
	var blob string
	err := s.db.QueryRowContext(ctx, `SELECT snapshot FROM sessions WHERE id = ?`, string(id)).Scan(&blob)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return session.SessionSnapshot{}, false, nil
	case err != nil:
		return session.SessionSnapshot{}, false, ErrStore.WithMessagef("get session %q", id).Wrap(err)
	}
	snap, err := unmarshalSession(id, blob)
	if err != nil {
		return session.SessionSnapshot{}, false, err
	}
	return snap, true, nil
}
