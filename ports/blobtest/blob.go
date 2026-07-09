// Package blobtest provides a reusable conformance suite that every
// ports.BlobStore implementation must pass, so all backends (in-memory today,
// filesystem and S3 later) are guaranteed behavioural parity. It asserts the
// universal contract: content-addressed round-trip, idempotent Put, distinct
// refs for distinct content, snapshot isolation (a returned or supplied byte
// slice never aliases stored state), unknown-ref and invalid-input handling.
package blobtest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// RunBlobStoreTests runs the conformance suite against a fresh, empty store from
// newStore.
func RunBlobStoreTests(t *testing.T, newStore func() ports.BlobStore) {
	t.Helper()

	ctx := context.Background()
	svg := ports.Blob{Bytes: []byte(`<svg role="img"><title>x</title></svg>`), ContentType: "image/svg+xml"}

	t.Run("PutThenGetRoundTrips", func(t *testing.T) {
		store := newStore()
		ref, err := store.Put(ctx, svg)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		if ref == "" {
			t.Fatal("put returned an empty ref")
		}
		got, err := store.Get(ctx, ref)
		if err != nil {
			t.Fatalf("get %q: %v", ref, err)
		}
		if !bytes.Equal(got.Bytes, svg.Bytes) {
			t.Fatalf("bytes = %q, want %q", got.Bytes, svg.Bytes)
		}
		if got.ContentType != svg.ContentType {
			t.Fatalf("content type = %q, want %q", got.ContentType, svg.ContentType)
		}
	})

	t.Run("PutIsIdempotentForIdenticalContent", func(t *testing.T) {
		store := newStore()
		first, err := store.Put(ctx, svg)
		if err != nil {
			t.Fatalf("first put: %v", err)
		}
		second, err := store.Put(ctx, svg)
		if err != nil {
			t.Fatalf("second put: %v", err)
		}
		if first != second {
			t.Fatalf("identical content produced different refs: %q vs %q", first, second)
		}
	})

	t.Run("DistinctContentProducesDistinctRefs", func(t *testing.T) {
		store := newStore()
		a, err := store.Put(ctx, svg)
		if err != nil {
			t.Fatalf("put a: %v", err)
		}
		// Same bytes, different content type must not collide (the ref binds
		// both, so a renderer never serves bytes under the wrong MIME type).
		b, err := store.Put(ctx, ports.Blob{Bytes: svg.Bytes, ContentType: "text/plain"})
		if err != nil {
			t.Fatalf("put b: %v", err)
		}
		c, err := store.Put(ctx, ports.Blob{Bytes: []byte("other"), ContentType: svg.ContentType})
		if err != nil {
			t.Fatalf("put c: %v", err)
		}
		if a == b || a == c || b == c {
			t.Fatalf("distinct blobs collided: %q %q %q", a, b, c)
		}
	})

	t.Run("StoreDoesNotAliasCallerOrReturnedBytes", func(t *testing.T) {
		store := newStore()
		src := ports.Blob{Bytes: []byte("mutable"), ContentType: "text/plain"}
		ref, err := store.Put(ctx, src)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		src.Bytes[0] = 'X' // mutate the caller's slice after Put

		got, err := store.Get(ctx, ref)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if string(got.Bytes) != "mutable" {
			t.Fatalf("stored bytes aliased the caller slice: got %q", got.Bytes)
		}
		got.Bytes[0] = 'Y' // mutate the returned slice

		again, err := store.Get(ctx, ref)
		if err != nil {
			t.Fatalf("get again: %v", err)
		}
		if string(again.Bytes) != "mutable" {
			t.Fatalf("stored bytes aliased a returned slice: got %q", again.Bytes)
		}
	})

	t.Run("GetUnknownRefReturnsErrNotFound", func(t *testing.T) {
		store := newStore()
		if _, err := store.Get(ctx, "does-not-exist"); !errors.Is(err, shared.ErrNotFound) {
			t.Fatalf("want shared.ErrNotFound, got %v", err)
		}
	})

	t.Run("PutRejectsEmptyInput", func(t *testing.T) {
		store := newStore()
		if _, err := store.Put(ctx, ports.Blob{ContentType: "text/plain"}); !errors.Is(err, shared.ErrInvalid) {
			t.Fatalf("empty bytes: want shared.ErrInvalid, got %v", err)
		}
		if _, err := store.Put(ctx, ports.Blob{Bytes: []byte("x")}); !errors.Is(err, shared.ErrInvalid) {
			t.Fatalf("empty content type: want shared.ErrInvalid, got %v", err)
		}
	})
}
