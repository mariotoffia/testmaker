// Package stubfetcher is a no-op adapter implementing ports.Fetcher. It exists
// to wire the fetch boundary end-to-end before the real fetch adapters
// (direct-download, scrape-html, headless-browser, git-clone/generate) are
// built — one per source.Extraction.Method. It reports support for any source
// and returns an empty result.
package stubfetcher
