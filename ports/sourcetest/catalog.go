// Package sourcetest provides a reusable conformance suite that every
// ports.SourceRepository implementation must pass. Running the same suite
// against every adapter (memory, sqlite, ...) guarantees behavioural parity.
package sourcetest

import (
	"context"
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// RunSourceRepositoryTests runs the full conformance suite against a fresh,
// empty repository produced by newRepo.
func RunSourceRepositoryTests(t *testing.T, newRepo func() ports.SourceRepository) {
	t.Helper()

	ctx := context.Background()

	t.Run("PutThenGet", func(t *testing.T) {
		repo := newRepo()
		want := sampleSnapshot(t, "omib", source.CategoryOpenData, "A2")
		if err := repo.Put(ctx, want); err != nil {
			t.Fatalf("put: %v", err)
		}
		got, err := repo.Get(ctx, "omib")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.ID != want.ID || got.Name != want.Name || got.Category != want.Category {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("GetUnknownReturnsErrUnknownSource", func(t *testing.T) {
		repo := newRepo()
		_, err := repo.Get(ctx, "nope")
		if !errors.Is(err, source.ErrUnknownSource) {
			t.Fatalf("want ErrUnknownSource, got %v", err)
		}
	})

	t.Run("CountAndListEmpty", func(t *testing.T) {
		repo := newRepo()
		n, err := repo.Count(ctx)
		if err != nil || n != 0 {
			t.Fatalf("count empty = %d, %v", n, err)
		}
		all, err := repo.List(ctx, source.SourceFilter{})
		if err != nil || len(all) != 0 {
			t.Fatalf("list empty = %d, %v", len(all), err)
		}
	})

	t.Run("PutReplacesSameID", func(t *testing.T) {
		repo := newRepo()
		mustPut(t, repo, sampleSnapshot(t, "raven", source.CategoryMLDataset, "A2"))
		updated := sampleSnapshot(t, "raven", source.CategoryMLDataset, "A2")
		updated.Name = "RAVEN v2"
		mustPut(t, repo, updated)
		n, _ := repo.Count(ctx)
		if n != 1 {
			t.Fatalf("expected 1 after replace, got %d", n)
		}
		got, _ := repo.Get(ctx, "raven")
		if got.Name != "RAVEN v2" {
			t.Fatalf("replace failed: %q", got.Name)
		}
	})

	t.Run("ListFilterByCategory", func(t *testing.T) {
		repo := newRepo()
		mustPut(t, repo, sampleSnapshot(t, "omib", source.CategoryOpenData, "A2"))
		mustPut(t, repo, sampleSnapshot(t, "raven", source.CategoryMLDataset, "A2"))
		mustPut(t, repo, sampleSnapshot(t, "indiabix", source.CategoryGovStandardized, "B3"))
		got, err := repo.List(ctx, source.SourceFilter{Categories: []source.Category{source.CategoryMLDataset}})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 1 || got[0].ID != "raven" {
			t.Fatalf("filter by category failed: %+v", got)
		}
	})

	t.Run("ListFilterByFamily", func(t *testing.T) {
		repo := newRepo()
		mustPut(t, repo, sampleSnapshot(t, "omib", source.CategoryOpenData, "A2"))       // logical
		mustPut(t, repo, sampleSnapshot(t, "indiabix", source.CategoryGovStandardized, "B3")) // numerical
		got, err := repo.List(ctx, source.SourceFilter{Families: []source.AbilityFamily{source.FamilyNumerical}})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 1 || got[0].ID != "indiabix" {
			t.Fatalf("filter by family failed: %+v", got)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		repo := newRepo()
		mustPut(t, repo, sampleSnapshot(t, "omib", source.CategoryOpenData, "A2"))
		if err := repo.Delete(ctx, "omib"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := repo.Get(ctx, "omib"); !errors.Is(err, source.ErrUnknownSource) {
			t.Fatalf("expected gone, got %v", err)
		}
		// deleting an absent id is not an error
		if err := repo.Delete(ctx, "omib"); err != nil {
			t.Fatalf("delete absent: %v", err)
		}
	})

	t.Run("SnapshotIsolation", func(t *testing.T) {
		repo := newRepo()
		snap := sampleSnapshot(t, "omib", source.CategoryOpenData, "A2")
		mustPut(t, repo, snap)
		// mutating the returned snapshot must not affect stored state
		got, _ := repo.Get(ctx, "omib")
		if len(got.URLs) > 0 {
			got.URLs[0] = "mutated"
		}
		again, _ := repo.Get(ctx, "omib")
		if len(again.URLs) > 0 && again.URLs[0] == "mutated" {
			t.Fatalf("repository leaked internal slice state")
		}
	})
}

func mustPut(t *testing.T, repo ports.SourceRepository, snap source.Snapshot) {
	t.Helper()
	if err := repo.Put(context.Background(), snap); err != nil {
		t.Fatalf("put %s: %v", snap.ID, err)
	}
}

func sampleSnapshot(t *testing.T, id source.SourceID, cat source.Category, tt ...source.TestTypeCode) source.Snapshot {
	t.Helper()
	if len(tt) == 0 {
		tt = []source.TestTypeCode{"A2"}
	}
	s, err := source.NewSource(source.SourceSpec{
		ID:              id,
		Name:            "Name " + string(id),
		Provider:        "provider",
		URLs:            []string{"https://example.com/" + string(id)},
		AccessClasses:   []source.AccessClass{source.AccessDatasetRepo},
		Formats:         []string{"json"},
		License:         source.License{Category: source.LicenseOpenSource, Redistributable: source.RedistYes},
		TestTypes:       tt,
		AnswerKeys:      source.AvailYes,
		NormsDifficulty: source.AvailNo,
		Languages:       []string{"en"},
		Priority:        source.PriorityHigh,
		IPRisk:          source.IPRiskLow,
		Category:        cat,
	})
	if err != nil {
		t.Fatalf("build sample %s: %v", id, err)
	}
	return s.Snapshot()
}
