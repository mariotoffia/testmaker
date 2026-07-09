package apifetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// Adapter error sentinels. Callers match by Code with errors.Is; the wrapped
// cause (transport error, context.Canceled) stays reachable through Unwrap.
var (
	// ErrFetch marks a failed request: it could not be built or sent, was
	// cancelled, the server answered with a non-2xx status, or the body ran
	// past the size cap.
	ErrFetch = &shared.TestmakerError{
		Code: "apifetch.fetch", Class: shared.ClassUnavailable, Message: "api request failed",
	}
	// ErrDecode marks a JSON endpoint that answered with a body that is not
	// valid JSON — a contract breach for an api source.
	ErrDecode = &shared.TestmakerError{
		Code: "apifetch.decode", Class: shared.ClassInvalid, Message: "api response is not valid JSON",
	}
)

// defaultTimeout bounds a single request when the caller does not supply their
// own HTTP client. A shorter per-call context deadline still wins.
const defaultTimeout = 60 * time.Second

// defaultMaxBytes caps how many bytes the adapter reads from one endpoint,
// guarding against a hostile or runaway server.
// ponytail: fixed 16 MiB ceiling; a paged JSON response is a few hundred KiB.
// Raise it with WithMaxBytes for a heavier endpoint rather than streaming.
const defaultMaxBytes = 16 << 20

// userAgent identifies the client. Some JSON APIs (notably the Wikimedia API)
// reject the stdlib default with 403 and require a descriptive User-Agent.
const userAgent = "testmaker/0.1 (+https://github.com/mariotoffia/testmaker)"

// Fetcher is the JSON-API adapter. The zero value is not usable; call New.
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

// WithMaxBytes overrides the per-response read cap. A non-positive value is
// ignored.
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

// Supports reports whether the source is fetched from a JSON API.
func (f *Fetcher) Supports(snap source.Snapshot) bool {
	return snap.Extraction.Method == source.MethodAPI
}

// Fetch GETs the source's JSON endpoints and returns one RawItem per endpoint,
// with the JSON body inlined into Raw["content"]. Non-endpoint URLs (human
// pages) are skipped. Limit, when positive, caps the number of endpoints
// returned and sets Partial.
func (f *Fetcher) Fetch(ctx context.Context, req ports.FetchRequest) (ports.FetchResult, error) {
	res := ports.FetchResult{SourceID: req.Source.ID}
	urls := apiURLs(req.Source.URLs)
	if len(urls) == 0 {
		res.Note = "apifetch: no JSON endpoint (expected a url with format=json/output=json or a .json path)"
		return res, nil
	}
	for i, u := range urls {
		if req.Limit > 0 && len(res.Items) >= req.Limit {
			res.Partial = true
			break
		}
		item, err := f.fetchEndpoint(ctx, u)
		if err != nil {
			return ports.FetchResult{}, err
		}
		res.Items = append(res.Items, item)
		if req.Limit > 0 && len(res.Items) >= req.Limit && i < len(urls)-1 {
			res.Partial = true
			break
		}
	}
	res.Note = fmt.Sprintf("apifetch: %d endpoint(s) from %d json url(s)", len(res.Items), len(urls))
	return res, nil
}

// fetchEndpoint GETs one endpoint and returns its JSON body as a RawItem.
func (f *Fetcher) fetchEndpoint(ctx context.Context, rawURL string) (ports.RawItem, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ports.RawItem{}, ErrFetch.WithMessagef("build request for %s", rawURL).Wrap(err)
	}
	httpReq.Header.Set("User-Agent", userAgent)
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
	if !json.Valid(body) {
		return ports.RawItem{}, ErrDecode.WithMessagef("endpoint %s did not return JSON", rawURL)
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

// apiURLs keeps only the URLs that look like a JSON endpoint. An api source
// often also lists human pages (a landing page, a category page); those are
// skipped so the adapter never GETs HTML it cannot use.
func apiURLs(urls []string) []string {
	var out []string
	for _, u := range urls {
		if isAPIURL(u) {
			out = append(out, u)
		}
	}
	return out
}

// isAPIURL reports whether a URL names a JSON endpoint: a query parameter asks
// for JSON (format=json / output=json) or the path ends in .json. Matching the
// parsed query — not a raw substring — keeps a human page whose path merely
// contains "format=json" from being treated as an endpoint.
func isAPIURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	q := parsed.Query()
	if strings.EqualFold(q.Get("format"), "json") || strings.EqualFold(q.Get("output"), "json") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(parsed.Path), ".json")
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
