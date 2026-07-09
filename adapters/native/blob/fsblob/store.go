package fsblob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// ErrStore marks a failure of the filesystem backend itself — creating the base
// directory, or reading/writing a blob file. The port's semantic outcomes
// surface as shared sentinels (an unknown ref is shared.ErrNotFound, invalid
// input shared.ErrInvalid); ErrStore is everything else. The wrapped cause stays
// reachable through Unwrap.
var ErrStore = &shared.TestmakerError{
	Code: "fsblob.store", Class: shared.ClassUnavailable, Message: "fsblob store error",
}

// Store is a filesystem-backed, content-addressed blob store. The mutex serializes
// the read-modify-write around each file so a Get never observes a half-written
// blob.
//
// ponytail: one store-wide lock — trivially correct and fine for the CLI/single
// server. Shard by ref prefix if blob write throughput ever matters.
type Store struct {
	mu  sync.RWMutex
	dir string
}

// Open returns a Store rooted at dir, creating the directory (and parents) if
// absent. A creation failure is ErrStore.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, ErrStore.WithMessagef("create blob dir %q", dir).Wrap(err)
	}
	return &Store{dir: dir}, nil
}

// blobRef derives a content-addressed ref binding both the content type and the
// bytes (NUL-separated, unambiguous), so identical media dedupe and the same
// bytes under a different type never collide.
func blobRef(b ports.Blob) string {
	h := sha256.New()
	h.Write([]byte(b.ContentType))
	h.Write([]byte{0})
	h.Write(b.Bytes)
	return hex.EncodeToString(h.Sum(nil))
}

// Put writes blob to <dir>/<ref>, returning the ref. Storing content that is
// already present is a no-op (the file exists), so Put is idempotent. The write
// is atomic — a temp file renamed into place — so a blob only ever appears under
// its ref complete; a crash mid-write leaves a collectable temp, never a
// truncated ref that would be served as content-addressed bytes. Empty bytes or
// content type is shared.ErrInvalid; an I/O failure is ErrStore.
func (s *Store) Put(_ context.Context, blob ports.Blob) (string, error) {
	if len(blob.Bytes) == 0 {
		return "", shared.ErrInvalid.WithMessage("blob has no bytes")
	}
	if blob.ContentType == "" {
		return "", shared.ErrInvalid.WithMessage("blob has no content type")
	}
	ref := blobRef(blob)
	path := filepath.Join(s.dir, ref)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(path); err == nil {
		return ref, nil // already stored; a file at its ref is always complete (atomic write)
	}
	body := append([]byte(blob.ContentType+"\n"), blob.Bytes...)
	if err := s.writeAtomic(path, body, ref); err != nil {
		return "", err
	}
	return ref, nil
}

// writeAtomic writes body to a temp file in s.dir (0o600 via os.CreateTemp) and
// renames it onto path. Same-directory rename is atomic on POSIX, so path never
// exposes a partial file. The temp name ("put-*.tmp") can never be mistaken for a
// content ref, so Get ignores any orphan a crash leaves behind. Backend failures
// surface as ErrStore.
func (s *Store) writeAtomic(path string, body []byte, ref string) error {
	tmp, err := os.CreateTemp(s.dir, "put-*.tmp")
	if err != nil {
		return ErrStore.WithMessagef("create temp for blob %s", ref).Wrap(err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return ErrStore.WithMessagef("write blob %s", ref).Wrap(err)
	}
	if err := tmp.Close(); err != nil {
		return ErrStore.WithMessagef("close temp for blob %s", ref).Wrap(err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return ErrStore.WithMessagef("commit blob %s", ref).Wrap(err)
	}
	return nil
}

// Get reads the blob at <dir>/<ref>, or shared.ErrNotFound if the file is absent.
// ref must be a content ref this store could have minted (64 lowercase hex
// chars); anything else — including a path-traversal attempt like "../secret"
// arriving through the renderer's /media/{ref} route — can never name a stored
// blob, so it is shared.ErrNotFound and never touches the filesystem.
func (s *Store) Get(_ context.Context, ref string) (ports.Blob, error) {
	if !isContentRef(ref) {
		return ports.Blob{}, shared.ErrNotFound.WithMessagef("unknown blob ref: %s", ref)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	body, err := os.ReadFile(filepath.Join(s.dir, ref))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ports.Blob{}, shared.ErrNotFound.WithMessagef("unknown blob ref: %s", ref)
		}
		return ports.Blob{}, ErrStore.WithMessagef("read blob %s", ref).Wrap(err)
	}
	ct, data, ok := bytes.Cut(body, []byte{'\n'})
	if !ok {
		return ports.Blob{}, ErrStore.WithMessagef("corrupt blob %s: missing content-type header", ref)
	}
	return ports.Blob{Bytes: data, ContentType: string(ct)}, nil
}

// isContentRef reports whether ref is shaped like a ref blobRef produces: a
// sha256 hex digest (64 lowercase hex chars). This keeps a ref a pure file name
// under s.dir — no separators, no "..", no absolute path can pass — so Get can
// never read outside the store.
func isContentRef(ref string) bool {
	if len(ref) != sha256.Size*2 {
		return false
	}
	for i := 0; i < len(ref); i++ {
		c := ref[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
