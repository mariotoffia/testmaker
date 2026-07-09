// Package memoryblob is an in-memory ports.BlobStore: a concurrency-safe,
// content-addressed map of media blobs. It is the dependency-free default blob
// backend (mirroring memorytestdb for the TestDb ports) — zero-config for the
// delivery surface and tests. Stored bytes are deep-copied on Put and Get so a
// caller can never mutate stored state through an aliased slice.
//
// Durability is out of scope here: use adapters/native/blob/fsblob (or an S3
// adapter) when media must outlive the process.
package memoryblob
