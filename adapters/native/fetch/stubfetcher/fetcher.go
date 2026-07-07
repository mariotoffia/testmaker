package stubfetcher

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// Fetcher is a placeholder ports.Fetcher that performs no I/O.
type Fetcher struct{}

// NewFetcher returns the stub fetcher.
func NewFetcher() *Fetcher { return &Fetcher{} }

// Supports reports support for any source (placeholder).
func (f *Fetcher) Supports(_ source.Snapshot) bool { return true }

// Fetch returns an empty result annotated as a stub; it never errors.
func (f *Fetcher) Fetch(_ context.Context, req ports.FetchRequest) (ports.FetchResult, error) {
	return ports.FetchResult{
		SourceID: req.Source.ID,
		Items:    nil,
		Partial:  false,
		Note:     "stubfetcher: fetch not implemented (placeholder adapter)",
	}, nil
}
