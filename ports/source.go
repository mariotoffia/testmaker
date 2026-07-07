package ports

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/source"
)

// SourceCatalog is the read side of the source catalogue (driven port).
type SourceCatalog interface {
	// Get returns the source with the given id or ErrUnknownSource.
	Get(ctx context.Context, id source.SourceID) (source.Snapshot, error)
	// List returns all sources matching the filter (empty filter = all).
	List(ctx context.Context, filter source.SourceFilter) ([]source.Snapshot, error)
	// Count returns the number of catalogued sources.
	Count(ctx context.Context) (int, error)
}

// SourceRepository is the read/write side of the source catalogue (driven port).
type SourceRepository interface {
	SourceCatalog
	// Put inserts or replaces a source by id.
	Put(ctx context.Context, snap source.Snapshot) error
	// Delete removes a source by id (no error if absent).
	Delete(ctx context.Context, id source.SourceID) error
}

// CatalogLoader ingests a source catalogue from an external representation
// (e.g. a JSON/YAML file) into validated snapshots (driving port).
type CatalogLoader interface {
	// Load reads and validates the catalogue, returning its sources.
	Load(ctx context.Context) ([]source.Snapshot, error)
}
