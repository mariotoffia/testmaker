package sqlitetestdb

import (
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
)

// TestItemListQueryDeDuplicatesInValues proves the IN-clause builder collapses
// duplicate filter values to a single placeholder (and a single bind) each, so
// the bind count is bounded by the distinct vocabulary no matter how repetitive a
// caller's filter is. De-duplication is safe because IN is a set test — the same
// reason item.ItemFilter.Matches uses slices.Contains — so the result set is
// unchanged; only the bind count shrinks. This is the runnable check for the
// bind-count ceiling.
func TestItemListQueryDeDuplicatesInValues(t *testing.T) {
	q, args := itemListQuery(item.ItemFilter{
		Families: []shared.AbilityFamily{
			shared.FamilyLogical,
			shared.FamilyLogical,
			shared.FamilyNumerical,
			shared.FamilyLogical,
		},
	})

	if want := "family IN (?,?)"; !strings.Contains(q, want) {
		t.Fatalf("query does not contain de-duped clause %q; query=%q", want, q)
	}
	if len(args) != 2 {
		t.Fatalf("bind count = %d, want 2; args=%v", len(args), args)
	}
	want := map[string]bool{
		string(shared.FamilyLogical):   true,
		string(shared.FamilyNumerical): true,
	}
	for _, a := range args {
		s, ok := a.(string)
		if !ok || !want[s] {
			t.Fatalf("unexpected bind %v (%T); want one of %v", a, a, want)
		}
		delete(want, s) // each distinct value appears exactly once
	}
	if len(want) != 0 {
		t.Fatalf("missing binds for %v", want)
	}
}
