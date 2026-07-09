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

	// Precedence matches the LLM path and the pre-refactor behaviour: a source
	// with neither a fetcher nor a normalizer reports ErrNoFetcher first. Select
	// (but do not yet call) the fetcher so the normalizer check can still win
	// before any network work happens.
	idx, err := s.fetcherIndex(snap)
	if err != nil {
		return rep, err
	}

	norm, ok := s.normalizers[snap.ID]
	if !ok {
		return rep, ErrNoNormalizer.WithMessagef("no normalizer registered for source %q", snap.ID).
			With("source", string(snap.ID))
	}

	res, err := s.fetchers[idx].Fetch(ctx, ports.FetchRequest{Source: snap, Limit: limit})
	if err != nil {
		return rep, err
	}
	rep.Fetched = len(res.Items)
	rep.Note = res.Note

	specs, err := norm(snap, res.Items)
	if err != nil {
		return rep, err
	}
	if err := s.saveSpecs(ctx, specs, &rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// fetcherIndex returns the position of the first configured fetcher that
// supports snap, without calling it. No supporting fetcher is ErrNoFetcher.
func (s *Service) fetcherIndex(snap source.Snapshot) (int, error) {
	for i, f := range s.fetchers {
		if f.Supports(snap) {
			return i, nil
		}
	}
	return -1, ErrNoFetcher.WithMessagef("no fetcher supports source %q (method %q)",
		snap.ID, snap.Extraction.Method).With("source", string(snap.ID))
}

// fetchFor selects the first configured fetcher that supports snap and pulls up
// to limit raw items from it. No supporting fetcher is ErrNoFetcher.
func (s *Service) fetchFor(ctx context.Context, snap source.Snapshot, limit int) (ports.FetchResult, error) {
	idx, err := s.fetcherIndex(snap)
	if err != nil {
		return ports.FetchResult{}, err
	}
	return s.fetchers[idx].Fetch(ctx, ports.FetchRequest{Source: snap, Limit: limit})
}

// saveSpecs validates every spec through item.NewItem and stores the survivors,
// updating rep's Normalized/Saved/Skipped counts. A spec NewItem rejects is
// skipped (counted), not fatal; a repository write error aborts. A non-empty
// spec set with zero survivors is ErrAllRejected — a broken mapping, not an
// empty-but-fine run.
func (s *Service) saveSpecs(ctx context.Context, specs []item.ItemSpec, rep *Report) error {
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
			return err
		}
		rep.Saved++
	}
	// Some rows may legitimately fail validation, but zero survivors from a
	// non-empty spec set means the mapping is broken — fail loudly rather than
	// report an empty success.
	if rep.Normalized > 0 && rep.Saved == 0 {
		return ErrAllRejected.WithMessagef("source %q: %d spec(s) produced, all rejected", rep.SourceID, rep.Normalized).
			With("source", string(rep.SourceID))
	}
	return nil
}
