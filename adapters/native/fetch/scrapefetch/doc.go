// Package scrapefetch is the HTML-scrape ports.Fetcher: it GETs a source's web
// pages and returns each page's raw HTML as a loose RawItem for a normalizer to
// parse into bank items.
//
// It reports support for a source whose Extraction.Method is scrape-html. Fetch
// GETs every URL the source lists, inlines the response body verbatim into
// Raw["content"] (the page HTML), and yields one RawItem per page. It does not
// parse the markup — turning a quiz page into keyed items is the normalizer's
// job (app/ingest), which keeps this adapter a generic, source-agnostic page
// puller. Limit, when positive, caps the number of pages returned and sets
// Partial.
//
// It depends on the standard library alone (net/http) plus the domain shared
// kernel for its error sentinels, so it carries no vendor dependency. A
// response body is read under a size cap; only transport failures, non-2xx
// statuses and an over-cap body surface as errors (matched by Code via
// errors.Is, with the cause — e.g. context.Canceled — reachable through
// Unwrap).
package scrapefetch
