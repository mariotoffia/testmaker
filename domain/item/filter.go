package item

import (
	"slices"

	"github.com/mariotoffia/testmaker/domain/shared"
)

// ItemFilter expresses item-bank query criteria. An empty filter matches
// everything; each populated field narrows the result set (AND across fields,
// OR within a multi-value field). Difficulty bounds are inclusive; a zero bound
// means "unbounded" (band is always >= 1, so 0 can never be a real lower/upper
// value).
type ItemFilter struct {
	Families        []shared.AbilityFamily
	TestTypes       []shared.TestTypeCode
	Origins         []Origin
	Redistributable []shared.Redistributable
	MinDifficulty   int
	MaxDifficulty   int
}

// Matches reports whether a snapshot satisfies the filter.
func (f ItemFilter) Matches(s ItemSnapshot) bool {
	if len(f.Families) > 0 && !slices.Contains(f.Families, s.Family) {
		return false
	}
	if len(f.TestTypes) > 0 && !slices.Contains(f.TestTypes, s.TestType) {
		return false
	}
	if len(f.Origins) > 0 && !slices.Contains(f.Origins, s.Provenance.Origin) {
		return false
	}
	if len(f.Redistributable) > 0 && !slices.Contains(f.Redistributable, s.Provenance.Redistributable) {
		return false
	}
	if f.MinDifficulty > 0 && s.Difficulty.Band < f.MinDifficulty {
		return false
	}
	if f.MaxDifficulty > 0 && s.Difficulty.Band > f.MaxDifficulty {
		return false
	}
	return true
}
