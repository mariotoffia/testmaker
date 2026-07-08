package memorytestdb_test

import (
	"context"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/ports/testdbtest"
)

// Compile-time proof that Store satisfies every TestDb port (kept in _test.go so
// the production package imports no ports package, per the arch rules).
var (
	_ ports.TestRepository    = (*memorytestdb.Store)(nil)
	_ ports.ItemRepository    = (*memorytestdb.Store)(nil)
	_ ports.SessionRepository = (*memorytestdb.Store)(nil)
)

func TestTestRepositoryConformance(t *testing.T) {
	testdbtest.RunTestRepositoryTests(t, func() ports.TestRepository {
		return memorytestdb.NewStore()
	})
}

func TestItemRepositoryConformance(t *testing.T) {
	testdbtest.RunItemRepositoryTests(t, func() ports.ItemRepository {
		return memorytestdb.NewStore()
	})
}

func TestSessionRepositoryConformance(t *testing.T) {
	testdbtest.RunSessionRepositoryTests(t, func() ports.SessionRepository {
		return memorytestdb.NewStore()
	})
}

// TestRepositoriesShareNoKeyspace proves the three repositories backed by one
// Store keep independent keyspaces: the same id in each repo does not collide,
// and deleting a test leaves an item/session with the same id untouched. This
// guards a future refactor toward a single shared map or a cross-map Delete.
func TestRepositoriesShareNoKeyspace(t *testing.T) {
	ctx := context.Background()
	s := memorytestdb.NewStore()

	if err := s.SaveTest(ctx, testset.TestSnapshot{ID: "x", Title: "T"}); err != nil {
		t.Fatalf("save test: %v", err)
	}
	if err := s.SaveItem(ctx, item.ItemSnapshot{ID: "x", Stem: "I"}); err != nil {
		t.Fatalf("save item: %v", err)
	}
	if err := s.SaveSession(ctx, session.SessionSnapshot{ID: "x", TestID: "T"}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	if got, err := s.GetTest(ctx, "x"); err != nil || got.Title != "T" {
		t.Fatalf("test keyspace: got %+v, err %v", got, err)
	}
	if got, err := s.GetItem(ctx, "x"); err != nil || got.Stem != "I" {
		t.Fatalf("item keyspace: got %+v, err %v", got, err)
	}
	if got, err := s.GetSession(ctx, "x"); err != nil || got.TestID != "T" {
		t.Fatalf("session keyspace: got %+v, err %v", got, err)
	}

	// deleting the test must not touch the item/session with the same id
	if err := s.DeleteTest(ctx, "x"); err != nil {
		t.Fatalf("delete test: %v", err)
	}
	if _, err := s.GetItem(ctx, "x"); err != nil {
		t.Fatalf("item removed by test delete: %v", err)
	}
	if _, err := s.GetSession(ctx, "x"); err != nil {
		t.Fatalf("session removed by test delete: %v", err)
	}
}
