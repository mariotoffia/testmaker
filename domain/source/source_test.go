package source_test

import (
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/domain/source"
)

func validSpec() source.SourceSpec {
	return source.SourceSpec{
		ID:              "omib",
		Name:            "Open Matrices Item Bank",
		URLs:            []string{"https://osf.io/4km79/"},
		AccessClasses:   []source.AccessClass{source.AccessDatasetRepo},
		License:         source.License{Category: source.LicenseOpenSource, Redistributable: source.RedistConditional},
		TestTypes:       []source.TestTypeCode{"A2"},
		AnswerKeys:      source.AvailYes,
		NormsDifficulty: source.AvailYes,
		Priority:        source.PriorityHigh,
		IPRisk:          source.IPRiskLow,
		Category:        source.CategoryOpenData,
	}
}

func TestNewSourceValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*source.SourceSpec)
		ok     bool
	}{
		{"valid", func(*source.SourceSpec) {}, true},
		{"missing id", func(s *source.SourceSpec) { s.ID = "" }, false},
		{"missing name", func(s *source.SourceSpec) { s.Name = "" }, false},
		{"no urls", func(s *source.SourceSpec) { s.URLs = nil }, false},
		{"no access classes", func(s *source.SourceSpec) { s.AccessClasses = nil }, false},
		{"bad access class", func(s *source.SourceSpec) { s.AccessClasses = []source.AccessClass{"ftp"} }, false},
		{"bad license category", func(s *source.SourceSpec) { s.License.Category = "nope" }, false},
		{"bad redistributable", func(s *source.SourceSpec) { s.License.Redistributable = "maybe" }, false},
		{"no test types", func(s *source.SourceSpec) { s.TestTypes = nil }, false},
		{"bad test type", func(s *source.SourceSpec) { s.TestTypes = []source.TestTypeCode{"Z9"} }, false},
		{"bad answer keys", func(s *source.SourceSpec) { s.AnswerKeys = "" }, false},
		{"bad norms", func(s *source.SourceSpec) { s.NormsDifficulty = "" }, false},
		{"bad priority", func(s *source.SourceSpec) { s.Priority = "urgent" }, false},
		{"bad ip risk", func(s *source.SourceSpec) { s.IPRisk = "" }, false},
		{"bad category", func(s *source.SourceSpec) { s.Category = "misc" }, false},
		{"bad extraction method", func(s *source.SourceSpec) { s.Extraction.Method = "carrier-pigeon" }, false},
		{"empty extraction method ok", func(s *source.SourceSpec) { s.Extraction.Method = "" }, true},
		{"bad items_as", func(s *source.SourceSpec) { s.Extraction.ItemsAs = "holograms" }, false},
		{"valid items_as ok", func(s *source.SourceSpec) { s.Extraction.ItemsAs = source.ItemsGrids }, true},
		{"empty items_as ok", func(s *source.SourceSpec) { s.Extraction.ItemsAs = "" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSpec()
			tc.mutate(&spec)
			s, err := source.NewSource(spec)
			if tc.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				if !errors.Is(err, source.ErrInvalidSource) {
					t.Fatalf("want ErrInvalidSource, got %v", err)
				}
			}
			if tc.ok && s == nil {
				t.Fatal("want aggregate, got nil")
			}
		})
	}
}

func TestEmptyExtractionMethodNormalizesToNone(t *testing.T) {
	spec := validSpec()
	spec.Extraction.Method = ""
	s, err := source.NewSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Extraction().Method; got != source.MethodNone {
		t.Fatalf("Method = %q, want normalized %q", got, source.MethodNone)
	}
}

func TestFamiliesAreDerivedNotAccepted(t *testing.T) {
	spec := validSpec()
	spec.TestTypes = []source.TestTypeCode{"A2", "A1", "B3", "D1"}
	s, err := source.NewSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	want := []source.AbilityFamily{source.FamilyLogical, source.FamilyNumerical, source.FamilySpatial}
	got := s.Families()
	if len(got) != len(want) {
		t.Fatalf("families = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("families = %v, want %v (sorted, distinct)", got, want)
		}
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	s, err := source.NewSource(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	back := source.RehydrateFromSnapshot(snap).Snapshot()
	if back.ID != snap.ID || back.Name != snap.Name ||
		back.License != snap.License || len(back.Families) != len(snap.Families) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", back, snap)
	}
	// snapshots must not share slices with the aggregate
	snap.URLs[0] = "mutated"
	if s.URLs()[0] == "mutated" {
		t.Fatal("Snapshot leaked internal slice")
	}
}

func TestSourceFilterMatches(t *testing.T) {
	gen := validSpec()
	gen.ID, gen.Generator = "raven", true
	gen.Category = source.CategoryMLDataset
	gen.License.Redistributable = source.RedistYes
	genSnap := source.MustSource(gen).Snapshot()
	omibSnap := source.MustSource(validSpec()).Snapshot()

	cases := []struct {
		name   string
		filter source.SourceFilter
		snap   source.Snapshot
		want   bool
	}{
		{"empty matches", source.SourceFilter{}, omibSnap, true},
		{"generators only excludes", source.SourceFilter{GeneratorsOnly: true}, omibSnap, false},
		{"generators only includes", source.SourceFilter{GeneratorsOnly: true}, genSnap, true},
		{"category hit", source.SourceFilter{Categories: []source.Category{source.CategoryOpenData}}, omibSnap, true},
		{"category miss", source.SourceFilter{Categories: []source.Category{source.CategoryPrepSite}}, omibSnap, false},
		{"redistributable hit", source.SourceFilter{Redistributable: []source.Redistributable{source.RedistYes, source.RedistConditional}}, omibSnap, true},
		{"redistributable miss", source.SourceFilter{Redistributable: []source.Redistributable{source.RedistNo}}, omibSnap, false},
		{"family hit", source.SourceFilter{Families: []source.AbilityFamily{source.FamilyLogical}}, omibSnap, true},
		{"family miss", source.SourceFilter{Families: []source.AbilityFamily{source.FamilyVerbal}}, omibSnap, false},
		{"test type hit", source.SourceFilter{TestTypes: []source.TestTypeCode{"A2"}}, omibSnap, true},
		{"test type miss", source.SourceFilter{TestTypes: []source.TestTypeCode{"C1"}}, omibSnap, false},
		{"priority hit", source.SourceFilter{Priorities: []source.Priority{source.PriorityHigh}}, omibSnap, true},
		{"AND across fields", source.SourceFilter{
			Categories: []source.Category{source.CategoryOpenData},
			Families:   []source.AbilityFamily{source.FamilyVerbal},
		}, omibSnap, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.filter.Matches(tc.snap); got != tc.want {
				t.Fatalf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTestTypeCodeFamily(t *testing.T) {
	for code, want := range map[source.TestTypeCode]source.AbilityFamily{
		"A1": source.FamilyLogical, "B5": source.FamilyNumerical, "C4": source.FamilyVerbal,
		"D3": source.FamilySpatial, "E2": source.FamilySpeed,
	} {
		f, ok := code.Family()
		if !ok || f != want {
			t.Fatalf("%s Family() = %v/%v, want %v", code, f, ok, want)
		}
	}
	if _, ok := source.TestTypeCode("").Family(); ok {
		t.Fatal("empty code must have no family")
	}
	if source.TestTypeCode("A9").Valid() {
		t.Fatal("A9 is outside the closed set")
	}
}
