package sqlitetestdb_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

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
	// A started, partially-answered session (plan slices, captured responses,
	// normalized timestamps) exercises the JSON session blob across a real
	// Close/reopen — the durability proof a memory store cannot give.
	wantSession := mustSessionSnapshot(t)
	if err := first.SaveSession(ctx, wantSession); err != nil {
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
	if got, err := second.GetSession(ctx, wantSession.ID); err != nil || !reflect.DeepEqual(got, wantSession) {
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

// TestMigrationV1UpgradePreservesLegacyRows proves upgrading a real v1 database
// to the current schema does not destroy its data: the pre-Block-4 items(id,
// source_id, stem) rows are quarantined into items_v1_legacy by migration 2, the
// pre-Block-7 tests(id, title) rows into tests_v1_legacy by migration 4, and the
// pre-Block-8 sessions(id, test_id) rows into sessions_v1_legacy by migration 5 —
// none is dropped. It is the regression lock for the data-preservation fix — a
// revert to `DROP TABLE` in any of those migrations makes a legacy-row assertion
// fail. The driver is registered by the package under test, so a bare handle can
// seed the frozen v1 schema (migration 1 is append-only, so that shape never
// changes).
func TestMigrationV1UpgradePreservesLegacyRows(t *testing.T) {
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

	// Open runs migrations 2 (RENAME items -> items_v1_legacy, CREATE new items),
	// 3 (add item query columns), 4 (RENAME tests -> tests_v1_legacy, CREATE new
	// tests) and 5 (RENAME sessions -> sessions_v1_legacy, CREATE new sessions).
	store := mustOpen(t, path)

	// None of the v1 rows can become a valid snapshot, so every migration
	// quarantines its table: the store no longer serves any of them under the old
	// key.
	if _, err := store.GetTest(ctx, "gia"); !errors.Is(err, testset.ErrUnknownTest) {
		t.Fatalf("quarantined v1 test: want ErrUnknownTest, got %v", err)
	}
	if _, err := store.GetSession(ctx, "sess-0"); !errors.Is(err, session.ErrUnknownSession) {
		t.Fatalf("quarantined v1 session: want ErrUnknownSession, got %v", err)
	}

	// The old rows must survive in the quarantine tables (the DROP-revert
	// tripwire) and the schema must have advanced to the latest version. Read
	// through a separate handle, then release it before writing so the
	// single-connection store never contends with it.
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen raw: %v", err)
	}
	var sourceID, stem, title, sessionTestID string
	var version int
	if err := legacy.QueryRow(`SELECT source_id, stem FROM items_v1_legacy WHERE id = 'old-1'`).Scan(&sourceID, &stem); err != nil {
		_ = legacy.Close()
		t.Fatalf("legacy item row lost: %v", err)
	}
	if err := legacy.QueryRow(`SELECT title FROM tests_v1_legacy WHERE id = 'gia'`).Scan(&title); err != nil {
		_ = legacy.Close()
		t.Fatalf("legacy test row lost: %v", err)
	}
	if err := legacy.QueryRow(`SELECT test_id FROM sessions_v1_legacy WHERE id = 'sess-0'`).Scan(&sessionTestID); err != nil {
		_ = legacy.Close()
		t.Fatalf("legacy session row lost: %v", err)
	}
	if err := legacy.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		_ = legacy.Close()
		t.Fatalf("read version: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy handle: %v", err)
	}
	if sourceID != "omib" || stem != "legacy stem" {
		t.Fatalf("legacy item row corrupted: source_id=%q stem=%q", sourceID, stem)
	}
	if title != "GIA" {
		t.Fatalf("legacy test row corrupted: title=%q", title)
	}
	if sessionTestID != "gia" {
		t.Fatalf("legacy session row corrupted: test_id=%q", sessionTestID)
	}
	if version != 5 {
		t.Fatalf("user_version = %d, want 5", version)
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

// TestMigrationV2ToV3AddsQueryColumns proves migration 3's generated query
// columns are computed from the JSON snapshot of pre-existing v2 rows (which held
// only id + snapshot), so a filter over every one of them — family, test type,
// origin, redistributable and difficulty, none of which the WHERE reads from the
// blob — returns the row after upgrade. This is the runnable check for the
// json_extract projection and guards each column's JSON path; a wrong path leaves
// that column NULL and drops the row.
func TestMigrationV2ToV3AddsQueryColumns(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v2.sqlite")

	want := mustItemSnapshot(t) // A2 -> logical family, band 3, fetched, conditional
	blob, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE items (id TEXT PRIMARY KEY, snapshot TEXT NOT NULL) STRICT`); err != nil {
		_ = raw.Close()
		t.Fatalf("seed v2 items table: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE tests (id TEXT PRIMARY KEY, title TEXT NOT NULL) STRICT`); err != nil {
		_ = raw.Close()
		t.Fatalf("seed v2 tests table: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, test_id TEXT NOT NULL) STRICT`); err != nil {
		_ = raw.Close()
		t.Fatalf("seed v2 sessions table: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO items (id, snapshot) VALUES (?, ?)`, string(want.ID), string(blob)); err != nil {
		_ = raw.Close()
		t.Fatalf("seed v2 row: %v", err)
	}
	if _, err := raw.Exec(`PRAGMA user_version = 2`); err != nil {
		_ = raw.Close()
		t.Fatalf("seed user_version: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// Open runs migration 3 (add generated query columns).
	store := mustOpen(t, path)

	// A filter over all five query columns can only match if each was computed
	// from the blob; the query never reads the JSON snapshot itself.
	got, err := store.ListItems(ctx, item.ItemFilter{
		Families:        []shared.AbilityFamily{shared.FamilyLogical},
		TestTypes:       []shared.TestTypeCode{"A2"},
		Origins:         []item.Origin{item.OriginFetched},
		Redistributable: []shared.Redistributable{shared.RedistConditional},
		MinDifficulty:   3,
		MaxDifficulty:   3,
	})
	if err != nil {
		t.Fatalf("list after upgrade: %v", err)
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("row not matched by column filter after upgrade: got %+v", got)
	}
}

// TestListNullDifficultyBandMirrorsGoZeroValue proves the COALESCE in the
// difficulty predicates makes a row whose blob is missing Difficulty.Band — so
// the generated difficulty_band column resolves to NULL — behave exactly as
// item.ItemFilter.Matches treats it: the unmarshalled snapshot has Band == 0, so
// a MaxDifficulty filter includes it and a MinDifficulty >= 1 filter excludes it.
// Without COALESCE the NULL column would make both predicates UNKNOWN and wrongly
// drop the row from the MaxDifficulty query. Such a blob is not producible via
// NewItem/SaveItem (Band >= 1 is enforced and always marshalled); the test
// hand-edits the JSON to lock the parity the COALESCE exists for, cross-checking
// every expectation against Matches as the oracle.
func TestListNullDifficultyBandMirrorsGoZeroValue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nullband.sqlite")

	// Build a valid snapshot, then strip Difficulty.Band from its JSON so the
	// generated column is NULL (json_extract of a missing path).
	full := mustItemSnapshot(t) // family logical, band 3
	blob, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	diff, ok := doc["Difficulty"].(map[string]any)
	if !ok {
		t.Fatalf("Difficulty not an object: %T", doc["Difficulty"])
	}
	delete(diff, "Band")
	stripped, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-marshal stripped: %v", err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE items (id TEXT PRIMARY KEY, snapshot TEXT NOT NULL) STRICT`); err != nil {
		_ = raw.Close()
		t.Fatalf("seed v2 items table: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE tests (id TEXT PRIMARY KEY, title TEXT NOT NULL) STRICT`); err != nil {
		_ = raw.Close()
		t.Fatalf("seed v2 tests table: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, test_id TEXT NOT NULL) STRICT`); err != nil {
		_ = raw.Close()
		t.Fatalf("seed v2 sessions table: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO items (id, snapshot) VALUES (?, ?)`, string(full.ID), string(stripped)); err != nil {
		_ = raw.Close()
		t.Fatalf("seed row: %v", err)
	}
	if _, err := raw.Exec(`PRAGMA user_version = 2`); err != nil {
		_ = raw.Close()
		t.Fatalf("seed user_version: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	store := mustOpen(t, path) // runs migration 3 -> difficulty_band is NULL for this row

	// The read-back snapshot's Band must be the JSON zero value; this is what
	// Matches sees and what COALESCE must mirror.
	oracle, err := store.GetItem(ctx, full.ID)
	if err != nil {
		t.Fatalf("get stripped item: %v", err)
	}
	if oracle.Difficulty.Band != 0 {
		t.Fatalf("stripped Band = %d, want 0 (json zero value)", oracle.Difficulty.Band)
	}

	// MaxDifficulty: COALESCE(NULL,0) <= 5 -> 0 <= 5 -> included (mirrors Matches).
	maxFilter := item.ItemFilter{MaxDifficulty: 5}
	if !maxFilter.Matches(oracle) {
		t.Fatalf("oracle: Matches(MaxDifficulty:5) = false, want true")
	}
	if list, err := store.ListItems(ctx, maxFilter); err != nil {
		t.Fatalf("list MaxDifficulty: %v", err)
	} else if len(list) != 1 || !reflect.DeepEqual(list[0], oracle) {
		t.Fatalf("MaxDifficulty:5 returned %d rows, want the null-band row (COALESCE null->0)", len(list))
	}

	// MinDifficulty: COALESCE(NULL,0) >= 1 -> 0 >= 1 -> excluded (mirrors Matches).
	minFilter := item.ItemFilter{MinDifficulty: 1}
	if minFilter.Matches(oracle) {
		t.Fatalf("oracle: Matches(MinDifficulty:1) = true, want false")
	}
	if list, err := store.ListItems(ctx, minFilter); err != nil {
		t.Fatalf("list MinDifficulty: %v", err)
	} else if len(list) != 0 {
		t.Fatalf("MinDifficulty:1 returned %d rows, want 0", len(list))
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

// mustSessionSnapshot builds a started, partially-answered session snapshot via
// the real aggregate, so the durability test round-trips the same nested plan,
// captured responses and UTC-normalized timestamps a real attempt would persist.
func mustSessionSnapshot(t *testing.T) session.SessionSnapshot {
	t.Helper()
	start := time.Date(2024, 6, 7, 8, 9, 10, 0, time.UTC)
	sess, err := session.NewSession(session.SessionSpec{
		ID:     "sess-1",
		TestID: "gia",
		Policy: session.PolicyFixedIncreasing,
		Timing: session.Timing{Total: 20 * time.Minute},
		Sections: []session.PlanSection{{
			Title:  "Reasoning",
			Family: shared.FamilyLogical,
			Timing: session.Timing{PerItem: 60 * time.Second},
			Items:  []session.PlanItem{{ItemID: "log-1", Difficulty: 1}, {ItemID: "log-2", Difficulty: 2}},
		}},
	})
	if err != nil {
		t.Fatalf("build session: %v", err)
	}
	if err := sess.Begin(start); err != nil {
		t.Fatalf("begin session: %v", err)
	}
	if err := sess.Record("log-1", session.Answer{OptionID: "c"}, true, start.Add(15*time.Second)); err != nil {
		t.Fatalf("record session: %v", err)
	}
	return sess.Snapshot()
}
