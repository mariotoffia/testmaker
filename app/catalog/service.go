// Package catalog is the application service (use-case layer) for the source
// catalogue. It orchestrates the CatalogLoader (driving) and SourceRepository
// (driven) ports; it holds no wire-format or storage knowledge of its own.
package catalog

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// Service is the source-catalogue application service.
type Service struct {
	repo   ports.SourceRepository
	loader ports.CatalogLoader
}

// NewService wires a repository and a loader into the service.
func NewService(repo ports.SourceRepository, loader ports.CatalogLoader) *Service {
	return &Service{repo: repo, loader: loader}
}

// Sync loads the catalogue through the loader and upserts every source into the
// repository, returning the number of sources synced.
func (s *Service) Sync(ctx context.Context) (int, error) {
	snaps, err := s.loader.Load(ctx)
	if err != nil {
		return 0, err
	}
	for _, snap := range snaps {
		if err := s.repo.Put(ctx, snap); err != nil {
			return 0, err
		}
	}
	return len(snaps), nil
}

// Get returns a single source by id.
func (s *Service) Get(ctx context.Context, id source.SourceID) (source.Snapshot, error) {
	return s.repo.Get(ctx, id)
}

// List returns sources matching the filter.
func (s *Service) List(ctx context.Context, filter source.SourceFilter) ([]source.Snapshot, error) {
	return s.repo.List(ctx, filter)
}

// Count returns the number of catalogued sources.
func (s *Service) Count(ctx context.Context) (int, error) {
	return s.repo.Count(ctx)
}

// Reusable returns sources whose items may be reused (redistributable yes or
// conditional) — the safe set to ingest into the item bank.
func (s *Service) Reusable(ctx context.Context) ([]source.Snapshot, error) {
	return s.repo.List(ctx, source.SourceFilter{
		Redistributable: []source.Redistributable{source.RedistYes, source.RedistConditional},
	})
}

// Generators returns sources that procedurally generate items (unlimited,
// IP-free) — the backbone of the designer/generator subsystem.
func (s *Service) Generators(ctx context.Context) ([]source.Snapshot, error) {
	return s.repo.List(ctx, source.SourceFilter{GeneratorsOnly: true})
}
