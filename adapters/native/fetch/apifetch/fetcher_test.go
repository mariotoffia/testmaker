package apifetch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/fetch/apifetch"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// Compile-time proof the adapter satisfies the port (kept out of production code).
var _ ports.Fetcher = (*apifetch.Fetcher)(nil)

// snap builds a minimal source snapshot with the given method and URLs.
func snap(method source.ExtractionMethod, urls ...string) source.Snapshot {
	return source.Snapshot{
		ID:         "test-source",
		URLs:       urls,
		Extraction: source.Extraction{Method: method},
	}
}

func TestSupports(t *testing.T) {
	f := apifetch.New()
	if !f.Supports(snap(source.MethodAPI)) {
		t.Errorf("expected support for api")
	}
	if f.Supports(snap(source.MethodScrapeHTML)) {
		t.Errorf("did not expect support for scrape-html")
	}
}

func TestFetchEndpoint(t *testing.T) {
	const jsonBody = `{"query":{"pages":{"42":{"title":"File:X.jpg"}}}}`
	var gotMethod, gotQuery, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotQuery, gotUA = r.Method, r.URL.RawQuery, r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonBody))
	}))
	defer srv.Close()

	f := apifetch.New(apifetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodAPI, srv.URL+"/w/api.php?action=query&format=json")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if !strings.Contains(gotQuery, "format=json") {
		t.Errorf("query = %q, want it to carry format=json", gotQuery)
	}
	// A descriptive User-Agent is required by some APIs (Wikimedia 403s without one).
	if !strings.Contains(gotUA, "testmaker") {
		t.Errorf("User-Agent = %q, want it to identify testmaker", gotUA)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(res.Items))
	}
	if got, _ := res.Items[0].Raw["content"].(string); got != jsonBody {
		t.Errorf("content = %q, want the JSON body verbatim", got)
	}
}

func TestFetchSkipsNonAPIURL(t *testing.T) {
	// A human landing page listed alongside the endpoint must not be requested:
	// apifetch filters to JSON endpoints before making any call.
	var apiRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "format=json") {
			apiRequests++
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		t.Errorf("unexpected request to non-api url: %s", r.URL)
	}))
	defer srv.Close()

	f := apifetch.New(apifetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(), ports.FetchRequest{Source: snap(source.MethodAPI,
		srv.URL+"/wiki/Category:Intelligence_tests",
		srv.URL+"/w/api.php?action=query&format=json")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items, want 1 (only the JSON endpoint)", len(res.Items))
	}
	if apiRequests != 1 {
		t.Errorf("api requests = %d, want 1", apiRequests)
	}
}

func TestFetchDotJSONPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[1,2,3]`))
	}))
	defer srv.Close()

	f := apifetch.New(apifetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodAPI, srv.URL+"/data/items.json")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(res.Items))
	}
	if res.Items[0].ExternalID != "items.json" {
		t.Errorf("ExternalID = %q, want items.json", res.Items[0].ExternalID)
	}
}

func TestFetchNoEndpoint(t *testing.T) {
	f := apifetch.New()
	res, err := f.Fetch(context.Background(), ports.FetchRequest{Source: snap(source.MethodAPI,
		"https://example.org/wiki/Category:Tests", "https://example.org/about")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 0 {
		t.Errorf("got %d items, want 0", len(res.Items))
	}
	if !strings.Contains(res.Note, "no JSON endpoint") {
		t.Errorf("Note = %q", res.Note)
	}
}

func TestFetchIgnoresFormatJSONInPath(t *testing.T) {
	// A human page whose path merely contains "format=json" is not an endpoint;
	// it must be skipped, not GET'd (which would return HTML and abort the source).
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`<html>a blog post about format=json</html>`))
	}))
	defer srv.Close()

	f := apifetch.New(apifetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodAPI, srv.URL+"/blog/using-format=json-in-mediawiki")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if hits != 0 {
		t.Errorf("server got %d hits, want 0 (path match must not fetch)", hits)
	}
	if len(res.Items) != 0 {
		t.Errorf("got %d items, want 0", len(res.Items))
	}
}

func TestFetchNonJSONIsDecodeErr(t *testing.T) {
	// A url that claims to be JSON (format=json) but answers with HTML is a
	// contract breach: apifetch must reject it, not inline garbage.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer srv.Close()

	f := apifetch.New(apifetch.WithHTTPClient(srv.Client()))
	_, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodAPI, srv.URL+"/api?format=json")})
	if !errors.Is(err, apifetch.ErrDecode) {
		t.Fatalf("err = %v, want ErrDecode", err)
	}
}

func TestFetchLimitAcrossURLs(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	f := apifetch.New(apifetch.WithHTTPClient(srv.Client()))
	res, err := f.Fetch(context.Background(), ports.FetchRequest{
		Source: snap(source.MethodAPI, srv.URL+"/a.json", srv.URL+"/b.json"),
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Items) != 1 || !res.Partial {
		t.Errorf("got %d items partial=%v, want 1 partial=true", len(res.Items), res.Partial)
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1 (second endpoint must not be fetched past the limit)", requests)
	}
}

func TestFetchNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	f := apifetch.New(apifetch.WithHTTPClient(srv.Client()))
	_, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodAPI, srv.URL+"/missing.json")})
	if !errors.Is(err, apifetch.ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch", err)
	}
}

func TestFetchCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the request is issued

	f := apifetch.New(apifetch.WithHTTPClient(srv.Client()))
	_, err := f.Fetch(ctx,
		ports.FetchRequest{Source: snap(source.MethodAPI, srv.URL+"/a.json")})
	if !errors.Is(err, apifetch.ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want wrapped context.Canceled", err)
	}
}

func TestFetchSizeCap(t *testing.T) {
	// A valid but oversized JSON body must fail rather than truncate to invalid
	// JSON or a partial payload.
	body := "[" + strings.Repeat("1,", 100) + "1]"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := apifetch.New(apifetch.WithHTTPClient(srv.Client()), apifetch.WithMaxBytes(10))
	_, err := f.Fetch(context.Background(),
		ports.FetchRequest{Source: snap(source.MethodAPI, srv.URL+"/big.json")})
	if !errors.Is(err, apifetch.ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch for over-cap body", err)
	}
}
