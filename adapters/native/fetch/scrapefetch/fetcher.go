package scrapefetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// ErrFetch marks a failed page load: the request could not be built or sent,
// was cancelled, the server answered with a non-2xx status, or the body ran
// past the size cap. Callers match by Code with errors.Is; the wrapped cause
// (transport error, context.Canceled) stays reachable through Unwrap.
var ErrFetch = &shared.TestmakerError{
	Code: "scrapefetch.fetch", Class: shared.ClassUnavailable, Message: "page load failed",
}

// defaultTimeout bounds a single page load when the caller does not supply
// their own HTTP client. A shorter per-call context deadline still wins.
const defaultTimeout = 60 * time.Second

// defaultMaxBytes caps how many bytes the adapter reads from one page, guarding
// against a hostile or runaway server.
// ponytail: fixed 16 MiB ceiling; a quiz page is a few hundred KiB. Raise it
// with WithMaxBytes for a heavier page rather than streaming.
const defaultMaxBytes = 16 << 20

// Fetcher is the HTML-scrape adapter. The zero value is not usable; call New.
type Fetcher struct {
	http     *http.Client
	maxBytes int64
}

// Option configures a Fetcher.
type Option func(*Fetcher)

// WithHTTPClient overrides the default HTTP client (e.g. to inject a transport
// or a test server client). A nil client is ignored.
func WithHTTPClient(c *http.Client) Option {
	return func(f *Fetcher) {
		if c != nil {
			f.http = c
		}
	}
}

// WithMaxBytes overrides the per-page read cap. A non-positive value is ignored.
func WithMaxBytes(n int64) Option {
	return func(f *Fetcher) {
		if n > 0 {
			f.maxBytes = n
		}
	}
}

// New builds a Fetcher with a default HTTP client and size cap.
func New(opts ...Option) *Fetcher {
	f := &Fetcher{
		http:     &http.Client{Timeout: defaultTimeout},
		maxBytes: defaultMaxBytes,
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Supports reports whether the source is fetched by scraping HTML pages.
func (f *Fetcher) Supports(snap source.Snapshot) bool {
	return snap.Extraction.Method == source.MethodScrapeHTML
}

// Fetch GETs the source's pages and returns one RawItem per page, with the raw
// HTML inlined into Raw["content"]. Limit, when positive, caps the number of
// pages returned and sets Partial.
func (f *Fetcher) Fetch(ctx context.Context, req ports.FetchRequest) (ports.FetchResult, error) {
	res := ports.FetchResult{SourceID: req.Source.ID}
	urls := req.Source.URLs
	if len(urls) == 0 {
		res.Note = "scrapefetch: source lists no page URL to scrape"
		return res, nil
	}
	for i, u := range urls {
		if req.Limit > 0 && len(res.Items) >= req.Limit {
			res.Partial = true
			break
		}
		item, err := f.fetchPage(ctx, u)
		if err != nil {
			return ports.FetchResult{}, err
		}
		res.Items = append(res.Items, item)
		if req.Limit > 0 && len(res.Items) >= req.Limit && i < len(urls)-1 {
			res.Partial = true
			break
		}
	}
	res.Note = fmt.Sprintf("scrapefetch: %d page(s) from %d url(s)", len(res.Items), len(urls))
	return res, nil
}

// fetchPage GETs one URL and returns its HTML as a RawItem.
func (f *Fetcher) fetchPage(ctx context.Context, rawURL string) (ports.RawItem, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ports.RawItem{}, ErrFetch.WithMessagef("build request for %s", rawURL).Wrap(err)
	}
	resp, err := f.http.Do(httpReq)
	if err != nil {
		return ports.RawItem{}, ErrFetch.WithMessagef("GET %s", rawURL).Wrap(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ports.RawItem{}, ErrFetch.WithMessagef("GET %s returned status %d", rawURL, resp.StatusCode).
			With("status", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return ports.RawItem{}, ErrFetch.WithMessagef("read body from %s", rawURL).Wrap(err)
	}
	if int64(len(body)) > f.maxBytes {
		return ports.RawItem{}, ErrFetch.WithMessagef("body from %s exceeds %d-byte cap", rawURL, f.maxBytes).
			With("cap", f.maxBytes)
	}

	name := path.Base(urlPath(rawURL))
	return ports.RawItem{
		ExternalID: name,
		Raw: map[string]any{
			"path":       name,
			"source_url": rawURL,
			"content":    string(body),
		},
	}, nil
}

// urlPath returns the path component of a URL, or the raw string if it will not
// parse (so a bare name still yields a stable ExternalID).
func urlPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return parsed.Path
}
