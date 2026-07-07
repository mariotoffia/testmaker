// Package filecatalog is a driving adapter implementing ports.CatalogLoader. It
// reads the research source catalogue (catalog/sources.json or sources.yaml)
// and maps each wire record through source.NewSource, so only valid sources
// reach the application. Wire-format knowledge (JSON/YAML tags) is confined to
// this adapter — the domain stays serialization-free.
package filecatalog
