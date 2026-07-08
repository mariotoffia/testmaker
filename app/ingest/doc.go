// Package ingest is the application service (use-case layer) that turns a
// catalogued source into validated item-bank items. It routes a source to a
// Fetcher (by ports.Fetcher.Supports), asks a source-specific Normalizer to map
// the fetched RawItems into item specs, validates each spec through
// item.NewItem (the untrusted-input gate), and persists the survivors via
// ports.ItemRepository.
//
// It orchestrates driven ports only (Fetcher, ItemRepository) and normalizers,
// holding no wire-format, HTTP, or storage knowledge of its own. Normalizers
// are pure functions registered per source id in the composition root, so this
// package stays source-agnostic while the messy, per-source shape-mapping lives
// in one small function each (see viqt.go for the reference normalizer).
package ingest
