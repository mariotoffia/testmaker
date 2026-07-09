package catalog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/app/catalog"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// fakeRepo is a minimal in-memory ports.SourceRepository for use-case tests.
type fakeRepo struct {
	items  map[source.SourceID]source.Snapshot
	putErr error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{items: map[source.SourceID]source.Snapshot{}} }

func (r *fakeRepo) Get(_ context.Context, id source.SourceID) (source.Snapshot, error) {
	s, ok := r.items[id]
	if !ok {
		return source.Snapshot{}, source.ErrUnknownSource
	}
	return s, nil
}

func (r *fakeRepo) List(_ context.Context, f source.SourceFilter) ([]source.Snapshot, error) {
	var out []source.Snapshot
	for _, s := range r.items {
		if f.Matches(s) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (r *fakeRepo) Count(_ context.Context) (int, error) { return len(r.items), nil }

func (r *fakeRepo) Put(_ context.Context, s source.Snapshot) error {
	if r.putErr != nil {
		return r.putErr
	}
	r.items[s.ID] = s
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, id source.SourceID) error {
	delete(r.items, id)
	return nil
}

type fakeLoader struct {
	snaps []source.Snapshot
	err   error
}

func (l *fakeLoader) Load(context.Context) ([]source.Snapshot, error) { return l.snaps, l.err }

var (
	_ ports.SourceRepository = (*fakeRepo)(nil)
	_ ports.CatalogLoader    = (*fakeLoader)(nil)
)

func snap(id source.SourceID, redist source.Redistributable, gen bool) source.Snapshot {
	return source.MustSource(source.SourceSpec{
		ID:              id,
		Name:            "Name " + string(id),
		URLs:            []string{"https://example.com/" + string(id)},
		AccessClasses:   []source.AccessClass{source.AccessDatasetRepo},
		License:         source.License{Category: source.LicenseOpenSource, Redistributable: redist},
		TestTypes:       []source.TestTypeCode{"A2"},
		AnswerKeys:      source.AvailYes,
		NormsDifficulty: source.AvailNo,
		Priority:        source.PriorityHigh,
		IPRisk:          source.IPRiskLow,
		Category:        source.CategoryOpenData,
		Generator:       gen,
	}).Snapshot()
}

func TestSyncUpsertsEverySource(t *testing.T) {
	repo := newFakeRepo()
	loader := &fakeLoader{snaps: []source.Snapshot{
		snap("a", source.RedistYes, false),
		snap("b", source.RedistNo, true),
	}}
	svc := catalog.NewService(repo, loader)

	n, err := svc.Sync(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("Sync = %d, %v", n, err)
	}
	if got, _ := svc.Count(context.Background()); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
}

// TestSyncPrunesRemovedSources proves Sync mirrors the catalogue authoritatively:
// a source dropped from the file is deleted on the next sync, not left behind.
func TestSyncPrunesRemovedSources(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepo()
	loader := &fakeLoader{snaps: []source.Snapshot{
		snap("a", source.RedistYes, false),
		snap("b", source.RedistYes, false),
	}}
	svc := catalog.NewService(repo, loader)
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// b is removed from the file; the re-sync must prune it.
	loader.snaps = []source.Snapshot{snap("a", source.RedistYes, false)}
	n, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if n != 1 {
		t.Fatalf("Sync reported %d, want 1", n)
	}
	got, err := repo.List(ctx, source.SourceFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("after prune repo holds %d source(s), want only [a]", len(got))
	}
}

func TestSyncPropagatesLoaderError(t *testing.T) {
	boom := errors.New("boom")
	svc := catalog.NewService(newFakeRepo(), &fakeLoader{err: boom})
	if _, err := svc.Sync(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("want loader error, got %v", err)
	}
}

func TestSyncPropagatesRepoError(t *testing.T) {
	repo := newFakeRepo()
	repo.putErr = errors.New("full")
	svc := catalog.NewService(repo, &fakeLoader{snaps: []source.Snapshot{snap("a", source.RedistYes, false)}})
	if _, err := svc.Sync(context.Background()); err == nil {
		t.Fatal("want repo error")
	}
}

func TestReusableAndConditionalSplitByLicense(t *testing.T) {
	repo := newFakeRepo()
	loader := &fakeLoader{snaps: []source.Snapshot{
		snap("yes", source.RedistYes, false),
		snap("cond", source.RedistConditional, false),
		snap("no", source.RedistNo, false),
	}}
	svc := catalog.NewService(repo, loader)
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Reusable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "yes" {
		t.Fatalf("Reusable = %v, want exactly the redistributable=yes source", got)
	}

	cond, err := svc.Conditional(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cond) != 1 || cond[0].ID != "cond" {
		t.Fatalf("Conditional = %v, want exactly the redistributable=conditional source", cond)
	}
}

func TestGeneratorsReturnsOnlyGenerators(t *testing.T) {
	repo := newFakeRepo()
	loader := &fakeLoader{snaps: []source.Snapshot{
		snap("plain", source.RedistYes, false),
		snap("gen", source.RedistYes, true),
	}}
	svc := catalog.NewService(repo, loader)
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Generators(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "gen" {
		t.Fatalf("Generators = %+v, want just 'gen'", got)
	}
}
