package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mariotoffia/testmaker/app/llm"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/prompt"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/source"
	"github.com/mariotoffia/testmaker/ports"
)

// ErrNoPayload marks an LLM extraction run whose fetched artifacts carried no
// text to extract from — there is nothing to lift, so the model is never called.
var ErrNoPayload = &shared.TestmakerError{
	Code: "ingest.no_payload", Class: shared.ClassInvalid, Message: "no text payload to extract",
}

// ErrNoLLM marks an IngestLLM call made without an LLM service — a composition
// wiring error, not a data outcome.
var ErrNoLLM = &shared.TestmakerError{
	Code: "ingest.no_llm", Class: shared.ClassInvalid, Message: "IngestLLM requires an LLM service",
}

// ErrExtractParse marks an LLM completion that did not decode into the extraction
// schema shape — a backend/prompt failure, not a routine partial skip.
var ErrExtractParse = &shared.TestmakerError{
	Code: "ingest.extract_parse", Class: shared.ClassInvalid, Message: "llm extraction output did not parse",
}

// maxPayloadChars caps how much fetched text is handed to the model in one call.
// ponytail: fixed ceiling, whole-payload-in-one-call. Chunk + map/reduce if a
// source's payload ever exceeds a single context window.
const maxPayloadChars = 100_000

// defaultBand is the difficulty assigned when the model omits or gives an
// out-of-range difficulty — the middle of the 1..5 scale it is asked for.
const defaultBand = 3

// extractionSchema constrains the model to the structured shape parseItems
// decodes. Every field the parser reads is present; "difficulty" is optional
// (parseItems clamps/defaults it).
const extractionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "stem": {"type": "string"},
          "options": {"type": "array", "items": {"type": "string"}, "minItems": 4, "maxItems": 6},
          "answer_index": {"type": "integer"},
          "explanation": {"type": "string"},
          "difficulty": {"type": "integer"}
        },
        "required": ["stem", "options", "answer_index"]
      }
    }
  },
  "required": ["items"]
}`

// LLMExtractRequest asks the ingest service to lift a source's unstructured
// fetched payload into item candidates via an LLM.
type LLMExtractRequest struct {
	Source    source.Snapshot     // catalogue source to fetch and tag provenance from
	LLM       *llm.Service        // the LLM library (backend + prompt store + hooks)
	TestType  shared.TestTypeCode // taxonomy code the extracted items are tagged with
	Model     string              // backend model id (e.g. "gpt-4o-mini", "qwen2.5:0.5b")
	MaxTokens int                 // output token cap (0 = backend default)
	Limit     int                 // fetch cap (0 = fetcher default)
}

// IngestLLM fetches a source's unstructured payload, lifts it into item
// candidates with the stored extraction prompt (applied by the LLM service per
// Purpose) under a JSON schema, then validates every candidate through
// item.NewItem before storing — LLM output is untrusted input, so nothing
// reaches the bank unvalidated. It returns a per-stage Report; Note records the
// model and prompt provenance of the run.
//
// Candidates are tagged item.OriginGenerated (the domain's own Origin doc counts
// LLM output as generated): an LLM-lifted item is a model transformation of the
// source text, not a verbatim source record, so calibration must treat it as
// unnormed — distinct from a deterministically normalized fetched item.
//
// ponytail: model + prompt id/version live in the Report, not as new
// item.Provenance fields — no consumer (psychometric calibration) reads them
// yet. Add Model/PromptID/PromptVersion to item.Provenance when one does. The
// decision and its upgrade path are recorded in docs/adr/0004.
func (s *Service) IngestLLM(ctx context.Context, req LLMExtractRequest) (Report, error) {
	rep := Report{SourceID: req.Source.ID}
	if req.LLM == nil {
		return rep, ErrNoLLM.With("source", string(req.Source.ID))
	}

	res, err := s.fetchFor(ctx, req.Source, req.Limit)
	if err != nil {
		return rep, err
	}
	rep.Fetched = len(res.Items)

	payload := joinPayload(res.Items)
	if payload == "" {
		return rep, ErrNoPayload.WithMessagef("source %q: fetched artifacts had no text", req.Source.ID).
			With("source", string(req.Source.ID))
	}

	out, err := req.LLM.GenerateFor(ctx, prompt.PurposeExtraction,
		map[string]string{"source": req.Source.Name, "testType": string(req.TestType)},
		ports.LLMRequest{
			Model:      req.Model,
			MaxTokens:  req.MaxTokens,
			Messages:   []ports.LLMMessage{{Role: ports.LLMRoleUser, Content: payload}},
			JSONSchema: extractionSchema,
		},
	)
	if err != nil {
		return rep, err
	}
	rep.Note = fmt.Sprintf("llm extraction: model=%s prompt=%s v%d",
		out.Model, out.PromptID, out.PromptVersion)

	specs, err := s.extractSpecs(req, out.Content)
	if err != nil {
		return rep, err
	}
	if err := s.saveSpecs(ctx, specs, &rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// extractSpecs decodes the model's JSON completion into validated-input specs.
// A completion that does not decode at all is ErrExtractParse (a backend/prompt
// failure); individual malformed candidates are dropped by saveSpecs via
// item.NewItem.
func (s *Service) extractSpecs(req LLMExtractRequest, content string) ([]item.ItemSpec, error) {
	var out struct {
		Items []struct {
			Stem        string   `json:"stem"`
			Options     []string `json:"options"`
			AnswerIndex int      `json:"answer_index"`
			Explanation string   `json:"explanation"`
			Difficulty  int      `json:"difficulty"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, ErrExtractParse.WithMessagef("source %q", req.Source.ID).Wrap(err)
	}

	specs := make([]item.ItemSpec, 0, len(out.Items))
	seen := make(map[item.ItemID]struct{}, len(out.Items))
	for _, ci := range out.Items {
		// Content-addressed id: re-extracting the same stem+options is
		// idempotent (rewrites its own item) instead of clobbering an
		// unrelated item that merely shared a positional index.
		id := item.ItemID(fmt.Sprintf("%s-llm-%s", req.Source.ID, contentHash(ci.Stem, ci.Options)))
		if _, dup := seen[id]; dup {
			// Byte-identical candidates in one completion collapse to a single
			// item, so the run's Saved count reflects distinct items persisted
			// rather than repeated upserts of the same id.
			continue
		}
		seen[id] = struct{}{}
		options := make([]item.Option, len(ci.Options))
		for j, text := range ci.Options {
			options[j] = item.Option{ID: "o" + strconv.Itoa(j), Text: text}
		}
		specs = append(specs, item.ItemSpec{
			ID: id,
			Provenance: item.Provenance{
				SourceID:        string(req.Source.ID),
				Origin:          item.OriginGenerated,
				Redistributable: req.Source.License.Redistributable,
			},
			TestType:     req.TestType,
			Stimulus:     []item.StimulusPart{{Text: ci.Stem}},
			AnswerFormat: item.FormatMultipleChoice,
			Options:      options,
			// An out-of-range answer_index yields a key referencing no option, so
			// item.NewItem rejects the candidate (dropped by saveSpecs). This
			// guards only against out-of-range keys; an in-range but wrong index
			// still yields a valid item with a mislabelled correct answer.
			AnswerKey:   item.AnswerKey{OptionID: "o" + strconv.Itoa(ci.AnswerIndex)},
			Explanation: ci.Explanation,
			Difficulty:  item.Difficulty{Band: clampBand(ci.Difficulty)},
		})
	}
	return specs, nil
}

