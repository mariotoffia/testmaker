package sqlitetestdb_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/testdb/sqlitetestdb"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
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
	if err := first.SaveItem(ctx, item.ItemSnapshot{ID: "omib-1", SourceID: "omib", Stem: "next?"}); err != nil {
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
	if got, err := second.GetItem(ctx, "omib-1"); err != nil || got.SourceID != "omib" || got.Stem != "next?" {
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
