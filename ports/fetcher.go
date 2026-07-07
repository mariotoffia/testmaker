package ports

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/source"
)

// RawItem is an item pulled from a source before it is normalized into a bank
// item. Its shape is intentionally loose at the fetch boundary; the item-bank
// block maps it to a validated item.ItemSnapshot.
type RawItem struct {
	ExternalID string         // id within the source, if any
	Stem       string         // question text, if textual
	Media      []string       // URLs / file references (images, PDFs, ...)
	Raw        map[string]any // provider-specific payload
}

// FetchRequest asks a Fetcher to pull items from a source.
type FetchRequest struct {
	Source source.Snapshot
	Limit  int // 0 = fetcher default
}

// FetchResult is the outcome of a fetch attempt.
type FetchResult struct {
	SourceID source.SourceID
	Items    []RawItem
	Partial  bool   // true if more items exist beyond Limit
	Note     string // human note (e.g. "stub: no items fetched")
}

// Fetcher pulls raw items from a source (driven port). Concrete fetchers are
// selected per source.AccessClass / source.Extraction.Method — e.g. a
// direct-download fetcher, an HTML scraper, a headless-browser driver or a
// generator runner.
type Fetcher interface {
	// Supports reports whether this fetcher can handle the given source.
	Supports(snap source.Snapshot) bool
	// Fetch pulls up to req.Limit raw items from the source.
	Fetch(ctx context.Context, req FetchRequest) (FetchResult, error)
}
