package ingest

import (
	"encoding/json"
	"sort"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// ErrWikimediaData marks a Wikimedia API artifact the normalizer could not read
// as a MediaWiki imageinfo response.
var ErrWikimediaData = &shared.TestmakerError{
	Code: "ingest.wikimedia_data", Class: shared.ClassInvalid, Message: "wikimedia api response missing or malformed",
}

// WikimediaSourceID is the catalogue id the Wikimedia figure preview maps.
const WikimediaSourceID = "wikimedia-commons"

// WikimediaFigure is one figure referenced from a MediaWiki imageinfo response.
// Wikimedia figures ship no answer key, so they are not scored bank items — they
// are candidate media a human (or a later authoring step) can build items around.
type WikimediaFigure struct {
	PageID  int
	Title   string
	URL     string
	MIME    string
	License string
}

// wikimediaResponse is the slice of the MediaWiki query=imageinfo response the
// parser reads: query.pages is an object keyed by page id, each page carrying an
// imageinfo array with the file URL, MIME type and license. Query is a pointer
// so a well-formed-but-non-query artifact (or an API error envelope) is
// distinguishable from a query that legitimately returned no pages.
type wikimediaResponse struct {
	Error *struct {
		Code string `json:"code"`
		Info string `json:"info"`
	} `json:"error"`
	Query *struct {
		Pages map[string]struct {
			PageID    int    `json:"pageid"`
			Title     string `json:"title"`
			ImageInfo []struct {
				URL         string `json:"url"`
				MIME        string `json:"mime"`
				ExtMetadata struct {
					LicenseShortName struct {
						Value string `json:"value"`
					} `json:"LicenseShortName"`
				} `json:"extmetadata"`
			} `json:"imageinfo"`
		} `json:"pages"`
	} `json:"query"`
}

// WikimediaFigures parses fetched MediaWiki imageinfo artifacts into figure
// references, sorted by page id for a deterministic order (the API returns pages
// as an unordered object). It is a pure transform over the RawItems apifetch
// inlines; pages without image info are skipped. A raw artifact that does not
// parse as a MediaWiki response is an error.
//
// The figures are media-only: they carry no answer key and so are not turned
// into scored items here. Wiring them into the item bank would need an authoring
// step that supplies the question and key.
func WikimediaFigures(raw []ports.RawItem) ([]WikimediaFigure, error) {
	var figures []WikimediaFigure
	for _, it := range raw {
		content, ok := it.Raw["content"].(string)
		if !ok || content == "" {
			continue
		}
		var resp wikimediaResponse
		if err := json.Unmarshal([]byte(content), &resp); err != nil {
			return nil, ErrWikimediaData.WithMessagef("artifact %s is not a MediaWiki response", it.ExternalID).Wrap(err)
		}
		if resp.Error != nil {
			// A 200 carrying an error envelope is valid JSON (so apifetch's
			// json.Valid gate passes it) but not a usable imageinfo response —
			// surface it instead of silently reporting zero figures.
			return nil, ErrWikimediaData.WithMessagef("MediaWiki API error for %s: %s (%s)", it.ExternalID, resp.Error.Info, resp.Error.Code)
		}
		if resp.Query == nil {
			return nil, ErrWikimediaData.WithMessagef("artifact %s has no query result", it.ExternalID)
		}
		for _, p := range resp.Query.Pages {
			if len(p.ImageInfo) == 0 {
				continue
			}
			ii := p.ImageInfo[0]
			figures = append(figures, WikimediaFigure{
				PageID:  p.PageID,
				Title:   p.Title,
				URL:     ii.URL,
				MIME:    ii.MIME,
				License: ii.ExtMetadata.LicenseShortName.Value,
			})
		}
	}
	sort.Slice(figures, func(i, j int) bool {
		if figures[i].PageID != figures[j].PageID {
			return figures[i].PageID < figures[j].PageID
		}
		return figures[i].Title < figures[j].Title
	})
	return figures, nil
}
