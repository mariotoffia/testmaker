package main

import (
	"context"
	"net/url"

	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/domain/scoring"
	"github.com/mariotoffia/testmaker/domain/source"
)

// sourceRef is the taker-facing attribution for one source that contributed
// items to a scored attempt: enough to name the author and link back to their
// (often commercial) site so the taker can explore more of their tests.
type sourceRef struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	Provider string `json:"Provider"`
	Site     string `json:"Site"`
}

// scoreResponse is the score endpoint's body: the score with an embedded
// (flattened) shape so existing clients are unaffected, plus the resolved author
// attribution for the sources behind the attempt.
type scoreResponse struct {
	scoring.Score
	Sources []sourceRef `json:"Sources"`
}

// siteRoot reduces a URL to its scheme://host origin — the author's landing
// site, for a "more tests from this author" link. Returns "" when the input
// carries no scheme+host (a relative path or garbage), so callers can skip it.
func siteRoot(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// sourceAttribution resolves the distinct sources behind a scored attempt's
// feedback into author links. Takers cannot query the operator-only sources API,
// so the score handler resolves attribution server-side and embeds it. Sources
// absent from the catalogue, or with no linkable URL (generated items), are
// dropped; the first occurrence order is preserved.
func sourceAttribution(ctx context.Context, cat *catalog.Service, feedback []scoring.ItemFeedback) []sourceRef {
	if cat == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []sourceRef
	for _, fb := range feedback {
		if fb.SourceID == "" || seen[fb.SourceID] {
			continue
		}
		seen[fb.SourceID] = true
		snap, err := cat.Get(ctx, source.SourceID(fb.SourceID))
		if err != nil || len(snap.URLs) == 0 {
			continue
		}
		site := siteRoot(snap.URLs[0])
		if site == "" {
			continue
		}
		out = append(out, sourceRef{ID: string(snap.ID), Name: snap.Name, Provider: snap.Provider, Site: site})
	}
	return out
}
