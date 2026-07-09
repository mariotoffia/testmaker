package fileprompts

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mariotoffia/testmaker/domain/prompt"
	"github.com/mariotoffia/testmaker/domain/shared"
)

// ErrStore marks a failure of the filesystem backend itself — creating the base
// directory, listing it, or reading/writing a prompt file. Semantic outcomes
// surface as the prompt-context sentinels (an unknown id is
// prompt.ErrUnknownPrompt, an invalid/corrupt prompt is prompt.ErrInvalidPrompt);
// ErrStore is everything else. The wrapped cause stays reachable through Unwrap.
var ErrStore = &shared.TestmakerError{
	Code: "fileprompts.store", Class: shared.ClassUnavailable, Message: "fileprompts store error",
}

const fileExt = ".yaml"

// Store is a filesystem-backed prompt repository rooted at a directory.
type Store struct {
	dir string
}

// Open returns a Store rooted at dir, creating it (and parents) if absent. A
// creation failure is ErrStore.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, ErrStore.WithMessagef("create prompt dir %q", dir).Wrap(err)
	}
	return &Store{dir: dir}, nil
}

// Get reads the prompt stored under id, or prompt.ErrUnknownPrompt if absent. A
// present-but-malformed file is prompt.ErrInvalidPrompt.
func (s *Store) Get(_ context.Context, id prompt.PromptID) (prompt.Snapshot, error) {
	if !isSlug(id) {
		// An id that could never name a file this store minted cannot exist here
		// (this also blocks a path-traversal id like "../secret").
		return prompt.Snapshot{}, prompt.ErrUnknownPrompt.With("id", string(id))
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, string(id)+fileExt))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return prompt.Snapshot{}, prompt.ErrUnknownPrompt.With("id", string(id))
		}
		return prompt.Snapshot{}, ErrStore.WithMessagef("read prompt %q", id).Wrap(err)
	}
	return decode(raw, string(id))
}

// ByPurpose returns the active prompt for the purpose: highest Version wins,
// ties broken by lexically smallest ID. No prompt for the purpose is
// prompt.ErrUnknownPrompt.
func (s *Store) ByPurpose(ctx context.Context, p prompt.Purpose) (prompt.Snapshot, error) {
	all, err := s.List(ctx)
	if err != nil {
		return prompt.Snapshot{}, err
	}
	var best *prompt.Snapshot
	for i := range all {
		if all[i].Purpose != p {
			continue
		}
		if best == nil || moreActive(all[i], *best) {
			best = &all[i]
		}
	}
	if best == nil {
		return prompt.Snapshot{}, prompt.ErrUnknownPrompt.With("purpose", string(p))
	}
	return *best, nil
}

