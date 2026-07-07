package prompt

import (
	"slices"
	"strings"
	"text/template"

	"github.com/mariotoffia/testmaker/domain/shared"
)

// PromptID uniquely identifies a stored prompt (kebab-case slug).
type PromptID string

// Purpose is the LLM step a prompt automatically applies to. Closed set: a
// purpose without consuming code is dead data, so new purposes arrive with
// the block that consumes them.
type Purpose string

const (
	PurposeExtraction  Purpose = "extraction"  // unstructured RawItem payload -> item candidates
	PurposeTranslation Purpose = "translation" // translate stem/options/explanations
	PurposeDerivation  Purpose = "derivation"  // run-time variant of the item just administered
	PurposeGeneration  Purpose = "generation"  // designer-driven item generation
)

var validPurposes = map[Purpose]struct{}{
	PurposeExtraction: {}, PurposeTranslation: {}, PurposeDerivation: {}, PurposeGeneration: {},
}

// Valid reports whether the purpose is known.
func (p Purpose) Valid() bool { _, ok := validPurposes[p]; return ok }

// Spec is the validated input to NewPrompt.
type Spec struct {
	ID       PromptID
	Version  int      // >= 1; recorded as provenance on every generation
	Purpose  Purpose  // which LLM step this prompt auto-applies to
	Template string   // Go text/template; values are referenced as {{.name}}
	Params   []string // documented placeholder names (informational, not enforced)
	Notes    string
}

// Prompt is the aggregate root: a stored, versioned prompt template. State is
// private and validated; it crosses ports only as a Snapshot.
type Prompt struct {
	id       PromptID
	version  int
	purpose  Purpose
	template string
	params   []string
	notes    string
}

// NewPrompt validates a spec and returns the aggregate.
func NewPrompt(spec Spec) (*Prompt, *shared.TestmakerError) {
	if spec.ID == "" {
		return nil, ErrInvalidPrompt.WithMessage("prompt id is required")
	}
	if spec.Version < 1 {
		return nil, ErrInvalidPrompt.WithMessage("version must be >= 1").With("id", string(spec.ID))
	}
	if !spec.Purpose.Valid() {
		return nil, ErrInvalidPrompt.WithMessage("unknown purpose").
			With("id", string(spec.ID)).With("purpose", string(spec.Purpose))
	}
	if strings.TrimSpace(spec.Template) == "" {
		return nil, ErrInvalidPrompt.WithMessage("template is required").With("id", string(spec.ID))
	}
	if _, err := parse(spec.Template); err != nil {
		return nil, ErrInvalidPrompt.WithMessage("template does not parse").
			Wrap(err).With("id", string(spec.ID))
	}
	return &Prompt{
		id:       spec.ID,
		version:  spec.Version,
		purpose:  spec.Purpose,
		template: spec.Template,
		params:   slices.Clone(spec.Params),
		notes:    spec.Notes,
	}, nil
}

// MustPrompt panics on invalid input; for tests and static fixtures only.
func MustPrompt(spec Spec) *Prompt {
	p, err := NewPrompt(spec)
	if err != nil {
		panic(err)
	}
	return p
}

func (p *Prompt) ID() PromptID { return p.id }

func (p *Prompt) Version() int { return p.version }

func (p *Prompt) Purpose() Purpose { return p.purpose }

// Render fills the template with the given values. A placeholder without a
// value is an error — a bad call site must not ship a broken prompt.
func (p *Prompt) Render(values map[string]string) (string, *shared.TestmakerError) {
	tmpl, err := parse(p.template)
	if err != nil {
		// unreachable for aggregates built via NewPrompt; guards rehydration
		return "", ErrInvalidPrompt.WithMessage("template does not parse").
			Wrap(err).With("id", string(p.id))
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, values); err != nil {
		return "", ErrInvalidPrompt.WithMessage("template render failed").
			Wrap(err).With("id", string(p.id))
	}
	return b.String(), nil
}

// parse compiles the Go text/template with missing placeholders as errors.
// ponytail: re-parsed per Render — prompts are short and rendering is not a
// hot path; caching the parsed template buys nothing yet.
func parse(text string) (*template.Template, error) {
	//nolint:wrapcheck // callers wrap into ErrInvalidPrompt
	return template.New("prompt").Option("missingkey=error").Parse(text)
}

// Snapshot is the dependency-neutral DTO used to persist/transport a Prompt.
type Snapshot struct {
	ID       PromptID
	Version  int
	Purpose  Purpose
	Template string
	Params   []string
	Notes    string
}

// Snapshot returns the persistence/transport DTO for the aggregate.
func (p *Prompt) Snapshot() Snapshot {
	return Snapshot{
		ID:       p.id,
		Version:  p.version,
		Purpose:  p.purpose,
		Template: p.template,
		Params:   slices.Clone(p.params),
		Notes:    p.notes,
	}
}

// RehydrateFromSnapshot rebuilds an aggregate from a trusted snapshot without
// re-validating (the snapshot is assumed to have passed NewPrompt previously).
func RehydrateFromSnapshot(s Snapshot) *Prompt {
	return &Prompt{
		id: s.ID, version: s.Version, purpose: s.Purpose,
		template: s.Template, params: slices.Clone(s.Params), notes: s.Notes,
	}
}
