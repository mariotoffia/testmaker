// Package memorycatalog is an in-memory adapter implementing
// ports.SourceRepository. It is the default, dependency-free catalogue backing
// used in tests and for the file-seeded catalogue at runtime.
//
// It returns deep copies of stored snapshots so callers can never mutate
// internal state, and is safe for concurrent use.
package memorycatalog
