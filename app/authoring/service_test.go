package authoring_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
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
	listErr   error // non-nil = ListItems fails
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
	if b.listErr != nil {
		return nil, b.listErr
	}
	var out []item.ItemSnapshot
	for _, s := range b.saved {
		if filter.Matches(s) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (b *fakeBank) DeleteItem(_ context.Context, id item.ItemID) error {
	delete(b.saved, id)
	return nil
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
	svc := authoring.NewService(gen, bank, nil)

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
	svc := authoring.NewService(&fakeGenerator{err: sentinel}, newFakeBank(), nil)

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
	svc := authoring.NewService(gen, bank, nil)

	rep, err := svc.Generate(context.Background(), ports.GenerateSpec{TestType: "A2", Difficulty: 2, Count: 3, Seed: 1})
	if !errors.Is(err, writeErr) {
		t.Fatalf("want store error, got %v", err)
	}
	if rep.Saved != 1 {
		t.Fatalf("saved = %d, want 1 (aborted after first write)", rep.Saved)
	}
}

func TestGenerateWithoutGeneratorErrors(t *testing.T) {
	svc := authoring.NewService(nil, newFakeBank(), nil)
	_, err := svc.Generate(context.Background(), ports.GenerateSpec{TestType: "A2", Difficulty: 1, Count: 1, Seed: 1})
	if !errors.Is(err, authoring.ErrNoGenerator) {
		t.Fatalf("want ErrNoGenerator, got %v", err)
	}
}

func TestAuthorValidatesAndStores(t *testing.T) {
	ctx := context.Background()
	bank := newFakeBank()
	svc := authoring.NewService(nil, bank, nil) // author path needs no generator

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
	svc := authoring.NewService(nil, bank, nil)

	_, err := svc.Author(context.Background(), item.ItemSpec{ID: ""}) // empty id is invalid
	if !errors.Is(err, item.ErrInvalidItem) {
		t.Fatalf("want ErrInvalidItem, got %v", err)
	}
	if len(bank.saved) != 0 {
		t.Fatalf("invalid author stored %d items, want 0", len(bank.saved))
	}
}

// --- media offload (Block 11) ----------------------------------------------

// fakeBlobStore is an in-process ports.BlobStore that records puts. It hands back
// a synthetic ref so a test can assert the offloaded MediaRef points at it.
type fakeBlobStore struct {
	blobs   map[string]ports.Blob
	putErr  error
	putCall int
}

func newFakeBlobStore() *fakeBlobStore { return &fakeBlobStore{blobs: map[string]ports.Blob{}} }

func (b *fakeBlobStore) Put(_ context.Context, blob ports.Blob) (string, error) {
	b.putCall++
	if b.putErr != nil {
		return "", b.putErr
	}
	ref := "blobref-" + blob.ContentType
	b.blobs[ref] = blob
	return ref, nil
}

func (b *fakeBlobStore) Get(_ context.Context, ref string) (ports.Blob, error) {
	blob, ok := b.blobs[ref]
	if !ok {
		return ports.Blob{}, shared.ErrNotFound
	}
	return blob, nil
}

var _ ports.BlobStore = (*fakeBlobStore)(nil)

const sampleSVG = `<svg role="img"><title>x</title></svg>`

// mediaSnapshot builds a valid multiple-choice snapshot whose stimulus and first
// option carry an inline base64 SVG data URI (what rulegen emits).
func mediaSnapshot(t *testing.T, id item.ItemID) item.ItemSnapshot {
	t.Helper()
	dataURI := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(sampleSVG))
	it, err := item.NewItem(item.ItemSpec{
		ID:           id,
		Provenance:   item.Provenance{SourceID: "rulegen", Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
		TestType:     "A1",
		Stimulus:     []item.StimulusPart{{Text: "next?"}, {MediaKind: item.MediaSVG, MediaRef: dataURI}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", MediaKind: item.MediaSVG, MediaRef: dataURI},
			{ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:  item.AnswerKey{OptionID: "a"},
		Difficulty: item.Difficulty{Band: 1},
	})
	if err != nil {
		t.Fatalf("media snapshot: %v", err)
	}
	return it.Snapshot()
}

func TestGenerateOffloadsInlineMediaToBlobStore(t *testing.T) {
	ctx := context.Background()
	gen := &fakeGenerator{snaps: []item.ItemSnapshot{mediaSnapshot(t, "gen-A1-1-0")}}
	bank := newFakeBank()
	blobs := newFakeBlobStore()
	svc := authoring.NewService(gen, bank, blobs)

	if _, err := svc.Generate(ctx, ports.GenerateSpec{TestType: "A1", Difficulty: 1, Count: 1, Seed: 1}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	stored, err := bank.GetItem(ctx, "gen-A1-1-0")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	// The stimulus media ref and the option media ref are now blob refs, not the
	// inline data URI; the store holds the decoded bytes under those refs.
	stimRef := stored.Stimulus[1].MediaRef
	optRef := stored.Options[0].MediaRef
	for _, ref := range []string{stimRef, optRef} {
		if strings.HasPrefix(ref, "data:") {
			t.Fatalf("media ref was not offloaded: %q", ref)
		}
		blob, gerr := blobs.Get(ctx, ref)
		if gerr != nil {
			t.Fatalf("blob %q not in store: %v", ref, gerr)
		}
		if string(blob.Bytes) != sampleSVG {
			t.Fatalf("blob bytes = %q, want the SVG", blob.Bytes)
		}
		if blob.ContentType != "image/svg+xml" {
			t.Fatalf("blob content type = %q, want image/svg+xml", blob.ContentType)
		}
	}

	// The text-only stimulus part is untouched (no spurious Put for it).
	if stored.Stimulus[0].MediaRef != "" {
		t.Fatalf("text part gained a media ref: %q", stored.Stimulus[0].MediaRef)
	}
}

func TestGenerateWithoutBlobStoreKeepsInlineMedia(t *testing.T) {
	ctx := context.Background()
	gen := &fakeGenerator{snaps: []item.ItemSnapshot{mediaSnapshot(t, "gen-A1-1-0")}}
	bank := newFakeBank()
	svc := authoring.NewService(gen, bank, nil) // no blob store wired

	if _, err := svc.Generate(ctx, ports.GenerateSpec{TestType: "A1", Difficulty: 1, Count: 1, Seed: 1}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	stored, err := bank.GetItem(ctx, "gen-A1-1-0")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.HasPrefix(stored.Stimulus[1].MediaRef, "data:") {
		t.Fatalf("media ref should stay inline without a store, got %q", stored.Stimulus[1].MediaRef)
	}
}

func TestGenerateOffloadPropagatesBlobError(t *testing.T) {
	putErr := &shared.TestmakerError{Code: "blob.io", Class: shared.ClassUnavailable, Message: "disk full"}
	gen := &fakeGenerator{snaps: []item.ItemSnapshot{mediaSnapshot(t, "gen-A1-1-0")}}
	bank := newFakeBank()
	blobs := newFakeBlobStore()
	blobs.putErr = putErr
	svc := authoring.NewService(gen, bank, blobs)

	_, err := svc.Generate(context.Background(), ports.GenerateSpec{TestType: "A1", Difficulty: 1, Count: 1, Seed: 1})
	if !errors.Is(err, putErr) {
		t.Fatalf("want blob put error, got %v", err)
	}
	if len(bank.saved) != 0 {
		t.Fatalf("item stored despite offload failure: %d saved", len(bank.saved))
	}
}

func TestOffloadIgnoresNonDataURIRefs(t *testing.T) {
	ctx := context.Background()
	// A hand-authored item already carrying a blob ref (not a data URI) must pass
	// through offload untouched, with no Put attempted.
	blobs := newFakeBlobStore()
	bank := newFakeBank()
	svc := authoring.NewService(nil, bank, blobs)

	id, err := svc.Author(ctx, item.ItemSpec{
		ID:           "authored-media",
		Provenance:   item.Provenance{SourceID: "hand", Origin: item.OriginAuthored, Redistributable: shared.RedistYes},
		TestType:     "A1",
		Stimulus:     []item.StimulusPart{{MediaKind: item.MediaImage, MediaRef: "https://example.test/x.png"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:  item.AnswerKey{OptionID: "a"},
		Difficulty: item.Difficulty{Band: 1},
	})
	if err != nil {
		t.Fatalf("author: %v", err)
	}
	if blobs.putCall != 0 {
		t.Fatalf("offload put a non-data-URI ref %d time(s), want 0", blobs.putCall)
	}
	stored, err := bank.GetItem(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Stimulus[0].MediaRef != "https://example.test/x.png" {
		t.Fatalf("external URL ref was rewritten: %q", stored.Stimulus[0].MediaRef)
	}
}
