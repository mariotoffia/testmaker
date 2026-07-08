package sqlitetestdb_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/testdb/sqlitetestdb"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/ports/testdbtest"
)

// Compile-time proof that Store satisfies every TestDb port (kept in _test.go so
// the production package imports no ports package, per the arch rules).
var (
	_ ports.TestRepository    = (*sqlitetestdb.Store)(nil)
	_ ports.ItemRepository    = (*sqlitetestdb.Store)(nil)
	_ ports.SessionRepository = (*sqlitetestdb.Store)(nil)
)

// mustOpen opens a Store and schedules its Close on the test's cleanup. It takes
// the parent *testing.T so a constructor called once per conformance subtest
// still has every handle released when the parent test finishes.
func mustOpen(t *testing.T, dsn string) *sqlitetestdb.Store {
	t.Helper()
	store, err := sqlitetestdb.Open(dsn)
	if err != nil {
		t.Fatalf("open %q: %v", dsn, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// The conformance suites run against two backings — an in-memory database and a
// real file — so the "done when" (memory and sqlite interchangeable, over both
// :memory: and a file DB) is proven by the same shared contract memorytestdb
// passes. Each constructor call gets a fresh, empty database: ":memory:" is
// isolated per Open, and the file backing takes a unique t.TempDir() per call.

func TestTestRepositoryConformance(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		testdbtest.RunTestRepositoryTests(t, func() ports.TestRepository {
			return mustOpen(t, ":memory:")
		})
	})
	t.Run("file", func(t *testing.T) {
		testdbtest.RunTestRepositoryTests(t, func() ports.TestRepository {
			return mustOpen(t, filepath.Join(t.TempDir(), "testdb.sqlite"))
		})
	})
}

func TestItemRepositoryConformance(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		testdbtest.RunItemRepositoryTests(t, func() ports.ItemRepository {
			return mustOpen(t, ":memory:")
		})
	})
	t.Run("file", func(t *testing.T) {
		testdbtest.RunItemRepositoryTests(t, func() ports.ItemRepository {
			return mustOpen(t, filepath.Join(t.TempDir(), "testdb.sqlite"))
		})
	})
}

func TestSessionRepositoryConformance(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		testdbtest.RunSessionRepositoryTests(t, func() ports.SessionRepository {
			return mustOpen(t, ":memory:")
		})
	})
	t.Run("file", func(t *testing.T) {
		testdbtest.RunSessionRepositoryTests(t, func() ports.SessionRepository {
			return mustOpen(t, filepath.Join(t.TempDir(), "testdb.sqlite"))
		})
	})
}

// TestSharedKeyspace proves the three repositories served by one Store keep
// independent keyspaces over both backings (a wrong table name or a cross-repo
// delete would surface here, not in the per-port suites).
func TestSharedKeyspace(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		testdbtest.RunSharedKeyspaceTests(t, func() testdbtest.TestDB {
			return mustOpen(t, ":memory:")
		})
	})
	t.Run("file", func(t *testing.T) {
		testdbtest.RunSharedKeyspaceTests(t, func() testdbtest.TestDB {
			return mustOpen(t, filepath.Join(t.TempDir(), "testdb.sqlite"))
		})
	})
}

// TestConcurrentAccess drives one Store from many goroutines at once. With a
// single underlying connection (SetMaxOpenConns(1)) database/sql serializes the
// calls; this test proves that serialization is deadlock-free — a query that
// held the only connection while issuing another would hang here — and is clean
// under -race.
func TestConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := mustOpen(t, ":memory:")

	const workers = 24
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := testset.TestID(fmt.Sprintf("t-%d", i))
			if err := store.SaveTest(ctx, testset.TestSnapshot{ID: id, Title: "x"}); err != nil {
				errCh <- err
				return
			}
			if _, err := store.GetTest(ctx, id); err != nil {
				errCh <- err
				return
			}
			if _, err := store.ListTests(ctx, testset.TestFilter{}); err != nil {
				errCh <- err
				return
			}
			if err := store.DeleteTest(ctx, id); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent op: %v", err)
	}
}

