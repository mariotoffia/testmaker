package source

import "github.com/mariotoffia/testmaker/domain/shared"

// The ability-family / A1..E2 taxonomy and the Redistributable reuse gate now
// live in the shared kernel (domain/shared) so source, item and testset share
// one definition. These aliases and re-exports keep the source package's public
// vocabulary — source.AbilityFamily, source.FamilyLogical, source.TestTypeCode,
// source.RedistYes, … — stable for existing callers.

// AbilityFamily is the top-level cognitive family an item belongs to.
type AbilityFamily = shared.AbilityFamily

const (
	FamilyLogical   = shared.FamilyLogical
	FamilyNumerical = shared.FamilyNumerical
	FamilyVerbal    = shared.FamilyVerbal
	FamilySpatial   = shared.FamilySpatial
	FamilySpeed     = shared.FamilySpeed
)

// TestTypeCode is a fine-grained item-type code (A1..E2); its leading letter
// selects the AbilityFamily. Valid() and Family() live on the shared type.
type TestTypeCode = shared.TestTypeCode

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

// Redistributable is the reuse gate for a source's items. Defined in the shared
// kernel because it also travels onto every item derived from the source.
type Redistributable = shared.Redistributable

const (
	RedistYes         = shared.RedistYes
	RedistConditional = shared.RedistConditional
	RedistNo          = shared.RedistNo
)

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

// ItemsAs is the shape items arrive in from a source; it routes fetched
// material to the right normalization path (e.g. image items vs text items).
type ItemsAs string

const (
	ItemsGrids       ItemsAs = "grids"
	ItemsImages      ItemsAs = "images"
	ItemsInteractive ItemsAs = "interactive"
	ItemsMixed       ItemsAs = "mixed"
	ItemsText        ItemsAs = "text"
	ItemsVectors     ItemsAs = "vectors"
)

var validItemsAs = map[ItemsAs]struct{}{
	ItemsGrids: {}, ItemsImages: {}, ItemsInteractive: {}, ItemsMixed: {},
	ItemsText: {}, ItemsVectors: {},
}

// Valid reports whether the item shape is known.
func (i ItemsAs) Valid() bool { _, ok := validItemsAs[i]; return ok }

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
