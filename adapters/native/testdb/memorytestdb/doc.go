// Package memorytestdb is an in-memory adapter implementing the "TestDb"
// repositories — ports.TestRepository, ports.ItemRepository and
// ports.SessionRepository — from a single map-backed Store. It is the default,
// dependency-free persistence used in tests and the CLI, and the behavioural
// baseline the sqlite adapter is proven against (ports/testdbtest).
//
// It stores and returns snapshots by value and is safe for concurrent use.
package memorytestdb
