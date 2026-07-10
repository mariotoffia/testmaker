package sqlitetestdb

import (
	"context"
	"database/sql"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
)

// ErrStore marks a failure of the SQLite backend itself — opening the database,
// running a migration, or executing a statement. The port's semantic outcomes
// (unknown id, invalid snapshot) surface as the owning context's sentinels
// (testset.ErrUnknownTest, item.ErrInvalidItem, …); ErrStore is everything else.
// The wrapped cause stays reachable through Unwrap, so callers can still inspect
// the underlying driver error.
var ErrStore = &shared.TestmakerError{
	Code: "sqlitetestdb.store", Class: shared.ClassUnavailable, Message: "sqlite testdb store error",
}

// Store is a SQLite-backed TestDb serving the Test, Item and Session
// repositories from one database. Open it with Open and release it with Close.
type Store struct {
	db *sql.DB
}

// Open opens (creating if absent) the SQLite database at dsn, migrates its
// schema and returns a ready Store. Use ":memory:" for an ephemeral database or
// a file path for a durable one. The caller owns the returned Store and must
// Close it.
func Open(dsn string) (*Store, error) {
	db, err := openDB(dsn)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return ErrStore.WithMessage("close").Wrap(err)
	}
	return nil
}

// --- TestRepository ---

// SaveTest inserts or replaces a test by id.
func (s *Store) SaveTest(ctx context.Context, snap testset.TestSnapshot) error {
	if snap.ID == "" {
		return testset.ErrInvalidTest.WithMessage("snapshot id is required")
	}
	return s.saveTestRow(ctx, snap)
}

// GetTest returns the snapshot for id or testset.ErrUnknownTest.
func (s *Store) GetTest(ctx context.Context, id testset.TestID) (testset.TestSnapshot, error) {
	snap, found, err := s.getTestRow(ctx, id)
	if err != nil {
		return testset.TestSnapshot{}, err
	}
	if !found {
		return testset.TestSnapshot{}, testset.ErrUnknownTest.With("id", string(id))
	}
	return snap, nil
}

// ListTests returns all tests ordered by id. The filter is a placeholder shell
// (no test-query consumer yet), so every stored test is returned.
func (s *Store) ListTests(ctx context.Context, _ testset.TestFilter) ([]testset.TestSnapshot, error) {
	return s.listTestRows(ctx)
}

// DeleteTest removes a test by id; deleting an absent id is not an error.
func (s *Store) DeleteTest(ctx context.Context, id testset.TestID) error {
	return s.deleteTestRow(ctx, id)
}

// --- ItemRepository ---

// SaveItem inserts or replaces an item by id.
func (s *Store) SaveItem(ctx context.Context, snap item.ItemSnapshot) error {
	if snap.ID == "" {
		return item.ErrInvalidItem.WithMessage("snapshot id is required")
	}
	return s.saveItemRow(ctx, snap)
}

// GetItem returns the snapshot for id or item.ErrUnknownItem.
func (s *Store) GetItem(ctx context.Context, id item.ItemID) (item.ItemSnapshot, error) {
	snap, found, err := s.getItemRow(ctx, id)
	if err != nil {
		return item.ItemSnapshot{}, err
	}
	if !found {
		return item.ItemSnapshot{}, item.ErrUnknownItem.With("id", string(id))
	}
	return snap, nil
}

// ListItems returns the items matching filter, ordered by id. Filtering happens
// in SQL over the migration-3 query columns (see itemListQuery); the returned
// snapshots are decoded from the JSON blob, which stays the source of truth.
func (s *Store) ListItems(ctx context.Context, filter item.ItemFilter) ([]item.ItemSnapshot, error) {
	return s.listItemRows(ctx, filter)
}

// DeleteItem removes an item by id; deleting an absent id is not an error.
func (s *Store) DeleteItem(ctx context.Context, id item.ItemID) error {
	return s.deleteItemRow(ctx, id)
}

// --- SessionRepository ---

// SaveSession inserts or replaces a session by id.
func (s *Store) SaveSession(ctx context.Context, snap session.SessionSnapshot) error {
	if snap.ID == "" {
		return session.ErrInvalidSession.WithMessage("snapshot id is required")
	}
	return s.saveSessionRow(ctx, snap)
}

// GetSession returns the snapshot for id or session.ErrUnknownSession.
func (s *Store) GetSession(ctx context.Context, id session.SessionID) (session.SessionSnapshot, error) {
	snap, found, err := s.getSessionRow(ctx, id)
	if err != nil {
		return session.SessionSnapshot{}, err
	}
	if !found {
		return session.SessionSnapshot{}, session.ErrUnknownSession.With("id", string(id))
	}
	return snap, nil
}
