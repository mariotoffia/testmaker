package memoryblob_test

import (
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/blob/memoryblob"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/ports/blobtest"
)

// Store satisfies the ports.BlobStore contract (kept out of the production
// package so it imports no ports package, per the arch rules).
var _ ports.BlobStore = (*memoryblob.Store)(nil)

func TestBlobStoreConformance(t *testing.T) {
	blobtest.RunBlobStoreTests(t, func() ports.BlobStore {
		return memoryblob.NewStore()
	})
}
