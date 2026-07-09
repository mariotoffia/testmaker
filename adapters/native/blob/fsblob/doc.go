// Package fsblob is a filesystem ports.BlobStore: content-addressed media blobs
// stored one-file-per-blob under a base directory. It is the durable native blob
// backend (the "local FS first" step of the media block); an S3 adapter
// (adapters/aws/blob/*) is the documented cloud upgrade behind the same port.
//
// Each blob is written to <dir>/<ref>, where ref is the sha256 of the content
// type and bytes, so identical media dedupe to one file. The file itself is the
// content type on the first line, a newline, then the raw bytes — a single,
// binary-safe file (a MIME type never contains a newline).
package fsblob
