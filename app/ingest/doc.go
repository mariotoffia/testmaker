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
//
// IngestLLM (extract.go) is the second lifting path: instead of a deterministic
// per-source normalizer it hands the fetched payload to an app/llm.Service under
// a JSON schema, so an LLM lifts unstructured artifacts into item candidates.
// LLM output is untrusted, so every candidate passes the same item.NewItem gate
// before it is stored; survivors are tagged item.OriginGenerated and the run's
// model and prompt provenance are recorded in the Report.
package ingest
