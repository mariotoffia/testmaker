package scrapefetch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/scrapefetch"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// Compile-time proof the adapter satisfies the port (kept out of production code).
var _ ports.Fetcher = (*scrapefetch.Fetcher)(nil)

// snap builds a minimal source snapshot with the given method and URLs.
func snap(method source.ExtractionMethod, urls ...string) source.Snapshot {
	return source.Snapshot{
		ID:         "test-source",
		URLs:       urls,
		Extraction: source.Extraction{Method: method},
	}
}

func TestSupports(t *testing.T) {
	f := scrapefetch.New()
	if !f.Supports(snap(source.MethodScrapeHTML)) {
		t.Errorf("expected support for scrape-html")
	}
	if f.Supports(snap(source.MethodDirectDownload)) {
		t.Errorf("did not expect support for direct-download")
	}
}

func TestFetchPages(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><div class=\"quiz\">hi</div></body></html>"))
	}))
	defer srv.Close()

	f := scrapefetch.New(scrapefetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodScrapeHTML, srv.URL+"/word-knowledge-wk/")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/word-knowledge-wk/" {
		t.Errorf("path = %q, want /word-knowledge-wk/", gotPath)
	}
	if res.SourceID != "test-source" {
		t.Errorf("SourceID = %q", res.SourceID)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(res.Items))
	}
	if res.Items[0].ExternalID != "word-knowledge-wk" {
		t.Errorf("ExternalID = %q, want word-knowledge-wk", res.Items[0].ExternalID)
	}
	content, _ := res.Items[0].Raw["content"].(string)
	if !strings.Contains(content, "class=\"quiz\"") {
		t.Errorf("HTML not inlined into content: %q", content)
	}
	if got, _ := res.Items[0].Raw["source_url"].(string); got != srv.URL+"/word-knowledge-wk/" {
		t.Errorf("source_url = %q", got)
	}
}

func TestFetchMultiplePages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>" + r.URL.Path + "</html>"))
	}))
	defer srv.Close()

	f := scrapefetch.New(scrapefetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(), ports.FetchRequest{
		Source: snap(source.MethodScrapeHTML, srv.URL+"/a/", srv.URL+"/b/", srv.URL+"/c/"),
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 3 || res.Partial {
		t.Fatalf("got %d items partial=%v, want 3 partial=false", len(res.Items), res.Partial)
	}
}

func TestFetchNoURL(t *testing.T) {
	f := scrapefetch.New()
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodScrapeHTML)})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 0 {
		t.Errorf("got %d items, want 0", len(res.Items))
	}
	if !strings.Contains(res.Note, "no page URL") {
		t.Errorf("Note = %q", res.Note)
	}
}

func TestFetchLimitAcrossURLs(t *testing.T) {
	// Three pages, Limit 2: only the first two are fetched and Partial reports
	// the third was left unread.
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte("<html/>"))
	}))
	defer srv.Close()

	f := scrapefetch.New(scrapefetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(), ports.FetchRequest{
		Source: snap(source.MethodScrapeHTML, srv.URL+"/a/", srv.URL+"/b/", srv.URL+"/c/"),
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 2 || !res.Partial {
		t.Errorf("got %d items partial=%v, want 2 partial=true", len(res.Items), res.Partial)
	}
	if requests != 2 {
		t.Errorf("made %d requests, want 2 (third page must not be fetched past the limit)", requests)
	}
}

func TestFetchLimitExactNotPartial(t *testing.T) {
	// Two pages, Limit 2: budget is filled and nothing remains, so Partial=false.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html/>"))
	}))
	defer srv.Close()

	f := scrapefetch.New(scrapefetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(), ports.FetchRequest{
		Source: snap(source.MethodScrapeHTML, srv.URL+"/a/", srv.URL+"/b/"),
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 2 || res.Partial {
		t.Errorf("got %d items partial=%v, want 2 partial=false", len(res.Items), res.Partial)
	}
}

func TestFetchNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	f := scrapefetch.New(scrapefetch.WithHTTPClient(srv.Client()))
	_, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodScrapeHTML, srv.URL+"/missing/")})
	if !errors.Is(err, scrapefetch.ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch", err)
	}
}

func TestFetchCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html/>"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the request is issued

	f := scrapefetch.New(scrapefetch.WithHTTPClient(srv.Client()))
	_, err := f.Fetch(ctx,
		ports.FetchRequest{Source: snap(source.MethodScrapeHTML, srv.URL+"/a/")})
	if !errors.Is(err, scrapefetch.ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want wrapped context.Canceled", err)
	}
}

func TestFetchSizeCap(t *testing.T) {
	body := strings.Repeat("x", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := scrapefetch.New(scrapefetch.WithHTTPClient(srv.Client()), scrapefetch.WithMaxBytes(10))
	_, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodScrapeHTML, srv.URL+"/big/")})
	if !errors.Is(err, scrapefetch.ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch for over-cap body", err)
	}
}

func TestFetchSizeCapBoundary(t *testing.T) {
	body := strings.Repeat("x", 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := scrapefetch.New(scrapefetch.WithHTTPClient(srv.Client()), scrapefetch.WithMaxBytes(10))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodScrapeHTML, srv.URL+"/exact/")})
	if err != nil {
		t.Fatalf("Fetch at exact cap: %v", err)
	}
	if got, _ := res.Items[0].Raw["content"].(string); len(got) != 10 {
		t.Errorf("content length = %d, want 10 (kept whole at cap)", len(got))
	}
}
