package prompt_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/domain/prompt"
)

func validSpec() prompt.Spec {
	return prompt.Spec{
		ID:       "extract-items",
		Version:  1,
		Purpose:  prompt.PurposeExtraction,
		Template: "Extract items from {{.format}} into JSON.",
		Params:   []string{"format"},
	}
}

func TestNewPromptValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*prompt.Spec)
		ok     bool
	}{
		{"valid", func(*prompt.Spec) {}, true},
		{"missing id", func(s *prompt.Spec) { s.ID = "" }, false},
		{"zero version", func(s *prompt.Spec) { s.Version = 0 }, false},
		{"negative version", func(s *prompt.Spec) { s.Version = -1 }, false},
		{"unknown purpose", func(s *prompt.Spec) { s.Purpose = "chitchat" }, false},
		{"empty template", func(s *prompt.Spec) { s.Template = "  " }, false},
		{"unparsable template", func(s *prompt.Spec) { s.Template = "{{.oops" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSpec()
			tc.mutate(&spec)
			p, err := prompt.NewPrompt(spec)
			if tc.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				if !errors.Is(err, prompt.ErrInvalidPrompt) {
					t.Fatalf("want ErrInvalidPrompt, got %v", err)
				}
			}
			if tc.ok && p == nil {
				t.Fatal("want aggregate, got nil")
			}
		})
	}
}

func TestRender(t *testing.T) {
	p := prompt.MustPrompt(validSpec())

	got, err := p.Render(map[string]string{"format": "PDF"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "from PDF into JSON") {
		t.Fatalf("rendered = %q", got)
	}
}

func TestRenderMissingParamFails(t *testing.T) {
	p := prompt.MustPrompt(validSpec())

	_, err := p.Render(map[string]string{})
	if !errors.Is(err, prompt.ErrInvalidPrompt) {
		t.Fatalf("want ErrInvalidPrompt for missing placeholder, got %v", err)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	p := prompt.MustPrompt(validSpec())
	snap := p.Snapshot()

	back := prompt.RehydrateFromSnapshot(snap)
	if back.ID() != p.ID() || back.Version() != p.Version() || back.Purpose() != p.Purpose() {
		t.Fatalf("round trip lost identity: %+v vs %+v", back, p)
	}
	out, err := back.Render(map[string]string{"format": "HTML"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "from HTML") {
		t.Fatalf("rehydrated render = %q", out)
	}

	// snapshot isolation: mutating the DTO must not reach the aggregate
	snap.Params[0] = "mutated"
	if p.Snapshot().Params[0] != "format" {
		t.Fatal("snapshot shares state with the aggregate")
	}
}
