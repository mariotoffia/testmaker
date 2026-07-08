package httpfetch

import (
	"archive/zip"
	"bytes"
	"context"
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
// cause (transport error, zip error) stays reachable through Unwrap, so a
// cancelled request still satisfies errors.Is(err, context.Canceled).
var (
	// ErrFetch marks a failed download: the request could not be built or sent,
	// was cancelled, or the server answered with a non-2xx status.
	ErrFetch = &shared.TestmakerError{
		Code: "httpfetch.fetch", Class: shared.ClassUnavailable, Message: "download failed",
	}
	// ErrDecode marks a downloaded artifact the adapter could not read as the
	// container its extension promised (e.g. a corrupt .zip).
	ErrDecode = &shared.TestmakerError{
		Code: "httpfetch.decode", Class: shared.ClassInvalid, Message: "could not decode downloaded artifact",
	}
)

// defaultTimeout bounds a single download when the caller does not supply their
// own HTTP client. A shorter per-call context deadline still takes precedence.
const defaultTimeout = 60 * time.Second

// defaultMaxBytes caps how many bytes the adapter reads from one URL (or one
// zip member), guarding against a hostile or runaway server.
// ponytail: fixed 64 MiB ceiling; the open datasets in scope are a few MiB.
// Raise it with WithMaxBytes for a larger source rather than streaming.
const defaultMaxBytes = 64 << 20

// Fetcher is the direct-download adapter. The zero value is not usable; call New.
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

// WithMaxBytes overrides the per-artifact read cap. A non-positive value is
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

// Supports reports whether the source is fetched by direct download.
func (f *Fetcher) Supports(snap source.Snapshot) bool {
	return snap.Extraction.Method == source.MethodDirectDownload
}

// Fetch downloads the source's data URLs and returns one RawItem per artifact
// or archive member. Non-data URLs (test pages, papers) are skipped. Limit, when
// positive, caps the number of RawItems returned and sets Partial.
func (f *Fetcher) Fetch(ctx context.Context, req ports.FetchRequest) (ports.FetchResult, error) {
	res := ports.FetchResult{SourceID: req.Source.ID}
	urls := dataURLs(req.Source.URLs)
	if len(urls) == 0 {
		res.Note = "httpfetch: no downloadable data URL (expected one of .zip/.csv/.tsv/.json)"
		return res, nil
	}
	for i, u := range urls {
		remaining := 0
		if req.Limit > 0 {
			remaining = req.Limit - len(res.Items)
		}
		items, truncated, err := f.fetchURL(ctx, u, remaining)
		if err != nil {
			return ports.FetchResult{}, err
		}
		res.Items = append(res.Items, items...)
		if req.Limit > 0 && len(res.Items) >= req.Limit {
			// More items exist if this URL's archive was cut short by the budget,
			// or the batch overshot, or unread URLs remain.
			res.Partial = truncated || len(res.Items) > req.Limit || i < len(urls)-1
			if len(res.Items) > req.Limit {
				res.Items = res.Items[:req.Limit]
			}
			break
		}
	}
	res.Note = fmt.Sprintf("httpfetch: %d artifact(s) from %d data url(s)", len(res.Items), len(urls))
	return res, nil
}

// fetchURL downloads one URL and expands it into RawItems (unzipping a .zip).
// limit, when positive, caps how many RawItems this URL contributes (an archive
// stops expanding once met); truncated reports that it was cut short.
func (f *Fetcher) fetchURL(ctx context.Context, rawURL string, limit int) (items []ports.RawItem, truncated bool, err error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, ErrFetch.WithMessagef("build request for %s", rawURL).Wrap(err)
	}
	resp, err := f.http.Do(httpReq)
	if err != nil {
		return nil, false, ErrFetch.WithMessagef("GET %s", rawURL).Wrap(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, ErrFetch.WithMessagef("GET %s returned status %d", rawURL, resp.StatusCode).
			With("status", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, false, ErrFetch.WithMessagef("read body from %s", rawURL).Wrap(err)
	}
	if int64(len(body)) > f.maxBytes {
		return nil, false, ErrFetch.WithMessagef("body from %s exceeds %d-byte cap", rawURL, f.maxBytes).
			With("cap", f.maxBytes)
	}

	name := path.Base(urlPath(rawURL))
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		return f.unzip(ctx, body, rawURL, limit)
	}
	return []ports.RawItem{artifact(name, rawURL, body)}, false, nil
}

// unzip expands an in-memory zip into one RawItem per file member. It honours
// ctx between members and stops once limit (when positive) members are read, so
// a cancelled or capped fetch does not walk a huge archive to the end;
// truncated reports that members were left unread.
func (f *Fetcher) unzip(ctx context.Context, body []byte, srcURL string, limit int) (items []ports.RawItem, truncated bool, err error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, false, ErrDecode.WithMessagef("open zip from %s", srcURL).Wrap(err)
	}
	items = make([]ports.RawItem, 0, len(zr.File))
	for i, zf := range zr.File {
		if err := ctx.Err(); err != nil {
			return nil, false, ErrFetch.WithMessagef("cancelled expanding zip from %s", srcURL).Wrap(err)
		}
		if zf.FileInfo().IsDir() {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, false, ErrDecode.WithMessagef("open zip member %s from %s", zf.Name, srcURL).Wrap(err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, f.maxBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, false, ErrDecode.WithMessagef("read zip member %s from %s", zf.Name, srcURL).Wrap(err)
		}
		if int64(len(content)) > f.maxBytes {
			return nil, false, ErrFetch.WithMessagef("zip member %s from %s exceeds %d-byte cap",
				zf.Name, srcURL, f.maxBytes).With("cap", f.maxBytes)
		}
		items = append(items, artifact(zf.Name, srcURL, content))
		if limit > 0 && len(items) >= limit {
			// Partial only if a later member would still have been yielded.
			truncated = hasFileMember(zr.File[i+1:])
			break
		}
	}
	return items, truncated, nil
}

// hasFileMember reports whether any entry is a non-directory file.
func hasFileMember(files []*zip.File) bool {
	for _, zf := range files {
		if !zf.FileInfo().IsDir() {
			return true
		}
	}
	return false
}

// artifact builds a RawItem for one downloaded file. Text is inlined into
// Raw["content"]; binary content is referenced by Media (name), leaving the
// bytes for a blob store to pull.
func artifact(name, srcURL string, content []byte) ports.RawItem {
	it := ports.RawItem{
		ExternalID: name,
		Raw: map[string]any{
			"path":       name,
			"source_url": srcURL,
		},
	}
	if isText(name) {
		it.Raw["content"] = string(content)
	} else {
		it.Media = []string{name}
		it.Raw["bytes"] = len(content)
	}
	return it
}

// dataURLs keeps only the URLs that point at a downloadable data file. A
// direct-download source usually also lists human pages (the test page, a
// paper); those are skipped so the adapter never scrapes HTML.
func dataURLs(urls []string) []string {
	var out []string
	for _, u := range urls {
		if isDataURL(urlPath(u)) {
			out = append(out, u)
		}
	}
	return out
}

// isDataURL reports whether a path points at a downloadable data file.
func isDataURL(p string) bool {
	return hasSuffixFold(p, ".zip", ".csv", ".tsv", ".json")
}

// isText reports whether a file name should be inlined as text (vs referenced
// via Media as an opaque blob).
func isText(name string) bool {
	return hasSuffixFold(name, ".txt", ".csv", ".tsv", ".json", ".md", ".xml", ".yaml", ".yml")
}

// hasSuffixFold reports whether s ends with any of the suffixes, case-insensitively.
func hasSuffixFold(s string, suffixes ...string) bool {
	low := strings.ToLower(s)
	for _, suf := range suffixes {
		if strings.HasSuffix(low, suf) {
			return true
		}
	}
	return false
}

// urlPath returns the path component of a URL, or the raw string if it will not
// parse (so a bare filename still routes by extension).
func urlPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return parsed.Path
}
