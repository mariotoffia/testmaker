package source

import (
	"sort"

	"github.com/mariotoffia/testmaker/domain/shared"
)

// SourceID uniquely identifies a source in the catalog (kebab-case slug).
type SourceID string

// IPRisk estimates the risk of reusing a source's items verbatim.
type IPRisk string

const (
	IPRiskLow    IPRisk = "low"
	IPRiskMedium IPRisk = "medium"
	IPRiskHigh   IPRisk = "high"
)

// Valid reports whether the risk level is known.
func (r IPRisk) Valid() bool { return r == IPRiskLow || r == IPRiskMedium || r == IPRiskHigh }

// License is a value object describing redistribution terms.
type License struct {
	Category        LicenseCategory
	Detail          string
	Redistributable Redistributable
}

// Extraction is a value object describing how the Fetcher obtains items.
type Extraction struct {
	Method  ExtractionMethod
	Auth    string
	ItemsAs string
	Notes   string
}

// SourceSpec is the validated input to NewSource.
type SourceSpec struct {
	ID              SourceID
	Name            string
	Provider        string
	URLs            []string
	AccessClasses   []AccessClass
	Formats         []string
	License         License
	TestTypes       []TestTypeCode
	ItemCount       string
	AnswerKeys      Availability
	NormsDifficulty Availability
	Languages       []string
	Extraction      Extraction
	Generator       bool
	Priority        Priority
	IPRisk          IPRisk
	Category        Category
	Notes           string
}

// Source is the aggregate root of the source bounded context: a catalogued
// place from which test items can be fetched or generated. All state is private
// and validated; it crosses ports only as a Snapshot.
type Source struct {
	id              SourceID
	name            string
	provider        string
	urls            []string
	accessClasses   []AccessClass
	formats         []string
	license         License
	testTypes       []TestTypeCode
	families        []AbilityFamily
	itemCount       string
	answerKeys      Availability
	normsDifficulty Availability
	languages       []string
	extraction      Extraction
	generator       bool
	priority        Priority
	ipRisk          IPRisk
	category        Category
	notes           string
}

// NewSource validates a spec and returns the aggregate. Families are derived
// from the test-type codes and are not accepted from callers.
func NewSource(spec SourceSpec) (*Source, *shared.TestmakerError) {
	if spec.ID == "" {
		return nil, ErrInvalidSource.WithMessage("source id is required")
	}
	if spec.Name == "" {
		return nil, ErrInvalidSource.WithMessage("source name is required").With("id", string(spec.ID))
	}
	if len(spec.URLs) == 0 {
		return nil, ErrInvalidSource.WithMessage("at least one url is required").With("id", string(spec.ID))
	}
	if len(spec.AccessClasses) == 0 {
		return nil, ErrInvalidSource.WithMessage("at least one access class is required").With("id", string(spec.ID))
	}
	for _, a := range spec.AccessClasses {
		if err := requireValid(a.Valid(), "access_class", string(a)); err != nil {
			return nil, err.With("id", string(spec.ID))
		}
	}
	if err := requireValid(spec.License.Category.Valid(), "license.category", string(spec.License.Category)); err != nil {
		return nil, err.With("id", string(spec.ID))
	}
	if err := requireValid(spec.License.Redistributable.Valid(), "license.redistributable", string(spec.License.Redistributable)); err != nil {
		return nil, err.With("id", string(spec.ID))
	}
	if len(spec.TestTypes) == 0 {
		return nil, ErrInvalidSource.WithMessage("at least one test type is required").With("id", string(spec.ID))
	}
	for _, t := range spec.TestTypes {
		if err := requireValid(t.Valid(), "test_type", string(t)); err != nil {
			return nil, err.With("id", string(spec.ID))
		}
	}
	if err := requireValid(spec.AnswerKeys.Valid(), "answer_keys", string(spec.AnswerKeys)); err != nil {
		return nil, err.With("id", string(spec.ID))
	}
	if err := requireValid(spec.NormsDifficulty.Valid(), "norms_difficulty", string(spec.NormsDifficulty)); err != nil {
		return nil, err.With("id", string(spec.ID))
	}
	if err := requireValid(spec.Priority.Valid(), "priority", string(spec.Priority)); err != nil {
		return nil, err.With("id", string(spec.ID))
	}
	if err := requireValid(spec.IPRisk.Valid(), "ip_risk", string(spec.IPRisk)); err != nil {
		return nil, err.With("id", string(spec.ID))
	}
	if err := requireValid(spec.Category.Valid(), "category", string(spec.Category)); err != nil {
		return nil, err.With("id", string(spec.ID))
	}
	if spec.Extraction.Method != "" {
		if err := requireValid(spec.Extraction.Method.Valid(), "extraction.method", string(spec.Extraction.Method)); err != nil {
			return nil, err.With("id", string(spec.ID))
		}
	}

	return &Source{
		id:              spec.ID,
		name:            spec.Name,
		provider:        spec.Provider,
		urls:            cloneStrings(spec.URLs),
		accessClasses:   cloneAccess(spec.AccessClasses),
		formats:         cloneStrings(spec.Formats),
		license:         spec.License,
		testTypes:       cloneCodes(spec.TestTypes),
		families:        DeriveFamilies(spec.TestTypes),
		itemCount:       spec.ItemCount,
		answerKeys:      spec.AnswerKeys,
		normsDifficulty: spec.NormsDifficulty,
		languages:       cloneStrings(spec.Languages),
		extraction:      spec.Extraction,
		generator:       spec.Generator,
		priority:        spec.Priority,
		ipRisk:          spec.IPRisk,
		category:        spec.Category,
		notes:           spec.Notes,
	}, nil
}

