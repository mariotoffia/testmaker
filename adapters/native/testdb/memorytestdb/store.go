package memorytestdb

import (
	"context"
	"sort"
	"sync"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/testset"
)

// Store is an in-memory TestDb backing the Test, Item and Session repositories,
// safe for concurrent use.
//
// Every snapshot carries slices (test sections/item-refs, item stimulus/options,
// session plan/responses), so reads and writes are deep-copied through the
// aggregate (RehydrateFromSnapshot(...).Snapshot()) to keep stored state from
// aliasing caller memory — the memorycatalog clone pattern.
type Store struct {
	mu       sync.RWMutex
	tests    map[testset.TestID]testset.TestSnapshot
	items    map[item.ItemID]item.ItemSnapshot
	sessions map[session.SessionID]session.SessionSnapshot
}

// NewStore returns an empty in-memory TestDb.
func NewStore() *Store {
	return &Store{
		tests:    make(map[testset.TestID]testset.TestSnapshot),
		items:    make(map[item.ItemID]item.ItemSnapshot),
		sessions: make(map[session.SessionID]session.SessionSnapshot),
	}
}

// --- TestRepository ---

// cloneTest deep-copies a snapshot via the aggregate so stored state never
// aliases (and is never aliased by) caller-held section/item slices.
func cloneTest(snap testset.TestSnapshot) testset.TestSnapshot {
	return testset.RehydrateFromSnapshot(snap).Snapshot()
}

// SaveTest inserts or replaces a test by id.
func (s *Store) SaveTest(_ context.Context, snap testset.TestSnapshot) error {
	if snap.ID == "" {
		return testset.ErrInvalidTest.WithMessage("snapshot id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tests[snap.ID] = cloneTest(snap)
	return nil
}

// GetTest returns the snapshot for id or testset.ErrUnknownTest.
func (s *Store) GetTest(_ context.Context, id testset.TestID) (testset.TestSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap, ok := s.tests[id]
	if !ok {
		return testset.TestSnapshot{}, testset.ErrUnknownTest.With("id", string(id))
	}
	return cloneTest(snap), nil
}

// ListTests returns all tests, ordered by id. The filter is a placeholder shell
// (no test-query consumer yet), so every stored test is returned.
func (s *Store) ListTests(_ context.Context, _ testset.TestFilter) ([]testset.TestSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]testset.TestSnapshot, 0, len(s.tests))
	for _, snap := range s.tests {
		out = append(out, cloneTest(snap))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// DeleteTest removes a test by id; deleting an absent id is not an error.
func (s *Store) DeleteTest(_ context.Context, id testset.TestID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tests, id)
	return nil
}

// --- ItemRepository ---

// cloneItem deep-copies a snapshot via the aggregate so stored state never
// aliases (and is never aliased by) caller-held slices.
func cloneItem(snap item.ItemSnapshot) item.ItemSnapshot {
	return item.RehydrateFromSnapshot(snap).Snapshot()
}

// SaveItem inserts or replaces an item by id.
func (s *Store) SaveItem(_ context.Context, snap item.ItemSnapshot) error {
	if snap.ID == "" {
		return item.ErrInvalidItem.WithMessage("snapshot id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[snap.ID] = cloneItem(snap)
	return nil
}

// GetItem returns the snapshot for id or item.ErrUnknownItem.
func (s *Store) GetItem(_ context.Context, id item.ItemID) (item.ItemSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap, ok := s.items[id]
	if !ok {
		return item.ItemSnapshot{}, item.ErrUnknownItem.With("id", string(id))
	}
	return cloneItem(snap), nil
}

// ListItems returns the items matching filter, ordered by id.
func (s *Store) ListItems(_ context.Context, filter item.ItemFilter) ([]item.ItemSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]item.ItemSnapshot, 0, len(s.items))
	for _, snap := range s.items {
		if filter.Matches(snap) {
			out = append(out, cloneItem(snap))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// --- SessionRepository ---

// cloneSession deep-copies a snapshot via the aggregate so stored state never
// aliases (and is never aliased by) caller-held plan/response slices.
func cloneSession(snap session.SessionSnapshot) session.SessionSnapshot {
	return session.RehydrateFromSnapshot(snap).Snapshot()
}

// SaveSession inserts or replaces a session by id.
func (s *Store) SaveSession(_ context.Context, snap session.SessionSnapshot) error {
	if snap.ID == "" {
		return session.ErrInvalidSession.WithMessage("snapshot id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[snap.ID] = cloneSession(snap)
	return nil
}

// GetSession returns the snapshot for id or session.ErrUnknownSession.
func (s *Store) GetSession(_ context.Context, id session.SessionID) (session.SessionSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap, ok := s.sessions[id]
	if !ok {
		return session.SessionSnapshot{}, session.ErrUnknownSession.With("id", string(id))
	}
	return cloneSession(snap), nil
}
