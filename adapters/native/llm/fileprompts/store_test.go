package fileprompts_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/llm/fileprompts"
	"github.com/mariotoffia/testmaker/domain/prompt"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/ports/prompttest"
)

// Store satisfies the ports.PromptRepository contract (kept out of the
// production package so it imports no ports package, per the arch rules).
var _ ports.PromptRepository = (*fileprompts.Store)(nil)

func TestPromptRepositoryConformance(t *testing.T) {
	prompttest.RunPromptRepositoryTests(t, func() ports.PromptRepository {
		store, err := fileprompts.Open(t.TempDir())
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return store
	})
}

// TestGetRejectsCorruptFile proves a hand-edited, unparseable prompt file
// surfaces as an error rather than a silently broken template.
func TestGetRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	store, err := fileprompts.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("id: broken\nversion: not-an-int\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := store.Get(context.Background(), "broken"); err == nil {
		t.Fatal("expected an error reading a corrupt prompt file")
	}
}

// TestFilenameIDMismatchRejected proves a hand-authored file whose declared id
// disagrees with its filename is rejected by both Get and List, so List can
// never surface an id that Get would then fail to resolve.
func TestFilenameIDMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	store, err := fileprompts.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// File is foo.yaml but declares id: bar.
	body := []byte("id: bar\nversion: 1\npurpose: extraction\ntemplate: hi\n")
	if err := os.WriteFile(filepath.Join(dir, "foo.yaml"), body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := store.Get(context.Background(), "foo"); !errors.Is(err, prompt.ErrInvalidPrompt) {
		t.Fatalf("Get err = %v, want ErrInvalidPrompt", err)
	}
	if _, err := store.List(context.Background()); !errors.Is(err, prompt.ErrInvalidPrompt) {
		t.Fatalf("List err = %v, want ErrInvalidPrompt", err)
	}
}

// back intact by a second Store opened on the same directory — the file store's
// reason to exist.
func TestReloadRoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	writer, err := fileprompts.Open(dir)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	want := prompt.MustPrompt(prompt.Spec{
		ID: "extract-items", Version: 4, Purpose: prompt.PurposeExtraction,
		Template: "Extract from {{.payload}}.", Params: []string{"payload"}, Notes: "seed",
	}).Snapshot()
	if err := writer.Put(context.Background(), want); err != nil {
		t.Fatalf("put: %v", err)
	}

	reader, err := fileprompts.Open(dir)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	got, err := reader.ByPurpose(context.Background(), prompt.PurposeExtraction)
	if err != nil {
		t.Fatalf("by purpose: %v", err)
	}
	if got.ID != want.ID || got.Version != want.Version || got.Template != want.Template {
		t.Fatalf("round-trip lost data: got %+v, want %+v", got, want)
	}
}
