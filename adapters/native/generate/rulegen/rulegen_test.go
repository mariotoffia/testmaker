package rulegen_test

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/generate/rulegen"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/ports/generatortest"
)

var _ ports.Generator = (*rulegen.Generator)(nil)

// supportedCodes are the figural taxonomy codes rulegen serves.
func supportedCodes() []shared.TestTypeCode {
	return []shared.TestTypeCode{"A1", "A2", "A3", "A4"}
}

func TestGeneratorConformance(t *testing.T) {
	generatortest.RunGeneratorTests(t, func() ports.Generator { return rulegen.New() }, supportedCodes())
}

// TestDifferentSeedProducesDifferentItems is a smoke check that the seed
// actually reaches the figural content (not just the id). It is NOT a universal
// injectivity claim — the finite rule space permits content collisions for some
// seed pairs (see GenerateSpec.Seed); these particular seeds diverge.
func TestDifferentSeedProducesDifferentItems(t *testing.T) {
	ctx := context.Background()
	for _, code := range supportedCodes() {
		spec := ports.GenerateSpec{TestType: code, Difficulty: 3, Count: 3, Seed: 1}
		a, err := rulegen.New().Generate(ctx, spec)
		if err != nil {
			t.Fatalf("%s: generate a: %v", code, err)
		}
		spec.Seed = 2
		b, err := rulegen.New().Generate(ctx, spec)
		if err != nil {
			t.Fatalf("%s: generate b: %v", code, err)
		}
		if reflect.DeepEqual(snapshotContent(a), snapshotContent(b)) {
			t.Fatalf("%s: different seeds produced identical figural content", code)
		}
		if a[0].ID == b[0].ID {
			t.Fatalf("%s: ids did not incorporate the seed: %q == %q", code, a[0].ID, b[0].ID)
		}
	}
}

// snapshotContent extracts the rendered figural content (ignoring ids, which
// embed the seed) so seed tests prove the CONTENT diverges, not just the ids.
func snapshotContent(snaps []item.ItemSnapshot) []string {
	var out []string
	for _, s := range snaps {
		for _, p := range s.Stimulus {
			out = append(out, p.Text, p.MediaRef)
		}
		for _, o := range s.Options {
			out = append(out, o.MediaRef)
		}
	}
	return out
}

func TestUnsupportedTypeReturnsSentinel(t *testing.T) {
	// Numerical/verbal/spatial/speed codes are out of the figural engine's scope.
	for _, code := range []shared.TestTypeCode{"A5", "B1", "C2", "D1", "E1", "ZZ"} {
		_, err := rulegen.New().Generate(context.Background(),
			ports.GenerateSpec{TestType: code, Difficulty: 1, Count: 1, Seed: 1})
		if !errors.Is(err, rulegen.ErrUnsupportedType) {
			t.Fatalf("%s: want ErrUnsupportedType, got %v", code, err)
		}
	}
}

func TestInvalidSpecReturnsSentinel(t *testing.T) {
	ctx := context.Background()
	t.Run("ZeroCount", func(t *testing.T) {
		_, err := rulegen.New().Generate(ctx, ports.GenerateSpec{TestType: "A1", Difficulty: 1, Count: 0, Seed: 1})
		if !errors.Is(err, rulegen.ErrInvalidSpec) {
			t.Fatalf("want ErrInvalidSpec, got %v", err)
		}
	})
	t.Run("ZeroDifficulty", func(t *testing.T) {
		_, err := rulegen.New().Generate(ctx, ports.GenerateSpec{TestType: "A1", Difficulty: 0, Count: 1, Seed: 1})
		if !errors.Is(err, rulegen.ErrInvalidSpec) {
			t.Fatalf("want ErrInvalidSpec, got %v", err)
		}
	})
	t.Run("CountAboveMax", func(t *testing.T) {
		// An absurd count must be rejected, not panic the slice preallocation.
		_, err := rulegen.New().Generate(ctx, ports.GenerateSpec{TestType: "A1", Difficulty: 1, Count: 1 << 40, Seed: 1})
		if !errors.Is(err, rulegen.ErrInvalidSpec) {
			t.Fatalf("want ErrInvalidSpec, got %v", err)
		}
	})
}

