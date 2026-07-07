package source

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
	if len(f.Categories) > 0 && !containsCategory(f.Categories, s.Category) {
		return false
	}
	if len(f.Redistributable) > 0 && !containsRedist(f.Redistributable, s.License.Redistributable) {
		return false
	}
	if len(f.Priorities) > 0 && !containsPriority(f.Priorities, s.Priority) {
		return false
	}
	if len(f.Families) > 0 && !anyFamily(f.Families, s.Families) {
		return false
	}
	if len(f.TestTypes) > 0 && !anyTestType(f.TestTypes, s.TestTypes) {
		return false
	}
	return true
}

func containsCategory(set []Category, v Category) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}

func containsRedist(set []Redistributable, v Redistributable) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}

func containsPriority(set []Priority, v Priority) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}

func anyFamily(want, have []AbilityFamily) bool {
	for _, w := range want {
		for _, h := range have {
			if w == h {
				return true
			}
		}
	}
	return false
}

func anyTestType(want, have []TestTypeCode) bool {
	for _, w := range want {
		for _, h := range have {
			if w == h {
				return true
			}
		}
	}
	return false
}
