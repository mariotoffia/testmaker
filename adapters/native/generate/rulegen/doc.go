// Package rulegen is a native Go rule-engine adapter implementing
// ports.Generator: it procedurally generates figural cognitive-test items with
// ground-truth answer keys and no external dependencies.
//
// It resolves DESIGN.md open question #299 (shell out to external engines vs.
// port Go rule logic) in favour of native Go rules — IP-free, deterministic by
// seed, and vendor-free (stdlib only). The catalogued generator sources
// (Sandia SGMT, matRiks, RAVEN family, Bongard-LOGO) are format references, not
// dependencies.
//
// Coverage is the primary figural families: A1/A3 figure series, A2 matrices,
// and A4 odd-one-out. Each item's correct answer is derived from the same rules
// that build its stimulus, so keys are correct by construction; figures are
// drawn as SVG and inlined as base64 data-URIs, so a generated item is fully
// self-contained and needs no blob store (Block 11 is the upgrade path).
package rulegen
