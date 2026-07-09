package ports

import "context"

// Blob is a stored media object: its raw bytes and the MIME content type a
// renderer serves them with. The content type travels with the bytes because a
// blob ref is opaque (a content hash) — the store cannot infer "image/svg+xml"
// from a hash, and item.MediaKind (image/svg/grid/figure) is too coarse to pick
// an exact MIME type (an "image" may be png or jpeg).
type Blob struct {
	Bytes       []byte
	ContentType string
}

// BlobStore stores and resolves figural item media by content-addressed ref
// (driven port). It backs the media referenced by item.StimulusPart.MediaRef /
// item.Option.MediaRef: a producer offloads a blob and rewrites the MediaRef to
// the returned ref, and the renderer resolves the ref back to bytes.
//
// The ref is derived from the content, so storing identical media twice yields
// the same ref (idempotent) and the ref is stable across runs. A local
// filesystem store (adapters/native/blob/fsblob) is the first backend; an S3
// store (adapters/aws/blob/*) is the documented upgrade path — both satisfy this
// same contract, proven by ports/blobtest.
//
// This port owns no domain aggregate (bytes are infrastructure, like
// ports.RawItem at the fetch boundary), so it reuses the shared sentinels rather
// than minting a bounded-context one: an unknown ref is shared.ErrNotFound and
// invalid input (no bytes, no content type) is shared.ErrInvalid.
type BlobStore interface {
	// Put stores blob and returns its content-addressed ref. Storing bytes that
	// are already present returns the same ref without error (idempotent). Empty
	// bytes or an empty content type is shared.ErrInvalid.
	Put(ctx context.Context, blob Blob) (string, error)
	// Get resolves a ref to its blob, or shared.ErrNotFound if the ref is unknown.
	Get(ctx context.Context, ref string) (Blob, error)
}
