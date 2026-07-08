// Package item is the "item bank" bounded context — the store of concrete,
// scored test items (questions) with a stimulus, options, an answer key,
// difficulty and provenance back to a source.
//
// The aggregate root is Item. It is validated on construction (NewItem) and
// crosses ports only as an ItemSnapshot DTO; RehydrateFromSnapshot rebuilds a
// trusted snapshot without re-validating. One aggregate serves every ability
// family (figural, numerical, verbal, spatial, speed): they differ only in
// TestType, Stimulus media and AnswerFormat, which keeps the bank, generator
// and renderer uniform. Media is stored by reference (blob key / URL), never as
// bytes, so the aggregate stays small and serializable.
//
// The A1..E2 taxonomy and the Redistributable reuse gate live in the shared
// kernel (domain/shared); this package consumes them so source, item and
// testset never drift.
package item
