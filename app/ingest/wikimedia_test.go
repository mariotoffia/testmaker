package ingest_test

import (
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/ports"
)

// wmRaw wraps a MediaWiki JSON body the way apifetch inlines it.
func wmRaw(bodies ...string) []ports.RawItem {
	raw := make([]ports.RawItem, len(bodies))
	for i, b := range bodies {
		raw[i] = ports.RawItem{ExternalID: "api.php", Raw: map[string]any{"content": b}}
	}
	return raw
}

const wmResponse = `{
  "batchcomplete": "",
  "continue": {"gcmcontinue": "file|ABC|123", "continue": "gcmcontinue||"},
  "query": {
    "pages": {
      "77": {
        "pageid": 77,
        "ns": 6,
        "title": "File:Raven matrix.svg",
        "imageinfo": [{
          "url": "https://upload.wikimedia.org/wikipedia/commons/1/12/Raven_matrix.svg",
          "mime": "image/svg+xml",
          "extmetadata": {"LicenseShortName": {"value": "Public domain"}}
        }]
      },
      "42": {
        "pageid": 42,
        "ns": 6,
        "title": "File:Binet scale.pdf",
        "imageinfo": [{
          "url": "https://upload.wikimedia.org/wikipedia/commons/2/2a/Binet_scale.pdf",
          "mime": "application/pdf",
          "extmetadata": {"LicenseShortName": {"value": "CC BY 4.0"}}
        }]
      }
    }
  }
}`

func TestWikimediaFiguresParsesImageInfo(t *testing.T) {
	figures, err := ingest.WikimediaFigures(wmRaw(wmResponse))
	if err != nil {
		t.Fatalf("WikimediaFigures: %v", err)
	}
	if len(figures) != 2 {
		t.Fatalf("got %d figures, want 2", len(figures))
	}
	// Sorted by page id for determinism (the API returns pages unordered).
	if figures[0].PageID != 42 || figures[1].PageID != 77 {
		t.Errorf("page ids = %d,%d, want 42,77 (sorted)", figures[0].PageID, figures[1].PageID)
	}
	raven := figures[1]
	if raven.Title != "File:Raven matrix.svg" {
		t.Errorf("title = %q", raven.Title)
	}
	if raven.URL != "https://upload.wikimedia.org/wikipedia/commons/1/12/Raven_matrix.svg" {
		t.Errorf("url = %q", raven.URL)
	}
	if raven.MIME != "image/svg+xml" {
		t.Errorf("mime = %q", raven.MIME)
	}
	if raven.License != "Public domain" {
		t.Errorf("license = %q", raven.License)
	}
}

func TestWikimediaFiguresSkipsPagesWithoutImageInfo(t *testing.T) {
	// A page carrying no imageinfo (e.g. a redirect stub) is skipped, not an error.
	const body = `{"query":{"pages":{
		"1":{"pageid":1,"title":"File:Has info.png","imageinfo":[{"url":"https://x/y.png","mime":"image/png","extmetadata":{"LicenseShortName":{"value":"PD"}}}]},
		"2":{"pageid":2,"title":"File:No info.png"}
	}}}`
	figures, err := ingest.WikimediaFigures(wmRaw(body))
	if err != nil {
		t.Fatalf("WikimediaFigures: %v", err)
	}
	if len(figures) != 1 || figures[0].PageID != 1 {
		t.Fatalf("got %v, want the single page-1 figure", figures)
	}
}

func TestWikimediaFiguresEmptyQuery(t *testing.T) {
	// Valid JSON with no pages yields no figures and no error.
	figures, err := ingest.WikimediaFigures(wmRaw(`{"batchcomplete":"","query":{"pages":{}}}`))
	if err != nil {
		t.Fatalf("WikimediaFigures: %v", err)
	}
	if len(figures) != 0 {
		t.Errorf("got %d figures, want 0", len(figures))
	}
}

func TestWikimediaFiguresMalformedIsError(t *testing.T) {
	_, err := ingest.WikimediaFigures(wmRaw(`<html>not json</html>`))
	if !errors.Is(err, ingest.ErrWikimediaData) {
		t.Fatalf("err = %v, want ErrWikimediaData", err)
	}
}

func TestWikimediaFiguresErrorEnvelopeIsError(t *testing.T) {
	// MediaWiki returns HTTP 200 with an error envelope for a bad query; that is
	// valid JSON (apifetch's json.Valid gate passes it) but not a usable result,
	// so it must surface rather than read as "0 figures, no error".
	const body = `{"error":{"code":"badvalue","info":"Unrecognized value for parameter gcmtype."}}`
	_, err := ingest.WikimediaFigures(wmRaw(body))
	if !errors.Is(err, ingest.ErrWikimediaData) {
		t.Fatalf("err = %v, want ErrWikimediaData for an API error envelope", err)
	}
}

func TestWikimediaFiguresNoQueryIsError(t *testing.T) {
	// Valid JSON that carries neither a query nor an error is not an imageinfo
	// response; distinguishing it from an empty query prevents silent success.
	_, err := ingest.WikimediaFigures(wmRaw(`{"batchcomplete":""}`))
	if !errors.Is(err, ingest.ErrWikimediaData) {
		t.Fatalf("err = %v, want ErrWikimediaData for a response with no query", err)
	}
}