// TestFileDBPersistsAcrossReopen is the durability proof a memory store cannot
// give: data written through one Store is still there after Close and a reopen
// of the same file — which also exercises the migration runner finding an
// already-current schema (PRAGMA user_version) and applying nothing.
func TestFileDBPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "persist.sqlite")

	first, err := sqlitetestdb.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Close at acquisition so a fatal path below cannot leak the handle; the
	// explicit Close later is what the reopen tests, and a second Close is a
	// no-op on database/sql.
	t.Cleanup(func() { _ = first.Close() })
	if err := first.SaveTest(ctx, testset.TestSnapshot{ID: "gia", Title: "GIA"}); err != nil {
		t.Fatalf("save test: %v", err)
	}
	// A full item snapshot (nested value objects + a non-nil option slice)
	// exercises the JSON-blob column across a real Close/reopen — the durability
	// proof a memory store cannot give, and a check that the blob survives intact.
	wantItem := mustItemSnapshot(t)
	if err := first.SaveItem(ctx, wantItem); err != nil {
		t.Fatalf("save item: %v", err)
	}
	if err := first.SaveSession(ctx, session.SessionSnapshot{ID: "sess-1", TestID: "gia"}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := mustOpen(t, path)
	if got, err := second.GetTest(ctx, "gia"); err != nil || got.Title != "GIA" {
		t.Fatalf("test after reopen: got %+v, err %v", got, err)
	}
	if got, err := second.GetItem(ctx, "omib-1"); err != nil || !reflect.DeepEqual(got, wantItem) {
		t.Fatalf("item after reopen: got %+v, err %v", got, err)
	}
	if got, err := second.GetSession(ctx, "sess-1"); err != nil || got.TestID != "gia" {
		t.Fatalf("session after reopen: got %+v, err %v", got, err)
	}
}

// TestUnsupportedSchemaVersionRejected proves Open refuses a database whose
// user_version this build does not understand — a future (higher) version it
// has no migrations for, or a corrupt negative one that would index the
// migration slice out of bounds. The driver is already registered by the
// package under test, so a bare database/sql handle can seed the bad state.
func TestUnsupportedSchemaVersionRejected(t *testing.T) {
	for _, userVersion := range []int{-1, 999} {
		t.Run(fmt.Sprintf("version_%d", userVersion), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.sqlite")

			raw, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatalf("open raw: %v", err)
			}
			if _, err := raw.Exec(fmt.Sprintf("PRAGMA user_version = %d", userVersion)); err != nil {
				_ = raw.Close()
				t.Fatalf("seed user_version: %v", err)
			}
			if err := raw.Close(); err != nil {
				t.Fatalf("close raw: %v", err)
			}

			store, err := sqlitetestdb.Open(path)
			if err == nil {
				_ = store.Close()
				t.Fatalf("opened database with unsupported user_version %d", userVersion)
			}
			if !errors.Is(err, sqlitetestdb.ErrStore) {
				t.Fatalf("want ErrStore, got %v", err)
			}
		})
	}
}

