package httpfetch_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/httpfetch"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// Compile-time proof the adapter satisfies the port (kept out of production code).
var _ ports.Fetcher = (*httpfetch.Fetcher)(nil)

// snap builds a minimal source snapshot with the given method and URLs.
func snap(method source.ExtractionMethod, urls ...string) source.Snapshot {
	return source.Snapshot{
		ID:         "test-source",
		URLs:       urls,
		Extraction: source.Extraction{Method: method},
	}
}

// makeZip builds a deterministic in-memory zip from name->content.
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatalf("zip create %s: %v", n, err)
		}
		if _, err := w.Write([]byte(files[n])); err != nil {
			t.Fatalf("zip write %s: %v", n, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestSupports(t *testing.T) {
	f := httpfetch.New()
	if !f.Supports(snap(source.MethodDirectDownload)) {
		t.Errorf("expected support for direct-download")
	}
	if f.Supports(snap(source.MethodScrapeHTML)) {
		t.Errorf("did not expect support for scrape-html")
	}
}

func TestFetchZipArtifacts(t *testing.T) {
	zipBytes := makeZip(t, map[string]string{
		"VIQT_data/codebook.txt": "Q1\t24\ttiny faded new large big",
		"VIQT_data/data.csv":     "Q1\n24\n3",
	})

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/_rawdata/VIQT_data.zip")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Wire request assertion.
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/_rawdata/VIQT_data.zip" {
		t.Errorf("path = %q, want /_rawdata/VIQT_data.zip", gotPath)
	}

	if res.SourceID != "test-source" {
		t.Errorf("SourceID = %q", res.SourceID)
	}
	if len(res.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(res.Items))
	}
	// Shape: each member becomes a RawItem keyed by its path with inlined text.
	byID := map[string]ports.RawItem{}
	for _, it := range res.Items {
		byID[it.ExternalID] = it
	}
	cb, ok := byID["VIQT_data/codebook.txt"]
	if !ok {
		t.Fatalf("missing codebook RawItem; got %v", res.Items)
	}
	content, _ := cb.Raw["content"].(string)
	if !strings.Contains(content, "tiny faded new large big") {
		t.Errorf("codebook content not inlined: %q", content)
	}
	if _, ok := byID["VIQT_data/data.csv"]; !ok {
		t.Errorf("missing data.csv RawItem")
	}
}

func TestFetchPlainCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("a,b\n1,2\n"))
	}))
	defer srv.Close()

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/data/items.csv")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(res.Items))
	}
	if res.Items[0].ExternalID != "items.csv" {
		t.Errorf("ExternalID = %q", res.Items[0].ExternalID)
	}
	if got, _ := res.Items[0].Raw["content"].(string); got != "a,b\n1,2\n" {
		t.Errorf("content = %q", got)
	}
}

func TestFetchNoDataURL(t *testing.T) {
	// A direct-download source that only lists human pages yields nothing and
	// makes no request (no data-file extension present).
	f := httpfetch.New()
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload,
			"https://example.org/tests/VIQT/", "https://example.org/paper/62/")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 0 {
		t.Errorf("got %d items, want 0", len(res.Items))
	}
	if !strings.Contains(res.Note, "no downloadable data URL") {
		t.Errorf("Note = %q", res.Note)
	}
}

func TestFetchLimit(t *testing.T) {
	zipBytes := makeZip(t, map[string]string{"a.txt": "1", "b.txt": "2", "c.txt": "3"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/a.zip"), Limit: 2})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 2 || !res.Partial {
		t.Errorf("got %d items partial=%v, want 2 partial=true", len(res.Items), res.Partial)
	}
}

func TestFetchNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()))
	_, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/missing.csv")})
	if !errors.Is(err, httpfetch.ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch", err)
	}
}

func TestFetchMalformedZip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not a real zip"))
	}))
	defer srv.Close()

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()))
	_, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/bad.zip")})
	if !errors.Is(err, httpfetch.ErrDecode) {
		t.Fatalf("err = %v, want ErrDecode", err)
	}
}

func TestFetchCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("a,b\n"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the request is issued

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()))
	_, err := f.Fetch(ctx,
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/data.csv")})
	if !errors.Is(err, httpfetch.ErrFetch) {
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

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()), httpfetch.WithMaxBytes(10))
	_, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/big.csv")})
	// An oversized artifact must fail, not silently truncate to a valid-looking
	// prefix (which would corrupt downstream data).
	if !errors.Is(err, httpfetch.ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch for over-cap body", err)
	}
}

func TestFetchSizeCapBoundary(t *testing.T) {
	// Exactly at the cap must pass and keep the whole body.
	body := strings.Repeat("x", 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()), httpfetch.WithMaxBytes(10))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/exact.csv")})
	if err != nil {
		t.Fatalf("Fetch at exact cap: %v", err)
	}
	if got, _ := res.Items[0].Raw["content"].(string); len(got) != 10 {
		t.Errorf("content length = %d, want 10 (kept whole at cap)", len(got))
	}
}

func TestFetchZipMemberSizeCap(t *testing.T) {
	// A single over-cap zip member must fail rather than ingest a truncated file.
	// The member is highly compressible so the zip BODY stays under the cap while
	// its uncompressed size exceeds it — isolating the member-cap branch (a body
	// over the cap would be rejected earlier and pass this test for free).
	member := strings.Repeat("y", 100_000)
	zipBytes := makeZip(t, map[string]string{"big.txt": member})
	const capBytes = 5000
	if len(zipBytes) > capBytes {
		t.Fatalf("zip body %d bytes must stay <= cap %d to isolate the member branch", len(zipBytes), capBytes)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()), httpfetch.WithMaxBytes(capBytes))
	_, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/a.zip")})
	if !errors.Is(err, httpfetch.ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch for over-cap zip member", err)
	}
}

func TestFetchLimitAcrossURLs(t *testing.T) {
	// Two data URLs, Limit 1: only the first is fetched, and Partial reports the
	// second URL was left unread even though the count landed exactly on Limit.
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte("col\n1\n"))
	}))
	defer srv.Close()

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(), ports.FetchRequest{
		Source: snap(source.MethodDirectDownload, srv.URL+"/a.csv", srv.URL+"/b.csv"),
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 1 || !res.Partial {
		t.Errorf("got %d items partial=%v, want 1 partial=true", len(res.Items), res.Partial)
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1 (second URL must not be fetched past the limit)", requests)
	}
}

func TestFetchZipLimitExactNotPartial(t *testing.T) {
	// A single-file zip with Limit 1 fills the budget but nothing more exists:
	// Partial must be false.
	zipBytes := makeZip(t, map[string]string{"only.txt": "1"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	f := httpfetch.New(httpfetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodDirectDownload, srv.URL+"/a.zip"), Limit: 1})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 1 || res.Partial {
		t.Errorf("got %d items partial=%v, want 1 partial=false", len(res.Items), res.Partial)
	}
}
