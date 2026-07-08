// Package httpfetch is the direct-download ports.Fetcher: it pulls a source's
// data artifacts over HTTP and returns them as loose RawItems for a normalizer
// to map into bank items.
//
// It reports support for a source whose Extraction.Method is direct-download.
// Fetch GETs every source URL that points at a data file (.zip/.csv/.tsv/.json),
// expands a .zip in memory, and yields one RawItem per artifact or archive
// member: a text member's bytes land in Raw["content"], a binary member is
// referenced by Media (its bytes are left for a blob store to pull later). The
// RawItems are the downloaded files themselves, not test items — turning a
// codebook + response CSV into scored items is the normalizer's job (app/ingest),
// which keeps this adapter a generic, source-agnostic downloader.
//
// It depends on the standard library alone (net/http, archive/zip) plus the
// domain shared kernel for its error sentinels, so it carries no vendor
// dependency. A response body is read under a size cap; only transport
// failures, non-2xx statuses and unreadable archives surface as errors (matched
// by Code via errors.Is, with the cause — e.g. context.Canceled — reachable
// through Unwrap).
package httpfetch
