// Package generatortest provides a reusable conformance suite that every
// ports.Generator implementation must pass. It asserts the universal contract —
// requested count, an effective difficulty band within the requested band,
// generated provenance, valid and keyed multiple-choice items, deterministic
// output for a fixed spec (the port's stated contract, DESIGN §7), and
// precondition handling. Properties that are inherently adapter-specific (e.g.
// different seeds producing different items) stay in the adapter's own test.
package generatortest

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// RunGeneratorTests runs the conformance suite against a fresh generator from
// newGen, exercising every taxonomy code in supported (which must be non-empty).
func RunGeneratorTests(t *testing.T, newGen func() ports.Generator, supported []shared.TestTypeCode) {
	t.Helper()

	if len(supported) == 0 {
		t.Fatal("generatortest: supported must list at least one test type")
	}

	ctx := context.Background()

	t.Run("ProducesRequestedCountOfValidKeyedItems", func(t *testing.T) {
		for _, code := range supported {
			for band := 1; band <= 5; band++ {
				spec := ports.GenerateSpec{TestType: code, Difficulty: band, Count: 4, Seed: 100}
				snaps, err := newGen().Generate(ctx, spec)
				if err != nil {
					t.Fatalf("%s band %d: generate: %v", code, band, err)
				}
				if len(snaps) != spec.Count {
					t.Fatalf("%s band %d: got %d items, want %d", code, band, len(snaps), spec.Count)
				}
				for i, s := range snaps {
					assertValidKeyedItem(t, code, band, i, s)
				}
			}
		}
	})

	t.Run("RejectsNonPositiveCount", func(t *testing.T) {
		_, err := newGen().Generate(ctx, ports.GenerateSpec{TestType: supported[0], Difficulty: 1, Count: 0, Seed: 1})
		if err == nil {
			t.Fatal("want error for count 0, got nil")
		}
	})

	t.Run("DeterministicForFixedSpec", func(t *testing.T) {
		for _, code := range supported {
			for band := 1; band <= 5; band++ {
				spec := ports.GenerateSpec{TestType: code, Difficulty: band, Count: 3, Seed: 20240607}
				// Reuse ONE generator instance for both calls, so any hidden
				// per-instance state (a mutated field, a shared RNG) that broke
				// determinism would be caught here.
				gen := newGen()
				first, err := gen.Generate(ctx, spec)
				if err != nil {
					t.Fatalf("%s band %d: first generate: %v", code, band, err)
				}
				second, err := gen.Generate(ctx, spec)
				if err != nil {
					t.Fatalf("%s band %d: second generate: %v", code, band, err)
				}
				if !reflect.DeepEqual(first, second) {
					t.Fatalf("%s band %d: same spec produced different items — generation is not deterministic", code, band)
				}
			}
		}
	})

	t.Run("RejectsNonPositiveDifficulty", func(t *testing.T) {
		_, err := newGen().Generate(ctx, ports.GenerateSpec{TestType: supported[0], Difficulty: 0, Count: 1, Seed: 1})
		if err == nil {
			t.Fatal("want error for difficulty 0, got nil")
		}
	})

	t.Run("UnsupportedTypeErrors", func(t *testing.T) {
		unsupported := firstUnsupported(supported)
		if unsupported == "" {
			return // implementation supports every taxonomy code — nothing to assert
		}
		_, err := newGen().Generate(ctx, ports.GenerateSpec{TestType: unsupported, Difficulty: 1, Count: 1, Seed: 1})
		if err == nil {
			t.Fatalf("want error for unsupported type %q, got nil", unsupported)
		}
	})
}

// assertValidKeyedItem checks the universal per-item invariants: echoed
// TestType/Difficulty, generated provenance, and a valid, correctly-keyed
// multiple-choice item (re-validated through item.NewItem from its snapshot).
func assertValidKeyedItem(t *testing.T, code shared.TestTypeCode, band, i int, s item.ItemSnapshot) {
	t.Helper()

	if s.ID == "" {
		t.Fatalf("%s band %d item %d: empty id", code, band, i)
	}
	if s.TestType != code {
		t.Fatalf("%s band %d item %d: test type = %q, want %q", code, band, i, s.TestType, code)
	}
	// Difficulty is honest: an item's effective band is a real tier the
	// generator realized, never above what was requested (rule complexity may
	// saturate below high requested bands) and always a valid band (>= 1).
	if s.Difficulty.Band < 1 || s.Difficulty.Band > band {
		t.Fatalf("%s band %d item %d: effective difficulty band = %d, want within [1, %d]",
			code, band, i, s.Difficulty.Band, band)
	}
	if s.Provenance.Origin != item.OriginGenerated {
		t.Fatalf("%s band %d item %d: origin = %q, want generated", code, band, i, s.Provenance.Origin)
	}
	if s.Provenance.SourceID == "" {
		t.Fatalf("%s band %d item %d: empty provenance source id", code, band, i)
	}
	if !s.Provenance.Redistributable.Valid() {
		t.Fatalf("%s band %d item %d: invalid redistributable %q", code, band, i, s.Provenance.Redistributable)
	}

	// A snapshot that survives item.NewItem is structurally valid and correctly
	// keyed (the key must reference an existing option / match the format).
	if _, err := item.NewItem(specFromSnapshot(s)); err != nil {
		t.Fatalf("%s band %d item %d: snapshot fails re-validation: %v", code, band, i, err)
	}

	// The generator's contract is keyed multiple-choice items; assert the format
	// rather than only checking the key when it happens to be multiple-choice, so
	// a generator that silently emitted open-numeric/TFCS would fail here.
	if s.AnswerFormat != item.FormatMultipleChoice {
		t.Fatalf("%s band %d item %d: answer format %q, want %q",
			code, band, i, s.AnswerFormat, item.FormatMultipleChoice)
	}
	assertKeyReferencesOption(t, code, band, i, s)
}

// assertKeyReferencesOption confirms the answer key points at exactly one of the
// item's options — the "keyed" half of the contract.
func assertKeyReferencesOption(t *testing.T, code shared.TestTypeCode, band, i int, s item.ItemSnapshot) {
	t.Helper()

	matches := 0
	for _, o := range s.Options {
		if o.ID == s.AnswerKey.OptionID {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("%s band %d item %d: answer key %q matches %d options, want exactly 1",
			code, band, i, s.AnswerKey.OptionID, matches)
	}
}

// specFromSnapshot rebuilds the construction spec so the suite can re-run
// item.NewItem — proving the generator only emits validatable items.
func specFromSnapshot(s item.ItemSnapshot) item.ItemSpec {
	return item.ItemSpec{
		ID:           s.ID,
		Provenance:   s.Provenance,
		TestType:     s.TestType,
		Stimulus:     s.Stimulus,
		AnswerFormat: s.AnswerFormat,
		Options:      s.Options,
		AnswerKey:    s.AnswerKey,
		Explanation:  s.Explanation,
		Difficulty:   s.Difficulty,
	}
}

// firstUnsupported returns a taxonomy code absent from supported, or "" if the
// implementation supports the whole taxonomy.
func firstUnsupported(supported []shared.TestTypeCode) shared.TestTypeCode {
	all := []shared.TestTypeCode{
		"A1", "A2", "A3", "A4", "A5",
		"B1", "B2", "B3", "B4", "B5",
		"C1", "C2", "C3", "C4",
		"D1", "D2", "D3",
		"E1", "E2",
	}
	for _, c := range all {
		if !slices.Contains(supported, c) {
			return c
		}
	}
	return ""
}
