package shared

import "slices"

// The ability-family / A1..E2 taxonomy lives in the shared kernel so every
// bounded context that classifies items — source (where they come from), item
// (the bank) and testset (composed tests) — shares one definition and the two
// can never drift. Promoted here from domain/source with the item-bank block.

// AbilityFamily is the top-level cognitive family an item belongs to.
type AbilityFamily string

const (
	FamilyLogical   AbilityFamily = "logical"
	FamilyNumerical AbilityFamily = "numerical"
	FamilyVerbal    AbilityFamily = "verbal"
	FamilySpatial   AbilityFamily = "spatial"
	FamilySpeed     AbilityFamily = "speed"
)

// TestTypeCode is a fine-grained item-type code (A1..E2) from the CLAUDE.md
// taxonomy. The leading letter selects the AbilityFamily.
type TestTypeCode string

var familyByLetter = map[byte]AbilityFamily{
	'A': FamilyLogical, 'B': FamilyNumerical, 'C': FamilyVerbal, 'D': FamilySpatial, 'E': FamilySpeed,
}

// validTestTypes is the closed set of taxonomy codes.
var validTestTypes = map[TestTypeCode]struct{}{
	"A1": {}, "A2": {}, "A3": {}, "A4": {}, "A5": {},
	"B1": {}, "B2": {}, "B3": {}, "B4": {}, "B5": {},
	"C1": {}, "C2": {}, "C3": {}, "C4": {},
	"D1": {}, "D2": {}, "D3": {},
	"E1": {}, "E2": {},
}

// Valid reports whether the code is a known taxonomy code.
func (c TestTypeCode) Valid() bool { _, ok := validTestTypes[c]; return ok }

// Family returns the AbilityFamily implied by the code's leading letter.
func (c TestTypeCode) Family() (AbilityFamily, bool) {
	if len(c) == 0 {
		return "", false
	}
	f, ok := familyByLetter[c[0]]
	return f, ok
}

// DeriveFamilies maps a set of test-type codes to the distinct, sorted set of
// ability families they belong to. Families are always derived from codes,
// never accepted from input, so the two cannot drift.
func DeriveFamilies(codes []TestTypeCode) []AbilityFamily {
	seen := map[AbilityFamily]struct{}{}
	for _, c := range codes {
		if f, ok := c.Family(); ok {
			seen[f] = struct{}{}
		}
	}
	out := make([]AbilityFamily, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}