func TestCancelledContextStopsGeneration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := rulegen.New().Generate(ctx, ports.GenerateSpec{TestType: "A2", Difficulty: 2, Count: 3, Seed: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if !errors.Is(err, rulegen.ErrGenerate) {
		t.Fatalf("want ErrGenerate wrapper, got %v", err)
	}
}

// TestOddOneOutOptionsAreAllDistinct proves an A4 item cannot be solved by
// "pick the only non-duplicate image": all five option figures render
// distinctly, so the solver must reason about the shared property. (That the
// keyed option is the genuinely odd figure is proven structurally in
// engines_internal_test.go.)
func TestOddOneOutOptionsAreAllDistinct(t *testing.T) {
	ctx := context.Background()
	for band := 1; band <= 5; band++ {
		snaps, err := rulegen.New().Generate(ctx, ports.GenerateSpec{TestType: "A4", Difficulty: band, Count: 6, Seed: 7})
		if err != nil {
			t.Fatalf("band %d: generate: %v", band, err)
		}
		for _, s := range snaps {
			if len(s.Options) != 5 {
				t.Fatalf("band %d item %s: want 5 options, got %d", band, s.ID, len(s.Options))
			}
			freq := map[string]int{}
			var keyMatches int
			for _, o := range s.Options {
				freq[o.MediaRef]++
				if o.ID == s.AnswerKey.OptionID {
					keyMatches++
				}
			}
			if len(freq) != 5 {
				t.Fatalf("band %d item %s: want 5 distinct option images (so it cannot be solved by picking the unique one), got %d",
					band, s.ID, len(freq))
			}
			if keyMatches != 1 {
				t.Fatalf("band %d item %s: answer key matches %d options, want exactly 1", band, s.ID, keyMatches)
			}
		}
	}
}

// TestSeriesAndMatrixOptionsAreVisuallyDistinct proves distractors never
// duplicate each other or the answer.
func TestSeriesAndMatrixOptionsAreVisuallyDistinct(t *testing.T) {
	ctx := context.Background()
	for _, code := range []shared.TestTypeCode{"A1", "A2", "A3"} {
		for band := 1; band <= 5; band++ {
			snaps, err := rulegen.New().Generate(ctx, ports.GenerateSpec{TestType: code, Difficulty: band, Count: 6, Seed: 9})
			if err != nil {
				t.Fatalf("%s band %d: generate: %v", code, band, err)
			}
			for _, s := range snaps {
				seen := map[string]bool{}
				for _, o := range s.Options {
					if seen[o.MediaRef] {
						t.Fatalf("%s band %d item %s: duplicate option figure", code, band, s.ID)
					}
					seen[o.MediaRef] = true
				}
			}
		}
	}
}

// TestOptionsRenderAsSVG decodes an option's data-URI and checks it is a
// well-formed SVG document (shape, not pixel content).
func TestOptionsRenderAsSVG(t *testing.T) {
	snaps, err := rulegen.New().Generate(context.Background(),
		ports.GenerateSpec{TestType: "A2", Difficulty: 3, Count: 1, Seed: 3})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	const prefix = "data:image/svg+xml;base64,"
	for _, o := range snaps[0].Options {
		if !strings.HasPrefix(o.MediaRef, prefix) {
			t.Fatalf("option %s: media ref is not an svg data-uri: %.40q", o.ID, o.MediaRef)
		}
		raw, derr := base64.StdEncoding.DecodeString(strings.TrimPrefix(o.MediaRef, prefix))
		if derr != nil {
			t.Fatalf("option %s: base64 decode: %v", o.ID, derr)
		}
		svg := string(raw)
		if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "</svg>") {
			t.Fatalf("option %s: decoded payload is not an svg document: %.60q", o.ID, svg)
		}
		// The root must declare intrinsic width/height, not just a viewBox: an
		// <img> referencing a size-less SVG collapses to 0x0 and renders blank.
		if !strings.Contains(svg, "width=") || !strings.Contains(svg, "height=") {
			t.Fatalf("option %s: svg root lacks intrinsic width/height: %.90q", o.ID, svg)
		}
	}
	// The matrix stimulus carries the grid media alongside the prompt text.
	if len(snaps[0].Stimulus) < 2 || snaps[0].Stimulus[1].MediaKind != item.MediaGrid {
		t.Fatalf("matrix stimulus missing grid media: %+v", snaps[0].Stimulus)
	}
}
