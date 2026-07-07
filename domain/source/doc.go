// Package source is the "source catalogue" bounded context.
//
// A Source is a catalogued place from which cognitive-test items can be
// obtained — an open dataset, a generator repo, a downloadable PDF, a scrapable
// site, or a commercial vendor whose format may be mirrored. Sources are the
// input to the Fetcher (which pulls raw items) and to the item bank.
//
// The aggregate root is Source; it is validated on construction (NewSource) and
// crosses ports as a Snapshot DTO. Redistributable() is the load-bearing field:
// it gates whether a source's items may be reused or only mirrored as a format.
//
// This package is pure (stdlib + the shared kernel only) and carries no
// serialization tags — wire formats are the concern of the file adapter.
package source

import "github.com/mariotoffia/testmaker/domain/shared"

// Source-context sentinels.
var (
	// ErrInvalidSource is returned when a SourceSpec violates an invariant.
	ErrInvalidSource = &shared.TestmakerError{
		Code: "source.invalid", Class: shared.ClassInvalid, Message: "invalid source",
	}
	// ErrUnknownSource is returned when a source id is not in the catalogue.
	ErrUnknownSource = &shared.TestmakerError{
		Code: "source.unknown", Class: shared.ClassNotFound, Message: "unknown source",
	}
)
