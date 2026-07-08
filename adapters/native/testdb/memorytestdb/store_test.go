package memorytestdb_test

import (
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb"
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

// TestSharedKeyspace runs the shared-keyspace conformance check: one Store backs
// all three repositories with independent keyspaces (guards a future refactor
// toward a single shared map or a cross-map Delete).
func TestSharedKeyspace(t *testing.T) {
	testdbtest.RunSharedKeyspaceTests(t, func() testdbtest.TestDB {
		return memorytestdb.NewStore()
	})
}
