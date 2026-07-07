package stubfetcher_test

import (
	"context"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

var _ ports.Fetcher = (*stubfetcher.Fetcher)(nil)

func TestStubFetch(t *testing.T) {
	f := stubfetcher.NewFetcher()
	snap := source.Snapshot{ID: "omib"}

	if !f.Supports(snap) {
		t.Fatal("stub should support any source")
	}
	res, err := f.Fetch(context.Background(), ports.FetchRequest{Source: snap})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if res.SourceID != "omib" || len(res.Items) != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
}