// List reads and validates every prompt file under the directory, ordered by id.
// A single malformed file fails the whole call (fail loud, like the catalogue
// loader): a broken seed prompt must not be silently skipped.
func (s *Store) List(_ context.Context) ([]prompt.Snapshot, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, ErrStore.WithMessagef("list prompt dir %q", s.dir).Wrap(err)
	}
	out := make([]prompt.Snapshot, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != fileExt {
			continue
		}
		id := strings.TrimSuffix(e.Name(), fileExt)
		if !isSlug(prompt.PromptID(id)) {
			// A non-slug filename can never be addressed by Get (which rejects
			// non-slug ids as unknown), so it is not a prompt this store manages
			// — skip it rather than surface an id Get would then fail to resolve.
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if rerr != nil {
			return nil, ErrStore.WithMessagef("read prompt file %q", e.Name()).Wrap(rerr)
		}
		snap, derr := decode(raw, id)
		if derr != nil {
			return nil, derr
		}
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Put validates the snapshot through the domain constructor, then writes it as
// <dir>/<id>.yaml atomically (temp file + rename). An empty or non-slug id is
// prompt.ErrInvalidPrompt; an I/O failure is ErrStore.
func (s *Store) Put(_ context.Context, snap prompt.Snapshot) error {
	if _, err := prompt.NewPrompt(specOf(snap)); err != nil {
		return err
	}
	if !isSlug(snap.ID) {
		return prompt.ErrInvalidPrompt.WithMessagef("id %q must be a kebab-case slug", snap.ID)
	}
	body, err := yaml.Marshal(wireOf(snap))
	if err != nil {
		return prompt.ErrInvalidPrompt.WithMessage("marshal prompt").Wrap(err)
	}
	return s.writeAtomic(string(snap.ID)+fileExt, body)
}

// Delete removes the prompt file for id; an absent id (or a non-slug id that
// could never have been stored) is not an error.
func (s *Store) Delete(_ context.Context, id prompt.PromptID) error {
	if !isSlug(id) {
		return nil
	}
	if err := os.Remove(filepath.Join(s.dir, string(id)+fileExt)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ErrStore.WithMessagef("delete prompt %q", id).Wrap(err)
	}
	return nil
}

// writeAtomic writes body to a temp file in s.dir and renames it onto name.
// Same-directory rename is atomic on POSIX, so a reader never sees a torn file.
func (s *Store) writeAtomic(name string, body []byte) error {
	tmp, err := os.CreateTemp(s.dir, "put-*.tmp")
	if err != nil {
		return ErrStore.WithMessagef("create temp for %q", name).Wrap(err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return ErrStore.WithMessagef("write %q", name).Wrap(err)
	}
	if err := tmp.Close(); err != nil {
		return ErrStore.WithMessagef("close temp for %q", name).Wrap(err)
	}
	if err := os.Rename(tmpName, filepath.Join(s.dir, name)); err != nil {
		return ErrStore.WithMessagef("commit %q", name).Wrap(err)
	}
	return nil
}

// --- wire DTO (the only place that knows the on-disk schema) ----------------

type wirePrompt struct {
	ID       string   `yaml:"id"`
	Version  int      `yaml:"version"`
	Purpose  string   `yaml:"purpose"`
	Params   []string `yaml:"params,omitempty"`
	Template string   `yaml:"template"`
	Notes    string   `yaml:"notes,omitempty"`
}

func wireOf(s prompt.Snapshot) wirePrompt {
	return wirePrompt{
		ID: string(s.ID), Version: s.Version, Purpose: string(s.Purpose),
		Params: s.Params, Template: s.Template, Notes: s.Notes,
	}
}

func specOf(s prompt.Snapshot) prompt.Spec {
	return prompt.Spec(s)
}

// decode parses one prompt YAML file and validates it through the domain
// constructor, so a file on disk can never yield an unvalidated snapshot. wantID
// is the id implied by the file name; a file whose declared id disagrees is
// rejected so List can never surface an id that Get (which reads <id>.yaml)
// would then fail to find.
func decode(raw []byte, wantID string) (prompt.Snapshot, error) {
	var w wirePrompt
	if err := yaml.Unmarshal(raw, &w); err != nil {
		return prompt.Snapshot{}, prompt.ErrInvalidPrompt.WithMessagef("parse prompt %q", wantID).Wrap(err)
	}
	if w.ID != wantID {
		return prompt.Snapshot{}, prompt.ErrInvalidPrompt.WithMessagef(
			"prompt file %q.yaml declares mismatched id %q", wantID, w.ID).With("id", wantID)
	}
	p, verr := prompt.NewPrompt(prompt.Spec{
		ID: prompt.PromptID(w.ID), Version: w.Version, Purpose: prompt.Purpose(w.Purpose),
		Template: w.Template, Params: w.Params, Notes: w.Notes,
	})
	if verr != nil {
		return prompt.Snapshot{}, verr
	}
	return p.Snapshot(), nil
}

// moreActive reports whether a should outrank b as the active prompt: a higher
// Version wins; on an equal Version the smaller ID wins.
// ponytail: duplicated from memoryprompts — 4 lines, and the prompttest suite
// pins both adapters to the same ordering. A shared helper would need a new
// package for two callers.
func moreActive(a, b prompt.Snapshot) bool {
	if a.Version != b.Version {
		return a.Version > b.Version
	}
	return a.ID < b.ID
}

// isSlug reports whether id is a filesystem-safe kebab-case slug
// (^[a-z0-9]([a-z0-9-]*[a-z0-9])?$), so an id is always a plain file name under
// s.dir — no separators, no "..", no absolute path can pass.
func isSlug(id prompt.PromptID) bool {
	s := string(id)
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
		if !ok {
			return false
		}
	}
	return s[0] != '-' && s[len(s)-1] != '-'
}
