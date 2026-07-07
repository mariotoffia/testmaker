package memorycatalog

import (
	"context"
	"sort"
	"sync"

	"github.com/mariotoffia/testmaker/domain/source"
)

// Store is an in-memory ports.SourceRepository, safe for concurrent use.
type Store struct {
	mu    sync.RWMutex
	items map[source.SourceID]source.Snapshot
}

// NewStore returns an empty in-memory catalogue.
func NewStore() *Store {
	return &Store{items: make(map[source.SourceID]source.Snapshot)}
}

// Get returns the snapshot for id or source.ErrUnknownSource.
func (s *Store) Get(_ context.Context, id source.SourceID) (source.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap, ok := s.items[id]
	if !ok {
		return source.Snapshot{}, source.ErrUnknownSource.With("id", string(id))
	}
	return clone(snap), nil
}

// List returns all snapshots matching the filter, ordered by id.
func (s *Store) List(_ context.Context, filter source.SourceFilter) ([]source.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]source.Snapshot, 0, len(s.items))
	for _, snap := range s.items {
		if filter.Matches(snap) {
			out = append(out, clone(snap))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Count returns the number of stored sources.
func (s *Store) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items), nil
}

// Put inserts or replaces a source by id.
func (s *Store) Put(_ context.Context, snap source.Snapshot) error {
	if snap.ID == "" {
		return source.ErrInvalidSource.WithMessage("snapshot id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[snap.ID] = clone(snap)
	return nil
}

// Delete removes a source by id; deleting an absent id is not an error.
func (s *Store) Delete(_ context.Context, id source.SourceID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
	return nil
}

// clone deep-copies a snapshot via the domain aggregate so no internal slice is
// ever shared with a caller.
func clone(s source.Snapshot) source.Snapshot {
	return source.RehydrateFromSnapshot(s).Snapshot()
}
