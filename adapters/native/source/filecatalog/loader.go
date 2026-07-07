package filecatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mariotoffia/testmaker/domain/source"
)

// Loader reads a source catalogue file (JSON or YAML) and returns validated
// snapshots. It implements ports.CatalogLoader.
type Loader struct {
	path string
}

// NewLoader returns a loader bound to a catalogue file path. Format is chosen by
// extension (.json => JSON, otherwise YAML).
func NewLoader(path string) *Loader {
	return &Loader{path: path}
}

// Load reads, parses and validates the catalogue.
func (l *Loader) Load(_ context.Context) ([]source.Snapshot, error) {
	raw, err := os.ReadFile(l.path)
	if err != nil {
		return nil, fmt.Errorf("filecatalog: read %s: %w", l.path, err)
	}

	var wire wireCatalog
	if strings.HasSuffix(strings.ToLower(l.path), ".json") {
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, fmt.Errorf("filecatalog: parse json %s: %w", l.path, err)
		}
	} else {
		if err := yaml.Unmarshal(raw, &wire); err != nil {
			return nil, fmt.Errorf("filecatalog: parse yaml %s: %w", l.path, err)
		}
	}

	out := make([]source.Snapshot, 0, len(wire.Sources))
	for _, ws := range wire.Sources {
		src, verr := source.NewSource(ws.toSpec())
		if verr != nil {
			return nil, fmt.Errorf("filecatalog: source %q: %w", ws.ID, verr)
		}
		out = append(out, src.Snapshot())
	}
	return out, nil
}

// --- wire DTOs (the only place that knows the on-disk schema) ---------------

type wireCatalog struct {
	Sources []wireSource `json:"sources" yaml:"sources"`
}

type wireSource struct {
	ID              string         `json:"id" yaml:"id"`
	Name            string         `json:"name" yaml:"name"`
	Provider        string         `json:"provider" yaml:"provider"`
	URLs            []string       `json:"urls" yaml:"urls"`
	AccessClasses   []string       `json:"access_class" yaml:"access_class"`
	Formats         []string       `json:"formats" yaml:"formats"`
	License         wireLicense    `json:"license" yaml:"license"`
	TestTypes       []string       `json:"test_types" yaml:"test_types"`
	ItemCount       string         `json:"item_count" yaml:"item_count"`
	AnswerKeys      string         `json:"answer_keys" yaml:"answer_keys"`
	NormsDifficulty string         `json:"norms_difficulty" yaml:"norms_difficulty"`
	Languages       []string       `json:"languages" yaml:"languages"`
	Extraction      wireExtraction `json:"extraction" yaml:"extraction"`
	Generator       bool           `json:"generator" yaml:"generator"`
	Priority        string         `json:"priority" yaml:"priority"`
	IPRisk          string         `json:"ip_risk" yaml:"ip_risk"`
	Category        string         `json:"category" yaml:"category"`
	Notes           string         `json:"notes" yaml:"notes"`
}

type wireLicense struct {
	Category        string `json:"category" yaml:"category"`
	Detail          string `json:"detail" yaml:"detail"`
	Redistributable string `json:"redistributable" yaml:"redistributable"`
}

type wireExtraction struct {
	Method  string `json:"method" yaml:"method"`
	Auth    string `json:"auth" yaml:"auth"`
	ItemsAs string `json:"items_as" yaml:"items_as"`
	Notes   string `json:"notes" yaml:"notes"`
}

func (ws wireSource) toSpec() source.SourceSpec {
	return source.SourceSpec{
		ID:            source.SourceID(ws.ID),
		Name:          ws.Name,
		Provider:      ws.Provider,
		URLs:          ws.URLs,
		AccessClasses: toAccessClasses(ws.AccessClasses),
		Formats:       ws.Formats,
		License: source.License{
			Category:        source.LicenseCategory(ws.License.Category),
			Detail:          ws.License.Detail,
			Redistributable: source.Redistributable(ws.License.Redistributable),
		},
		TestTypes:       toTestTypes(ws.TestTypes),
		ItemCount:       ws.ItemCount,
		AnswerKeys:      source.Availability(ws.AnswerKeys),
		NormsDifficulty: source.Availability(ws.NormsDifficulty),
		Languages:       ws.Languages,
		Extraction: source.Extraction{
			Method:  source.ExtractionMethod(ws.Extraction.Method),
			Auth:    ws.Extraction.Auth,
			ItemsAs: ws.Extraction.ItemsAs,
			Notes:   ws.Extraction.Notes,
		},
		Generator: ws.Generator,
		Priority:  source.Priority(ws.Priority),
		IPRisk:    source.IPRisk(ws.IPRisk),
		Category:  source.Category(ws.Category),
		Notes:     ws.Notes,
	}
}

func toAccessClasses(in []string) []source.AccessClass {
	out := make([]source.AccessClass, len(in))
	for i, v := range in {
		out[i] = source.AccessClass(v)
	}
	return out
}

func toTestTypes(in []string) []source.TestTypeCode {
	out := make([]source.TestTypeCode, len(in))
	for i, v := range in {
		out[i] = source.TestTypeCode(v)
	}
	return out
}
