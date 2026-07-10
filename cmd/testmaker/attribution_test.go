package main

import (
	"context"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/domain/scoring"
	"github.com/mariotoffia/testmaker/domain/source"
)

func TestSiteRoot(t *testing.T) {
	cases := map[string]string{
		"https://www.123test.com/iq-test/":             "https://www.123test.com",
		"https://openpsychometrics.org/_rawdata/x.zip": "https://openpsychometrics.org",
		"ftp://x/y":      "ftp://x",
		"/relative/only": "",
		"":               "",
	}
	for in, want := range cases {
		if got := siteRoot(in); got != want {
			t.Errorf("siteRoot(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSourceAttribution proves the score enrichment: distinct sources only, the
// author site derived from the first URL, and sources with no catalogue entry or
// no linkable URL (generated items) dropped.
func TestSourceAttribution(t *testing.T) {
	ctx := context.Background()
	cat := catalog.NewService(memorycatalog.NewStore(), fakeLoader{snaps: []source.Snapshot{{
		ID:       "acme-iq",
		Name:     "Acme IQ",
		Provider: "Acme Corp",
		URLs:     []string{"https://acme.example/iq/take/", "https://acme.example/data.zip"},
	}}})
	if _, err := cat.Sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	fb := []scoring.ItemFeedback{
		{SourceID: "acme-iq"}, {SourceID: "acme-iq"}, // duplicate collapses to one
		{SourceID: "rulegen"}, // not in catalogue → skipped
		{SourceID: ""},        // generated / no source → skipped
	}
	got := sourceAttribution(ctx, cat, fb)
	if len(got) != 1 {
		t.Fatalf("attribution = %+v, want exactly 1 source", got)
	}
	if got[0].Provider != "Acme Corp" || got[0].Name != "Acme IQ" || got[0].Site != "https://acme.example" {
		t.Fatalf("attribution[0] = %+v, want Acme Corp / Acme IQ / https://acme.example", got[0])
	}
}
