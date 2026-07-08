package authoring_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/app/authoring"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// --- in-process fakes (no adapter imports, no I/O) -------------------------

// fakeGenerator returns a preset batch (or error) and records the last spec.
type fakeGenerator struct {
	snaps    []item.ItemSnapshot
	err      error
	lastSpec ports.GenerateSpec
}

func (f *fakeGenerator) Generate(_ context.Context, spec ports.GenerateSpec) ([]item.ItemSnapshot, error) {
	f.lastSpec = spec
	if f.err != nil {
		return nil, f.err
	}
	return f.snaps, nil
}

// fakeBank is an in-memory ports.ItemRepository that can be told to fail writes
// after a set number of successful saves.
type fakeBank struct {
	saved     map[item.ItemID]item.ItemSnapshot
	failAfter int // 0 = never fail
	writeErr  error
}

func newFakeBank() *fakeBank { return &fakeBank{saved: map[item.ItemID]item.ItemSnapshot{}} }

func (b *fakeBank) SaveItem(_ context.Context, snap item.ItemSnapshot) error {
	if b.failAfter > 0 && len(b.saved) >= b.failAfter {
		return b.writeErr
	}
	b.saved[snap.ID] = snap
	return nil
}

func (b *fakeBank) GetItem(_ context.Context, id item.ItemID) (item.ItemSnapshot, error) {
	s, ok := b.saved[id]
	if !ok {
		return item.ItemSnapshot{}, item.ErrUnknownItem
	}
	return s, nil
}

func (b *fakeBank) ListItems(_ context.Context, filter item.ItemFilter) ([]item.ItemSnapshot, error) {
	var out []item.ItemSnapshot
	for _, s := range b.saved {
		if filter.Matches(s) {
			out = append(out, s)
		}
	}
	return out, nil
}

// sampleSnapshot builds a valid multiple-choice snapshot for the fakes.
func sampleSnapshot(t *testing.T, id item.ItemID) item.ItemSnapshot {
	t.Helper()
	it, err := item.NewItem(item.ItemSpec{
		ID:           id,
		Provenance:   item.Provenance{SourceID: "rulegen", Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
		TestType:     "A2",
		Stimulus:     []item.StimulusPart{{Text: "which completes the matrix?"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:  item.AnswerKey{OptionID: "b"},
		Difficulty: item.Difficulty{Band: 2},
	})
	if err != nil {
		t.Fatalf("sample snapshot: %v", err)
	}
	return it.Snapshot()
}

// --- tests -----------------------------------------------------------------

var (
	_ ports.Generator      = (*fakeGenerator)(nil)
	_ ports.ItemRepository = (*fakeBank)(nil)
)

func TestGenerateStoresEveryItem(t *testing.T) {
	ctx := context.Background()
	gen := &fakeGenerator{snaps: []item.ItemSnapshot{
		sampleSnapshot(t, "gen-A2-1-0"),
		sampleSnapshot(t, "gen-A2-1-1"),
		sampleSnapshot(t, "gen-A2-1-2"),
	}}
	bank := newFakeBank()
	svc := authoring.NewService(gen, bank)

	rep, err := svc.Generate(ctx, ports.GenerateSpec{TestType: "A2", Difficulty: 2, Count: 3, Seed: 1})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if rep.Generated != 3 || rep.Saved != 3 {
		t.Fatalf("report = %+v, want generated=saved=3", rep)
	}
	if len(bank.saved) != 3 {
		t.Fatalf("bank has %d items, want 3", len(bank.saved))
	}
	if gen.lastSpec.Count != 3 {
		t.Fatalf("generator received count %d, want 3", gen.lastSpec.Count)
	}
}

func TestGeneratePropagatesGeneratorError(t *testing.T) {
	sentinel := &shared.TestmakerError{Code: "rulegen.unsupported_type", Class: shared.ClassUnsupported, Message: "boom"}
	svc := authoring.NewService(&fakeGenerator{err: sentinel}, newFakeBank())

	_, err := svc.Generate(context.Background(), ports.GenerateSpec{TestType: "B1", Difficulty: 1, Count: 1, Seed: 1})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want generator error propagated, got %v", err)
	}
}

func TestGenerateAbortsOnStoreError(t *testing.T) {
	writeErr := &shared.TestmakerError{Code: "testdb.write", Class: shared.ClassUnavailable, Message: "disk full"}
	gen := &fakeGenerator{snaps: []item.ItemSnapshot{
		sampleSnapshot(t, "gen-A2-1-0"),
		sampleSnapshot(t, "gen-A2-1-1"),
		sampleSnapshot(t, "gen-A2-1-2"),
	}}
	bank := newFakeBank()
	bank.failAfter = 1
	bank.writeErr = writeErr
	svc := authoring.NewService(gen, bank)

	rep, err := svc.Generate(context.Background(), ports.GenerateSpec{TestType: "A2", Difficulty: 2, Count: 3, Seed: 1})
	if !errors.Is(err, writeErr) {
		t.Fatalf("want store error, got %v", err)
	}
	if rep.Saved != 1 {
		t.Fatalf("saved = %d, want 1 (aborted after first write)", rep.Saved)
	}
}

func TestGenerateWithoutGeneratorErrors(t *testing.T) {
	svc := authoring.NewService(nil, newFakeBank())
	_, err := svc.Generate(context.Background(), ports.GenerateSpec{TestType: "A2", Difficulty: 1, Count: 1, Seed: 1})
	if !errors.Is(err, authoring.ErrNoGenerator) {
		t.Fatalf("want ErrNoGenerator, got %v", err)
	}
}

func TestAuthorValidatesAndStores(t *testing.T) {
	ctx := context.Background()
	bank := newFakeBank()
	svc := authoring.NewService(nil, bank) // author path needs no generator

	id, err := svc.Author(ctx, item.ItemSpec{
		ID:           "authored-1",
		Provenance:   item.Provenance{SourceID: "hand", Origin: item.OriginAuthored, Redistributable: shared.RedistYes},
		TestType:     "A5",
		Stimulus:     []item.StimulusPart{{Text: "All cats are mammals. Fluffy is a cat. Fluffy is a mammal."}},
		AnswerFormat: item.FormatTrueFalseCannotSay,
		AnswerKey:    item.AnswerKey{Verdict: item.VerdictTrue},
		Difficulty:   item.Difficulty{Band: 1},
	})
	if err != nil {
		t.Fatalf("author: %v", err)
	}
	if id != "authored-1" {
		t.Fatalf("id = %q, want authored-1", id)
	}
	got, err := bank.GetItem(ctx, "authored-1")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Provenance.Origin != item.OriginAuthored {
		t.Fatalf("stored origin = %q, want authored", got.Provenance.Origin)
	}
}

func TestAuthorRejectsInvalidSpec(t *testing.T) {
	bank := newFakeBank()
	svc := authoring.NewService(nil, bank)

	_, err := svc.Author(context.Background(), item.ItemSpec{ID: ""}) // empty id is invalid
	if !errors.Is(err, item.ErrInvalidItem) {
		t.Fatalf("want ErrInvalidItem, got %v", err)
	}
	if len(bank.saved) != 0 {
		t.Fatalf("invalid author stored %d items, want 0", len(bank.saved))
	}
}
