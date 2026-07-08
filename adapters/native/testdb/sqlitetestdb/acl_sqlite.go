package sqlitetestdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
// upgrade path for the snapshot fields that later blocks add (item = Block 4,
// test = Block 7, session = Block 8).
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

func (s *Store) saveTestRow(ctx context.Context, snap testset.TestSnapshot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tests (id, title) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET title = excluded.title`,
		string(snap.ID), snap.Title)
	if err != nil {
		return ErrStore.WithMessagef("save test %q", snap.ID).Wrap(err)
	}
	return nil
}

func (s *Store) getTestRow(ctx context.Context, id testset.TestID) (testset.TestSnapshot, bool, error) {
	snap := testset.TestSnapshot{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT title FROM tests WHERE id = ?`, string(id)).Scan(&snap.Title)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return testset.TestSnapshot{}, false, nil
	case err != nil:
		return testset.TestSnapshot{}, false, ErrStore.WithMessagef("get test %q", id).Wrap(err)
	}
	return snap, true, nil
}

func (s *Store) listTestRows(ctx context.Context) ([]testset.TestSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title FROM tests ORDER BY id`)
	if err != nil {
		return nil, ErrStore.WithMessage("list tests").Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]testset.TestSnapshot, 0)
	for rows.Next() {
		var snap testset.TestSnapshot
		if err := rows.Scan(&snap.ID, &snap.Title); err != nil {
			return nil, ErrStore.WithMessage("scan test").Wrap(err)
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

func (s *Store) saveItemRow(ctx context.Context, snap item.ItemSnapshot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO items (id, source_id, stem) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET source_id = excluded.source_id, stem = excluded.stem`,
		string(snap.ID), snap.SourceID, snap.Stem)
	if err != nil {
		return ErrStore.WithMessagef("save item %q", snap.ID).Wrap(err)
	}
	return nil
}

func (s *Store) getItemRow(ctx context.Context, id item.ItemID) (item.ItemSnapshot, bool, error) {
	snap := item.ItemSnapshot{ID: id}
	err := s.db.QueryRowContext(ctx,
		`SELECT source_id, stem FROM items WHERE id = ?`, string(id)).
		Scan(&snap.SourceID, &snap.Stem)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return item.ItemSnapshot{}, false, nil
	case err != nil:
		return item.ItemSnapshot{}, false, ErrStore.WithMessagef("get item %q", id).Wrap(err)
	}
	return snap, true, nil
}

func (s *Store) listItemRows(ctx context.Context) ([]item.ItemSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, stem FROM items ORDER BY id`)
	if err != nil {
		return nil, ErrStore.WithMessage("list items").Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]item.ItemSnapshot, 0)
	for rows.Next() {
		var snap item.ItemSnapshot
		if err := rows.Scan(&snap.ID, &snap.SourceID, &snap.Stem); err != nil {
			return nil, ErrStore.WithMessage("scan item").Wrap(err)
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrStore.WithMessage("iterate items").Wrap(err)
	}
	return out, nil
}

// --- sessions table ---

func (s *Store) saveSessionRow(ctx context.Context, snap session.SessionSnapshot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, test_id) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET test_id = excluded.test_id`,
		string(snap.ID), snap.TestID)
	if err != nil {
		return ErrStore.WithMessagef("save session %q", snap.ID).Wrap(err)
	}
	return nil
}

func (s *Store) getSessionRow(ctx context.Context, id session.SessionID) (session.SessionSnapshot, bool, error) {
	snap := session.SessionSnapshot{ID: id}
	err := s.db.QueryRowContext(ctx,
		`SELECT test_id FROM sessions WHERE id = ?`, string(id)).Scan(&snap.TestID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return session.SessionSnapshot{}, false, nil
	case err != nil:
		return session.SessionSnapshot{}, false, ErrStore.WithMessagef("get session %q", id).Wrap(err)
	}
	return snap, true, nil
}