// MustSource panics on invalid input; for tests and static fixtures only.
func MustSource(spec SourceSpec) *Source {
	s, err := NewSource(spec)
	if err != nil {
		panic(err)
	}
	return s
}

// Accessors (immutable identity, copies for slices).

func (s *Source) ID() SourceID { return s.id }

func (s *Source) Name() string { return s.name }

func (s *Source) Provider() string { return s.provider }

func (s *Source) License() License { return s.license }

func (s *Source) Category() Category { return s.category }

func (s *Source) Priority() Priority { return s.priority }

func (s *Source) IPRisk() IPRisk { return s.ipRisk }

func (s *Source) IsGenerator() bool { return s.generator }

func (s *Source) Extraction() Extraction { return s.extraction }

func (s *Source) Families() []AbilityFamily { return cloneFamilies(s.families) }

func (s *Source) TestTypes() []TestTypeCode { return cloneCodes(s.testTypes) }

func (s *Source) URLs() []string { return cloneStrings(s.urls) }

func (s *Source) AccessClasses() []AccessClass { return cloneAccess(s.accessClasses) }

// Redistributable reports the reuse gate for this source's items.
func (s *Source) Redistributable() Redistributable { return s.license.Redistributable }

// Snapshot returns the persistence/transport DTO for the aggregate.
func (s *Source) Snapshot() Snapshot {
	return Snapshot{
		ID:              s.id,
		Name:            s.name,
		Provider:        s.provider,
		URLs:            cloneStrings(s.urls),
		AccessClasses:   cloneAccess(s.accessClasses),
		Formats:         cloneStrings(s.formats),
		License:         s.license,
		TestTypes:       cloneCodes(s.testTypes),
		Families:        cloneFamilies(s.families),
		ItemCount:       s.itemCount,
		AnswerKeys:      s.answerKeys,
		NormsDifficulty: s.normsDifficulty,
		Languages:       cloneStrings(s.languages),
		Extraction:      s.extraction,
		Generator:       s.generator,
		Priority:        s.priority,
		IPRisk:          s.ipRisk,
		Category:        s.category,
		Notes:           s.notes,
	}
}

// Snapshot is the dependency-neutral DTO used to persist/transport a Source.
type Snapshot struct {
	ID              SourceID
	Name            string
	Provider        string
	URLs            []string
	AccessClasses   []AccessClass
	Formats         []string
	License         License
	TestTypes       []TestTypeCode
	Families        []AbilityFamily
	ItemCount       string
	AnswerKeys      Availability
	NormsDifficulty Availability
	Languages       []string
	Extraction      Extraction
	Generator       bool
	Priority        Priority
	IPRisk          IPRisk
	Category        Category
	Notes           string
}

// RehydrateFromSnapshot rebuilds an aggregate from a trusted snapshot without
// re-validating (the snapshot is assumed to have passed NewSource previously).
func RehydrateFromSnapshot(s Snapshot) *Source {
	return &Source{
		id: s.ID, name: s.Name, provider: s.Provider, urls: cloneStrings(s.URLs),
		accessClasses: cloneAccess(s.AccessClasses), formats: cloneStrings(s.Formats),
		license: s.License, testTypes: cloneCodes(s.TestTypes), families: cloneFamilies(s.Families),
		itemCount: s.ItemCount, answerKeys: s.AnswerKeys, normsDifficulty: s.NormsDifficulty,
		languages: cloneStrings(s.Languages), extraction: s.Extraction, generator: s.Generator,
		priority: s.Priority, ipRisk: s.IPRisk, category: s.Category, notes: s.Notes,
	}
}

// DeriveFamilies maps a set of test-type codes to the distinct, sorted set of
// ability families they belong to.
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
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneAccess(in []AccessClass) []AccessClass {
	if in == nil {
		return nil
	}
	out := make([]AccessClass, len(in))
	copy(out, in)
	return out
}

func cloneCodes(in []TestTypeCode) []TestTypeCode {
	if in == nil {
		return nil
	}
	out := make([]TestTypeCode, len(in))
	copy(out, in)
	return out
}

func cloneFamilies(in []AbilityFamily) []AbilityFamily {
	if in == nil {
		return nil
	}
	out := make([]AbilityFamily, len(in))
	copy(out, in)
	return out
}
