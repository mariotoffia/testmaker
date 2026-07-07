package source

import "slices"

// SourceFilter is a value object expressing catalogue query criteria. An empty
// filter matches everything; each populated field narrows the result set (AND
// across fields, OR within a field).
type SourceFilter struct {
	Categories      []Category
	Families        []AbilityFamily
	TestTypes       []TestTypeCode
	Redistributable []Redistributable
	Priorities      []Priority
	GeneratorsOnly  bool
}

// Matches reports whether a snapshot satisfies the filter.
func (f SourceFilter) Matches(s Snapshot) bool {
	if f.GeneratorsOnly && !s.Generator {
		return false
	}
	if len(f.Categories) > 0 && !slices.Contains(f.Categories, s.Category) {
		return false
	}
	if len(f.Redistributable) > 0 && !slices.Contains(f.Redistributable, s.License.Redistributable) {
		return false
	}
	if len(f.Priorities) > 0 && !slices.Contains(f.Priorities, s.Priority) {
		return false
	}
	if len(f.Families) > 0 && !overlaps(f.Families, s.Families) {
		return false
	}
	if len(f.TestTypes) > 0 && !overlaps(f.TestTypes, s.TestTypes) {
		return false
	}
	return true
}

// overlaps reports whether want and have share at least one element.
func overlaps[T comparable](want, have []T) bool {
	return slices.ContainsFunc(want, func(w T) bool { return slices.Contains(have, w) })
}