// TestGetItemRejectsIdMismatch proves the id column is authoritative: a row whose
// JSON snapshot carries a different id (a corrupted or hand-edited row) is
// rejected with ErrStore rather than returned under the wrong key. The driver is
// registered by the package under test, so a raw handle can seed the bad row.
func TestGetItemRejectsIdMismatch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mismatch.sqlite")

	// migrate the schema through the real Open, then Close so the raw handle owns
	// the file.
	if err := mustOpen(t, path).Close(); err != nil {
		t.Fatalf("close after migrate: %v", err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	// row id "x" but the blob's snapshot id is "y".
	if _, err := raw.Exec(`INSERT INTO items (id, snapshot) VALUES (?, ?)`, "x", `{"ID":"y"}`); err != nil {
		_ = raw.Close()
		t.Fatalf("seed mismatched row: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	store := mustOpen(t, path)
	if _, err := store.GetItem(ctx, "x"); !errors.Is(err, sqlitetestdb.ErrStore) {
		t.Fatalf("want ErrStore for id mismatch, got %v", err)
	}
}

// TestMigrationV1ToV2PreservesLegacyRows proves migration 2 upgrades a real v1
// database without destroying its data: the pre-Block-4 items(id, source_id,
// stem) rows are quarantined into items_v1_legacy, not dropped, and the untouched
// v1 tests/sessions rows still read back through the store. It is the regression
// lock for the data-preservation fix — a revert to `DROP TABLE items` makes the
// legacy-row assertion fail, and a migration that touched tests/sessions would
// fail the survival assertions. The driver is registered by the package under
// test, so a bare handle can seed the frozen v1 schema (migration 1 is
// append-only, so that shape never changes).
func TestMigrationV1ToV2PreservesLegacyRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.sqlite")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE tests (id TEXT PRIMARY KEY, title TEXT NOT NULL) STRICT`,
		`CREATE TABLE items (id TEXT PRIMARY KEY, source_id TEXT NOT NULL, stem TEXT NOT NULL) STRICT`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, test_id TEXT NOT NULL) STRICT`,
		`INSERT INTO tests (id, title) VALUES ('gia', 'GIA')`,
		`INSERT INTO items (id, source_id, stem) VALUES ('old-1', 'omib', 'legacy stem')`,
		`INSERT INTO sessions (id, test_id) VALUES ('sess-0', 'gia')`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			_ = raw.Close()
			t.Fatalf("seed v1: %v", err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// Open runs migration 2 (RENAME items -> items_v1_legacy, CREATE new items).
	store := mustOpen(t, path)

	// The v1 tests/sessions rows are untouched by migration 2 and must still read
	// back through the store's normal accessors.
	if got, err := store.GetTest(ctx, "gia"); err != nil || got.Title != "GIA" {
		t.Fatalf("v1 test after migration: got %+v, err %v", got, err)
	}
	if got, err := store.GetSession(ctx, "sess-0"); err != nil || got.TestID != "gia" {
		t.Fatalf("v1 session after migration: got %+v, err %v", got, err)
	}

	// The old row must survive in the quarantine table (the DROP-revert tripwire)
	// and the schema must have advanced to v2. Read through a separate handle,
	// then release it before writing so the single-connection store never
	// contends with it.
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen raw: %v", err)
	}
	var sourceID, stem string
	var version int
	if err := legacy.QueryRow(`SELECT source_id, stem FROM items_v1_legacy WHERE id = 'old-1'`).Scan(&sourceID, &stem); err != nil {
		_ = legacy.Close()
		t.Fatalf("legacy row lost: %v", err)
	}
	if err := legacy.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		_ = legacy.Close()
		t.Fatalf("read version: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy handle: %v", err)
	}
	if sourceID != "omib" || stem != "legacy stem" {
		t.Fatalf("legacy row corrupted: source_id=%q stem=%q", sourceID, stem)
	}
	if version != 2 {
		t.Fatalf("user_version = %d, want 2", version)
	}

	// The upgraded database's new items table is the JSON-blob shape and
	// round-trips a full snapshot.
	want := mustItemSnapshot(t)
	if err := store.SaveItem(ctx, want); err != nil {
		t.Fatalf("save item into upgraded db: %v", err)
	}
	if got, err := store.GetItem(ctx, want.ID); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("item roundtrip after upgrade: got %+v, err %v", got, err)
	}

	// A second open finds an already-current schema and applies nothing.
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	reopened := mustOpen(t, path)
	if got, err := reopened.GetItem(ctx, want.ID); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("item after second reopen: got %+v, err %v", got, err)
	}
}

// mustItemSnapshot builds a valid, fully-populated item snapshot via the real
// aggregate for the reopen durability proof.
func mustItemSnapshot(t *testing.T) item.ItemSnapshot {
	t.Helper()
	it, err := item.NewItem(item.ItemSpec{
		ID:           "omib-1",
		Provenance:   item.Provenance{SourceID: "omib", Origin: item.OriginFetched, Redistributable: shared.RedistConditional},
		TestType:     "A2",
		Stimulus:     []item.StimulusPart{{Text: "which figure continues?"}, {MediaKind: item.MediaGrid, MediaRef: "blob://omib-1"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:   item.AnswerKey{OptionID: "c"},
		Explanation: "rotation by 90 degrees",
		Difficulty:  item.Difficulty{Band: 3},
	})
	if err != nil {
		t.Fatalf("build item: %v", err)
	}
	return it.Snapshot()
}
