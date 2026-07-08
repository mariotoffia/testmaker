// Package sqlitetestdb is a SQLite-backed adapter implementing the "TestDb"
// repositories — ports.TestRepository, ports.ItemRepository and
// ports.SessionRepository — from a single database file (or an in-memory DB).
// It is the durable sibling of memorytestdb: both are proven against the same
// ports/testdbtest conformance suites, so memory and SQLite are interchangeable.
//
// The backing driver is modernc.org/sqlite — pure Go, no cgo — and every line
// that knows the driver or the SQL dialect (the blank driver import, the schema
// migrations and all queries) is isolated in acl_sqlite.go; store.go holds only
// the port-facing orchestration. Snapshots cross the ports by value; because
// every read scans SQL rows into fresh structs, returned snapshots never alias
// stored state.
package sqlitetestdb
