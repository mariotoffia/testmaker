package ingest

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// Ingest-service sentinels. Matched by Code via errors.Is (see shared.TestmakerError).
var (
	// ErrNoFetcher marks a source no configured fetcher supports.
	ErrNoFetcher = &shared.TestmakerError{
		Code: "ingest.no_fetcher", Class: shared.ClassUnsupported, Message: "no fetcher supports source",
	}
	// ErrNoNormalizer marks a source with no registered normalizer.
	ErrNoNormalizer = &shared.TestmakerError{
		Code: "ingest.no_normalizer", Class: shared.ClassUnsupported, Message: "no normalizer registered for source",
	}
	// ErrAllRejected marks a run where the normalizer produced specs but every
	// one failed item.NewItem validation — a normalizer/mapping regression, not
	// a routine partial skip. Surfacing it stops a broken mapping from reporting
	// a silent success.
	ErrAllRejected = &shared.TestmakerError{
		Code: "ingest.all_rejected", Class: shared.ClassInvalid, Message: "every normalized spec failed validation",
	}
)

// Normalizer maps a source's fetched RawItems into item specs — the messy,
// per-source shape-mapping. It must be a pure function (no IO): the service
// validates every returned spec through item.NewItem before it is stored, so a
// normalizer never produces an unvalidated bank item.
type Normalizer func(snap source.Snapshot, raw []ports.RawItem) ([]item.ItemSpec, error)

// Report summarizes one ingest run. The counts narrow at each stage: Fetched
// artifacts feed the Normalizer, whose specs are validated (Saved) or rejected
// (Skipped).
type Report struct {
	SourceID   source.SourceID
	Fetched    int    // raw artifacts returned by the fetcher
	Normalized int    // specs produced by the normalizer
	Saved      int    // specs that passed item.NewItem and were stored
	Skipped    int    // specs item.NewItem rejected
	Note       string // fetcher note (e.g. artifact/URL counts)
}

// Service is the ingestion use-case. It holds the configured fetchers, the item
// repository, and a per-source normalizer registry.
type Service struct {
	fetchers    []ports.Fetcher
	bank        ports.ItemRepository
	normalizers map[source.SourceID]Normalizer
}

// NewService wires the item repository and the ordered fetchers to try. The
// first fetcher whose Supports returns true handles a given source.
func NewService(bank ports.ItemRepository, fetchers ...ports.Fetcher) *Service {
	return &Service{
		fetchers:    fetchers,
		bank:        bank,
		normalizers: map[source.SourceID]Normalizer{},
	}
}

// Register binds a normalizer to a source id (composition-root wiring). A later
// registration for the same id replaces the earlier one.
func (s *Service) Register(id source.SourceID, n Normalizer) {
	s.normalizers[id] = n
}

// Ingest fetches, normalizes, validates and stores a source's items, returning
// a per-stage Report. limit caps the RawItems the fetcher returns (0 = fetcher
// default). A spec item.NewItem rejects is skipped (counted), not fatal; a
// repository write error aborts the run.
func (s *Service) Ingest(ctx context.Context, snap source.Snapshot, limit int) (Report, error) {
	rep := Report{SourceID: snap.ID}

	var fetcher ports.Fetcher
	for _, f := range s.fetchers {
		if f.Supports(snap) {
			fetcher = f
			break
		}
	}
	if fetcher == nil {
		return rep, ErrNoFetcher.WithMessagef("no fetcher supports source %q (method %q)",
			snap.ID, snap.Extraction.Method).With("source", string(snap.ID))
	}
	norm, ok := s.normalizers[snap.ID]
	if !ok {
		return rep, ErrNoNormalizer.WithMessagef("no normalizer registered for source %q", snap.ID).
			With("source", string(snap.ID))
	}

	res, err := fetcher.Fetch(ctx, ports.FetchRequest{Source: snap, Limit: limit})
	if err != nil {
		return rep, err
	}
	rep.Fetched = len(res.Items)
	rep.Note = res.Note

	specs, err := norm(snap, res.Items)
	if err != nil {
		return rep, err
	}
	rep.Normalized = len(specs)

	for _, spec := range specs {
		it, verr := item.NewItem(spec)
		if verr != nil {
			// A malformed spec is skipped, not fatal: one bad row must not sink
			// an otherwise good dataset. The count surfaces normalizer drift.
			rep.Skipped++
			continue
		}
		if err := s.bank.SaveItem(ctx, it.Snapshot()); err != nil {
			return rep, err
		}
		rep.Saved++
	}
	// Some rows may legitimately fail validation, but zero survivors from a
	// non-empty spec set means the mapping is broken — fail loudly rather than
	// report an empty success.
	if rep.Normalized > 0 && rep.Saved == 0 {
		return rep, ErrAllRejected.WithMessagef("source %q: %d spec(s) produced, all rejected", snap.ID, rep.Normalized).
			With("source", string(snap.ID))
	}
	return rep, nil
}
