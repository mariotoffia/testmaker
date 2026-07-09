package memoryprompts

import (
	"context"
	"sort"
	"sync"

	"github.com/mariotoffia/testmaker/domain/prompt"
)

// Store is an in-memory prompt repository, safe for concurrent use.
type Store struct {
	mu      sync.RWMutex
	prompts map[prompt.PromptID]prompt.Snapshot
}

// NewStore returns an empty in-memory prompt store.
func NewStore() *Store {
	return &Store{prompts: make(map[prompt.PromptID]prompt.Snapshot)}
}

// Get returns the snapshot for id or prompt.ErrUnknownPrompt.
func (s *Store) Get(_ context.Context, id prompt.PromptID) (prompt.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.prompts[id]
	if !ok {
		return prompt.Snapshot{}, prompt.ErrUnknownPrompt.With("id", string(id))
	}
	return clone(snap), nil
}

// ByPurpose returns the active prompt for the purpose: highest Version wins,
// ties broken by lexically smallest ID. No prompt for the purpose is
// prompt.ErrUnknownPrompt.
func (s *Store) ByPurpose(_ context.Context, p prompt.Purpose) (prompt.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var best *prompt.Snapshot
	for _, snap := range s.prompts {
		if snap.Purpose != p {
			continue
		}
		if best == nil || moreActive(snap, *best) {
			cur := snap
			best = &cur
		}
	}
	if best == nil {
		return prompt.Snapshot{}, prompt.ErrUnknownPrompt.With("purpose", string(p))
	}
	return clone(*best), nil
}

// List returns all stored prompts, ordered by id for deterministic output.
func (s *Store) List(_ context.Context) ([]prompt.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]prompt.Snapshot, 0, len(s.prompts))
	for _, snap := range s.prompts {
		out = append(out, clone(snap))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Put inserts or replaces a prompt by id. An empty id is prompt.ErrInvalidPrompt.
func (s *Store) Put(_ context.Context, snap prompt.Snapshot) error {
	if snap.ID == "" {
		return prompt.ErrInvalidPrompt.WithMessage("snapshot id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[snap.ID] = clone(snap)
	return nil
}

// Delete removes a prompt by id; deleting an absent id is not an error.
func (s *Store) Delete(_ context.Context, id prompt.PromptID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.prompts, id)
	return nil
}

// moreActive reports whether a should outrank b as the active prompt: a higher
// Version wins; on an equal Version the smaller ID wins.
func moreActive(a, b prompt.Snapshot) bool {
	if a.Version != b.Version {
		return a.Version > b.Version
	}
	return a.ID < b.ID
}

// clone deep-copies a snapshot via the domain aggregate so no internal slice is
// ever shared with a caller.
func clone(s prompt.Snapshot) prompt.Snapshot {
	return prompt.RehydrateFromSnapshot(s).Snapshot()
}
