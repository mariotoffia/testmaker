// Package filecatalog is an adapter implementing the ports.CatalogLoader
// driven port. It reads the research source catalogue (data/catalog/sources.json
// or sources.yaml) and maps each wire record through source.NewSource, so only
// valid sources reach the application. Wire-format knowledge (JSON/YAML tags)
// is confined to this adapter — the domain stays serialization-free.
package filecatalog