// contentHash derives a stable short id from an item's stem and options, so an
// extracted item's identity tracks its content rather than its position in the
// model's output array.
func contentHash(stem string, options []string) string {
	h := sha256.New()
	h.Write([]byte(stem))
	for _, o := range options {
		h.Write([]byte{0})
		h.Write([]byte(o))
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// joinPayload concatenates the text of the fetched artifacts (RawItem.Stem, else
// the inlined "content"), capped at maxPayloadChars. The cap counts the "\n\n"
// separators and never splits a multibyte rune, so the budget can neither be
// overrun nor drive a negative slice bound.
func joinPayload(raw []ports.RawItem) string {
	const sep = "\n\n"
	var b strings.Builder
	for _, it := range raw {
		text := it.Stem
		if text == "" {
			if s, ok := it.Raw["content"].(string); ok {
				text = s
			}
		}
		if text == "" {
			continue
		}
		rem := maxPayloadChars - b.Len()
		if rem <= 0 {
			break
		}
		if b.Len() > 0 {
			if len(sep) >= rem {
				break
			}
			b.WriteString(sep)
			rem -= len(sep)
		}
		if len(text) > rem {
			b.WriteString(text[:runeSafe(text, rem)])
			break
		}
		b.WriteString(text)
	}
	return b.String()
}

// runeSafe returns the largest byte offset <= n that lands on a UTF-8 rune
// boundary of s, so slicing at it never splits a multibyte character.
func runeSafe(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}

// clampBand maps a model-supplied difficulty into the valid 1..5 band, defaulting
// an omitted or out-of-range value to the middle.
func clampBand(d int) int {
	switch {
	case d < 1 || d > 5:
		return defaultBand
	default:
		return d
	}
}
