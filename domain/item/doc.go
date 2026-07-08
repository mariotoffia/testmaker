// Package item is the "item bank" bounded context — the store of concrete,
// scored test items (questions) with stems, options, keys, difficulty and
// provenance back to a source.
//
// SCAFFOLD: only the identifiers and DTO shells referenced by ports are present
// so the workspace compiles. The aggregate (Item), its value objects (Stimulus,
// Option, AnswerKey, Difficulty) and invariants are modeled in the "Item Bank"
// implementation block. The A1..E2 taxonomy currently in domain/source will be
// promoted to a shared taxonomy package during that block.
package item

import "github.com/mariotoffia/testmaker/domain/shared"

// Item-context sentinels.
var (
	// ErrInvalidItem is returned when an item snapshot violates an invariant.
	ErrInvalidItem = &shared.TestmakerError{
		Code: "item.invalid", Class: shared.ClassInvalid, Message: "invalid item",
	}
	// ErrUnknownItem is returned when an item id is not in the bank.
	ErrUnknownItem = &shared.TestmakerError{
		Code: "item.unknown", Class: shared.ClassNotFound, Message: "unknown item",
	}
)

// ItemID uniquely identifies a bank item.
type ItemID string

// ItemSnapshot is a placeholder persistence/transport DTO for a bank item.
// Fields will expand when the Item Bank block is implemented.
type ItemSnapshot struct {
	ID       ItemID
	SourceID string // provenance -> source.SourceID
	Stem     string
}

// ItemFilter is a placeholder query object for the item bank.
type ItemFilter struct{}
