package authoring

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// ErrNoGenerator marks a Generate call on a Service wired without a generator
// (author-only). Matched by Code via errors.Is.
var ErrNoGenerator = &shared.TestmakerError{
	Code: "authoring.no_generator", Class: shared.ClassUnsupported, Message: "no generator configured",
}

// Report summarizes one generate-and-store run: how many items the generator
// produced and how many reached the bank. They differ only if a store write
// aborts the run partway.
type Report struct {
	TestType  shared.TestTypeCode
	Generated int
	Saved     int
}

// Service is the authoring use-case. It wires a procedural generator and the
// item bank; the generator is optional (a nil generator still allows the manual
// Author path).
type Service struct {
	gen  ports.Generator
	bank ports.ItemRepository
}

// NewService wires the generator and item repository.
func NewService(gen ports.Generator, bank ports.ItemRepository) *Service {
	return &Service{gen: gen, bank: bank}
}

// Generate produces a batch through the generator and stores every item,
// returning a per-run Report. The generator already guarantees each item is
// valid and keyed, so a write failure (not a validation failure) is the only
// thing that stops the run — and it aborts, surfacing the partial Saved count.
func (s *Service) Generate(ctx context.Context, spec ports.GenerateSpec) (Report, error) {
	rep := Report{TestType: spec.TestType}
	if s.gen == nil {
		return rep, ErrNoGenerator
	}

	snaps, err := s.gen.Generate(ctx, spec)
	if err != nil {
		return rep, err
	}
	rep.Generated = len(snaps)

	for _, snap := range snaps {
		if err := s.bank.SaveItem(ctx, snap); err != nil {
			return rep, err
		}
		rep.Saved++
	}
	return rep, nil
}

// Author is the manual authoring path: it validates a hand-written spec through
// item.NewItem (the invariant gate) and stores the result, returning the item's
// id. An invalid spec is item.ErrInvalidItem and nothing is stored.
func (s *Service) Author(ctx context.Context, spec item.ItemSpec) (item.ItemID, error) {
	it, verr := item.NewItem(spec)
	if verr != nil {
		return "", verr
	}
	if err := s.bank.SaveItem(ctx, it.Snapshot()); err != nil {
		return "", err
	}
	return it.ID(), nil
}
