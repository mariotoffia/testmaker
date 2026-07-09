package fsblob_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/blob/fsblob"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/ports/blobtest"
)

// Store satisfies the ports.BlobStore contract (kept out of the production
// package so it imports no ports package, per the arch rules).
var _ ports.BlobStore = (*fsblob.Store)(nil)

func TestBlobStoreConformance(t *testing.T) {
	blobtest.RunBlobStoreTests(t, func() ports.BlobStore {
		store, err := fsblob.Open(t.TempDir())
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return store
	})
}

// TestPutPersistsAcrossStoreInstances proves the FS backend is durable: a blob
// written by one Store rooted at a directory is readable by a fresh Store rooted
// at the same directory — the property memoryblob cannot offer.
func TestPutPersistsAcrossStoreInstances(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	first, err := fsblob.Open(dir)
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	ref, err := first.Put(ctx, ports.Blob{Bytes: []byte("durable"), ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	second, err := fsblob.Open(dir)
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	got, err := second.Get(ctx, ref)
	if err != nil {
		t.Fatalf("get from fresh store: %v", err)
	}
	if string(got.Bytes) != "durable" {
		t.Fatalf("bytes = %q, want %q", got.Bytes, "durable")
	}
}

// TestGetCorruptBlobIsStoreError proves a file missing the content-type header
// (e.g. hand-corrupted) surfaces as ErrStore, not a silent success. The file is
// named a validly-shaped content ref so it passes the ref guard and is read.
func TestGetCorruptBlobIsStoreError(t *testing.T) {
	dir := t.TempDir()
	ref := strings.Repeat("de", 32) // 64 hex chars: a validly-shaped content ref
	if err := os.WriteFile(filepath.Join(dir, ref), []byte("no newline header"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	store, err := fsblob.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, err = store.Get(context.Background(), ref)
	if !errors.Is(err, fsblob.ErrStore) {
		t.Fatalf("want fsblob.ErrStore, got %v", err)
	}
	// A corruption must not be masked as a plain not-found miss.
	if errors.Is(err, shared.ErrNotFound) {
		t.Fatal("corrupt blob must not be reported as not-found")
	}
}

// TestPutIsAtomicAndLeavesNoTemp proves a successful Put leaves exactly the
// content-ref file in the directory — the temp file it writes through is renamed
// into place and never lingers — so a reader (or a second process) only ever sees
// a complete blob under its ref.
func TestPutIsAtomicAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	store, err := fsblob.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ref, err := store.Put(context.Background(), ports.Blob{Bytes: []byte("<svg/>"), ContentType: "image/svg+xml"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != ref {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dir entries = %v, want exactly [%s] (no leftover temp)", names, ref)
	}
}

// could have minted — a path-traversal attempt, an absolute path, any non-hex
// name — is a not-found miss and never reads the filesystem, so the renderer's
// /media/{ref} route cannot be turned into an arbitrary-file read.
func TestGetRejectsNonContentRef(t *testing.T) {
	dir := t.TempDir()
	// A secret sitting one level above the blob dir; a traversal ref must not reach it.
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	store, err := fsblob.Open(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, ref := range []string{
		"../secret.txt",
		"../../etc/passwd",
		"/etc/passwd",
		secret,
		"not-hex-and-too-short",
		strings.Repeat("g", 64), // right length, non-hex
	} {
		if _, err := store.Get(context.Background(), ref); !errors.Is(err, shared.ErrNotFound) {
			t.Fatalf("ref %q: want shared.ErrNotFound, got %v", ref, err)
		}
	}
}
