package memoryblob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sync"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// Store is an in-memory, content-addressed blob store safe for concurrent use.
type Store struct {
	mu    sync.RWMutex
	blobs map[string]ports.Blob
}

// NewStore returns an empty in-memory blob store.
func NewStore() *Store {
	return &Store{blobs: make(map[string]ports.Blob)}
}

// blobRef derives a content-addressed ref binding both the content type and the
// bytes, so identical media dedupe and the same bytes under a different type
// never collide. The NUL separator is unambiguous (a MIME type has no NUL byte).
func blobRef(b ports.Blob) string {
	h := sha256.New()
	h.Write([]byte(b.ContentType))
	h.Write([]byte{0})
	h.Write(b.Bytes)
	return hex.EncodeToString(h.Sum(nil))
}

// Put stores blob under its content-addressed ref, deep-copying the bytes so the
// store never aliases the caller's slice. Empty bytes or content type is
// shared.ErrInvalid; a repeated Put of identical content is a no-op that returns
// the same ref.
func (s *Store) Put(_ context.Context, blob ports.Blob) (string, error) {
	if len(blob.Bytes) == 0 {
		return "", shared.ErrInvalid.WithMessage("blob has no bytes")
	}
	if blob.ContentType == "" {
		return "", shared.ErrInvalid.WithMessage("blob has no content type")
	}
	ref := blobRef(blob)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.blobs[ref]; !ok {
		s.blobs[ref] = ports.Blob{Bytes: slices.Clone(blob.Bytes), ContentType: blob.ContentType}
	}
	return ref, nil
}

// Get returns a deep copy of the blob for ref, or shared.ErrNotFound.
func (s *Store) Get(_ context.Context, ref string) (ports.Blob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.blobs[ref]
	if !ok {
		return ports.Blob{}, shared.ErrNotFound.WithMessagef("unknown blob ref: %s", ref)
	}
	return ports.Blob{Bytes: slices.Clone(b.Bytes), ContentType: b.ContentType}, nil
}
