package source

import "github.com/mariotoffia/testmaker/domain/shared"

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

// AccessClass describes how a source is reached by the Fetcher.
type AccessClass string

const (
	AccessDownloadableArtifact AccessClass = "downloadable-artifact"
	AccessSiteScrape           AccessClass = "site-scrape"
	AccessDatasetRepo          AccessClass = "dataset-repo"
	AccessAPI                  AccessClass = "api"
	AccessInteractiveOnly      AccessClass = "interactive-only"
	AccessGenerator            AccessClass = "generator"
)

var validAccessClasses = map[AccessClass]struct{}{
	AccessDownloadableArtifact: {}, AccessSiteScrape: {}, AccessDatasetRepo: {},
	AccessAPI: {}, AccessInteractiveOnly: {}, AccessGenerator: {},
}

// Valid reports whether the access class is known.
func (a AccessClass) Valid() bool { _, ok := validAccessClasses[a]; return ok }

// LicenseCategory is the redistribution family of a source's content.
type LicenseCategory string

const (
	LicensePublicDomain         LicenseCategory = "public-domain"
	LicenseGovPublic            LicenseCategory = "gov-public"
	LicenseOpenSource           LicenseCategory = "open-source"
	LicenseOpenCC               LicenseCategory = "open-cc"
	LicenseCommercialFreeSample LicenseCategory = "commercial-free-sample"
	LicenseCommercialPaid       LicenseCategory = "commercial-paid"
	LicenseAcademicRestricted   LicenseCategory = "academic-restricted"
	LicenseUnknown              LicenseCategory = "unknown"
)

var validLicenseCategories = map[LicenseCategory]struct{}{
	LicensePublicDomain: {}, LicenseGovPublic: {}, LicenseOpenSource: {}, LicenseOpenCC: {},
	LicenseCommercialFreeSample: {}, LicenseCommercialPaid: {}, LicenseAcademicRestricted: {}, LicenseUnknown: {},
}

// Valid reports whether the license category is known.
func (l LicenseCategory) Valid() bool { _, ok := validLicenseCategories[l]; return ok }

// Redistributable is the gate for reusing a source's items.
type Redistributable string

const (
	RedistYes         Redistributable = "yes"
	RedistConditional Redistributable = "conditional"
	RedistNo          Redistributable = "no"
)

// Valid reports whether the value is a known tri-state.
func (r Redistributable) Valid() bool {
	return r == RedistYes || r == RedistConditional || r == RedistNo
}

// Availability is a yes/no/partial tri-state (answer keys, norms, ...).
type Availability string

const (
	AvailYes     Availability = "yes"
	AvailNo      Availability = "no"
	AvailPartial Availability = "partial"
)

// Valid reports whether the value is a known tri-state.
func (a Availability) Valid() bool {
	return a == AvailYes || a == AvailNo || a == AvailPartial
}

// ExtractionMethod is the concrete mechanism the Fetcher uses.
type ExtractionMethod string

const (
	MethodDirectDownload  ExtractionMethod = "direct-download"
	MethodScrapeHTML      ExtractionMethod = "scrape-html"
	MethodHeadlessBrowser ExtractionMethod = "headless-browser"
	MethodGitClone        ExtractionMethod = "git-clone"
	MethodAPI             ExtractionMethod = "api"
	MethodGenerate        ExtractionMethod = "generate"
	MethodOrderRequired   ExtractionMethod = "order-required"
	MethodNone            ExtractionMethod = "none"
)

var validMethods = map[ExtractionMethod]struct{}{
	MethodDirectDownload: {}, MethodScrapeHTML: {}, MethodHeadlessBrowser: {}, MethodGitClone: {},
	MethodAPI: {}, MethodGenerate: {}, MethodOrderRequired: {}, MethodNone: {},
}

// Valid reports whether the extraction method is known.
func (m ExtractionMethod) Valid() bool { _, ok := validMethods[m]; return ok }

// Priority ranks a source's value for a logic-first bank.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

// Valid reports whether the priority is known.
func (p Priority) Valid() bool { return p == PriorityHigh || p == PriorityMedium || p == PriorityLow }

// Category is the catalog grouping of a source.
type Category string

const (
	CategoryOpenData          Category = "open-data"
	CategoryMLDataset         Category = "ml-dataset"
	CategoryClassicInstrument Category = "classic-instrument"
	CategoryBrandedVendor     Category = "branded-vendor"
	CategoryPrepSite          Category = "prep-site"
	CategoryMensaSociety      Category = "mensa-society"
	CategoryGovStandardized   Category = "gov-standardized"
)

var validCategories = map[Category]struct{}{
	CategoryOpenData: {}, CategoryMLDataset: {}, CategoryClassicInstrument: {}, CategoryBrandedVendor: {},
	CategoryPrepSite: {}, CategoryMensaSociety: {}, CategoryGovStandardized: {},
}

// Valid reports whether the category is known.
func (c Category) Valid() bool { _, ok := validCategories[c]; return ok }

// requireValid is a small validation helper for enum fields.
func requireValid(ok bool, field, value string) *shared.TestmakerError {
	if ok {
		return nil
	}
	return ErrInvalidSource.WithMessagef("invalid %s: %q", field, value).With("field", field)
}
