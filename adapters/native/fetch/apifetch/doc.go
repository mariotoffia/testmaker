// Package apifetch is the JSON-API ports.Fetcher: it GETs a source's JSON
// endpoints and returns each response body as a loose RawItem for a normalizer
// to parse into bank items (or, for a keyless media source, into figure
// references).
//
// It reports support for a source whose Extraction.Method is api. Fetch GETs
// every source URL that looks like a JSON endpoint (query carries format=json /
// output=json, or the path ends in .json); other URLs the source lists (a human
// landing page, a category page) are skipped so the adapter never scrapes HTML.
// A fetched body must parse as JSON — a filtered-in endpoint that answers with
// non-JSON is a contract breach and surfaces as ErrDecode, not silently inlined.
// Each JSON body is inlined verbatim into Raw["content"]; the adapter does not
// interpret the payload — that is the normalizer's job (app/ingest). Limit, when
// positive, caps the number of endpoints returned and sets Partial.
//
// It depends on the standard library alone (net/http, encoding/json) plus the
// domain shared kernel for its error sentinels, so it carries no vendor
// dependency. A response body is read under a size cap; transport failures,
// non-2xx statuses, an over-cap body (ErrFetch) and non-JSON bodies (ErrDecode)
// surface as errors matched by Code via errors.Is, with the cause reachable
// through Unwrap.
package apifetch
