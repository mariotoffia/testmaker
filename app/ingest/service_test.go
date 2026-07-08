package ingest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// fakeFetcher is an in-process ports.Fetcher: it reports a fixed support answer
// and returns a canned result/error.
type fakeFetcher struct {
	supports bool
	result   ports.FetchResult
	err      error
	calls    int
}

func (f *fakeFetcher) Supports(source.Snapshot) bool { return f.supports }

func (f *fakeFetcher) Fetch(context.Context, ports.FetchRequest) (ports.FetchResult, error) {
	f.calls++
	return f.result, f.err
}

// fakeBank is an in-process ports.ItemRepository capturing saved snapshots.
type fakeBank struct {
	saved   []item.ItemSnapshot
	saveErr error
}

func (b *fakeBank) SaveItem(_ context.Context, snap item.ItemSnapshot) error {
	if b.saveErr != nil {
		return b.saveErr
	}
	b.saved = append(b.saved, snap)
	return nil
}

func (b *fakeBank) GetItem(context.Context, item.ItemID) (item.ItemSnapshot, error) {
	return item.ItemSnapshot{}, item.ErrUnknownItem
}

func (b *fakeBank) ListItems(context.Context, item.ItemFilter) ([]item.ItemSnapshot, error) {
	return b.saved, nil
}

var (
	_ ports.Fetcher        = (*fakeFetcher)(nil)
	_ ports.ItemRepository = (*fakeBank)(nil)
)

func testSnap() source.Snapshot {
	return source.Snapshot{ID: "src-1", Extraction: source.Extraction{Method: source.MethodDirectDownload}}
}

// validSpec builds a minimal valid multiple-choice item spec.
func validSpec(id item.ItemID) item.ItemSpec {
	return item.ItemSpec{
		ID:           id,
		Provenance:   item.Provenance{SourceID: "src-1", Origin: item.OriginFetched, Redistributable: shared.RedistYes},
		TestType:     "C3",
		Stimulus:     []item.StimulusPart{{Text: "stem"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options:      []item.Option{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"}},
		AnswerKey:    item.AnswerKey{OptionID: "a"},
		Difficulty:   item.Difficulty{Band: 1},
	}
}

func TestIngestHappyPathWithSkips(t *testing.T) {
	fetch := &fakeFetcher{
		supports: true,
		result: ports.FetchResult{
			SourceID: "src-1",
			Items:    []ports.RawItem{{ExternalID: "a"}, {ExternalID: "b"}},
			Note:     "fetched 2",
		},
	}
	bank := &fakeBank{}
	svc := ingest.NewService(bank, fetch)

	// Normalizer returns 3 specs: two valid, one invalid (no options).
	svc.Register("src-1", func(source.Snapshot, []ports.RawItem) ([]item.ItemSpec, error) {
		bad := validSpec("bad")
		bad.Options = nil
		return []item.ItemSpec{validSpec("ok-1"), bad, validSpec("ok-2")}, nil
	})

	rep, err := svc.Ingest(context.Background(), testSnap(), 0)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if rep.Fetched != 2 || rep.Normalized != 3 || rep.Saved != 2 || rep.Skipped != 1 {
		t.Errorf("report = %+v, want fetched=2 normalized=3 saved=2 skipped=1", rep)
	}
	if rep.Note != "fetched 2" {
		t.Errorf("note = %q", rep.Note)
	}
	if len(bank.saved) != 2 {
		t.Errorf("bank stored %d items, want 2", len(bank.saved))
	}
}

func TestIngestNoFetcher(t *testing.T) {
	fetch := &fakeFetcher{supports: false}
	svc := ingest.NewService(&fakeBank{}, fetch)
	svc.Register("src-1", func(source.Snapshot, []ports.RawItem) ([]item.ItemSpec, error) { return nil, nil })

	_, err := svc.Ingest(context.Background(), testSnap(), 0)
	if !errors.Is(err, ingest.ErrNoFetcher) {
		t.Fatalf("err = %v, want ErrNoFetcher", err)
	}
	if fetch.calls != 0 {
		t.Errorf("fetcher should not be called when unsupported")
	}
}

func TestIngestNoNormalizer(t *testing.T) {
	fetch := &fakeFetcher{supports: true}
	svc := ingest.NewService(&fakeBank{}, fetch)

	_, err := svc.Ingest(context.Background(), testSnap(), 0)
	if !errors.Is(err, ingest.ErrNoNormalizer) {
		t.Fatalf("err = %v, want ErrNoNormalizer", err)
	}
	// Normalizer is missing, so no fetch should happen.
	if fetch.calls != 0 {
		t.Errorf("fetcher should not be called when no normalizer is registered")
	}
}

func TestIngestFetchError(t *testing.T) {
	sentinel := errors.New("boom")
	fetch := &fakeFetcher{supports: true, err: sentinel}
	svc := ingest.NewService(&fakeBank{}, fetch)
	svc.Register("src-1", func(source.Snapshot, []ports.RawItem) ([]item.ItemSpec, error) { return nil, nil })

	_, err := svc.Ingest(context.Background(), testSnap(), 0)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want fetch error propagated", err)
	}
}

func TestIngestSaveError(t *testing.T) {
	sentinel := errors.New("disk full")
	fetch := &fakeFetcher{supports: true, result: ports.FetchResult{SourceID: "src-1"}}
	bank := &fakeBank{saveErr: sentinel}
	svc := ingest.NewService(bank, fetch)
	svc.Register("src-1", func(source.Snapshot, []ports.RawItem) ([]item.ItemSpec, error) {
		return []item.ItemSpec{validSpec("ok-1")}, nil
	})

	_, err := svc.Ingest(context.Background(), testSnap(), 0)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want save error propagated", err)
	}
}

func TestIngestAllRejected(t *testing.T) {
	fetch := &fakeFetcher{
		supports: true,
		result:   ports.FetchResult{SourceID: "src-1", Items: []ports.RawItem{{ExternalID: "a"}}},
	}
	bank := &fakeBank{}
	svc := ingest.NewService(bank, fetch)
	// Every produced spec is invalid (no options) — a mapping regression.
	svc.Register("src-1", func(source.Snapshot, []ports.RawItem) ([]item.ItemSpec, error) {
		bad := validSpec("bad")
		bad.Options = nil
		return []item.ItemSpec{bad, bad}, nil
	})

	rep, err := svc.Ingest(context.Background(), testSnap(), 0)
	if !errors.Is(err, ingest.ErrAllRejected) {
		t.Fatalf("err = %v, want ErrAllRejected", err)
	}
	if rep.Normalized != 2 || rep.Saved != 0 || rep.Skipped != 2 {
		t.Errorf("report = %+v, want normalized=2 saved=0 skipped=2", rep)
	}
	if len(bank.saved) != 0 {
		t.Errorf("bank stored %d items, want 0", len(bank.saved))
	}
}

func TestIngestFirstSupportingFetcherWins(t *testing.T) {
	no := &fakeFetcher{supports: false}
	yes := &fakeFetcher{supports: true, result: ports.FetchResult{SourceID: "src-1"}}
	svc := ingest.NewService(&fakeBank{}, no, yes)
	svc.Register("src-1", func(source.Snapshot, []ports.RawItem) ([]item.ItemSpec, error) { return nil, nil })

	if _, err := svc.Ingest(context.Background(), testSnap(), 0); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if no.calls != 0 || yes.calls != 1 {
		t.Errorf("routing wrong: no.calls=%d yes.calls=%d", no.calls, yes.calls)
	}
}
