package rulegen

import (
	"encoding/base64"
	"fmt"
	"math"
	"strings"
)

// A figure is one abstract figural element: a shape, repeated a number of
// times, at an orientation, filled or outlined. It is the atom every engine
// composes into sequences, grids and option sets, and the unit whose equality
// (by visual appearance) guarantees an item has exactly one correct answer.
type figure struct {
	shape       shape
	count       int  // 1..4 repetitions of the shape
	orientation int  // degrees; only visible for rotationally-asymmetric shapes
	filled      bool // solid vs outline
}

// shape is the closed set of silhouettes the engine can draw. Diamond is
// intentionally absent: it is a square rotated 45°, so including it would let
// two structurally-different figures render identically.
type shape string

const (
	shapeCircle   shape = "circle"
	shapeSquare   shape = "square"
	shapeTriangle shape = "triangle"
	shapePentagon shape = "pentagon"
)

// Attribute domains. Functions (not package globals) so the adapter keeps no
// mutable global state; the slices are tiny and short-lived.
func shapes() []shape { return []shape{shapeCircle, shapeSquare, shapeTriangle, shapePentagon} }
func counts() []int   { return []int{1, 2, 3, 4} }

// orientations lists the four orientations. They are visually distinct only for
// triangle and pentagon; canonicalOrientation collapses the others, so an
// orientation-varying rule must pick an asymmetric shape (asymmetricShapes).
func orientations() []int       { return []int{0, 90, 180, 270} }
func asymmetricShapes() []shape { return []shape{shapeTriangle, shapePentagon} }

// canonicalOrientation maps an orientation to its visually-distinguishable form
// for a shape. A circle looks the same at every angle; a square repeats every
// 90°, so within our 0/90/180/270 domain every value collapses to 0. Triangle
// and pentagon keep their angle — no two domain values coincide under their
// symmetry — so those are the shapes an orientation rule may vary.
func canonicalOrientation(s shape, deg int) int {
	switch s {
	case shapeCircle, shapeSquare:
		return 0
	default:
		return ((deg % 360) + 360) % 360
	}
}

// visualKey identifies a figure by how it *looks*, not by its raw fields, so two
// figures that render identically compare equal. Engines dedupe options and
// pick distractors on this key, which is what makes "exactly one correct
// answer" a structural guarantee rather than a hope.
func (f figure) visualKey() string {
	return fmt.Sprintf("%s|%d|%d|%t", f.shape, f.count, canonicalOrientation(f.shape, f.orientation), f.filled)
}

// sameVisual reports whether two figures are indistinguishable when drawn.
func (f figure) sameVisual(g figure) bool { return f.visualKey() == g.visualKey() }

// describe renders a short alt-text description for the SVG <title> so the
// figure carries a screen-reader label (accessibility basics are not lazied
// away, even for inherently visual items).
func (f figure) describe() string {
	fill := "outlined"
	if f.filled {
		fill = "filled"
	}
	plural := ""
	if f.count != 1 {
		plural = "s"
	}
	return fmt.Sprintf("%d %s %s%s at %d degrees", f.count, fill, f.shape, plural, canonicalOrientation(f.shape, f.orientation))
}

// --- SVG rendering ---------------------------------------------------------
//
// ponytail: figures are drawn as SVG and inlined as base64 data-URIs so a
// generated item is fully self-contained and viewable with no blob store. The
// Block 11 blob store now offloads these data-URIs to content refs at the
// composition root (app/authoring, when a store is wired) — the item shape
// (MediaKind + MediaRef) does not change, and a nil store keeps them inline.

const (
	cellSize    = 100.0 // one figure occupies a 100x100 cell
	strokeWidth = 4.0
	inkColor    = "#111"
)

// layout returns the centre points (in a 100x100 cell) for `count` repetitions.
func layout(count int) [][2]float64 {
	switch count {
	case 1:
		return [][2]float64{{50, 50}}
	case 2:
		return [][2]float64{{33, 50}, {67, 50}}
	case 3:
		return [][2]float64{{50, 32}, {33, 67}, {67, 67}}
	default: // 4
		return [][2]float64{{33, 33}, {67, 33}, {33, 67}, {67, 67}}
	}
}

// radiusFor sizes a single shape so `count` copies fit without overlapping.
func radiusFor(count int) float64 {
	if count == 1 {
		return 30
	}
	return 18
}

// shapeSVG draws one shape centred at (cx,cy) with the figure's orientation/fill.
func shapeSVG(s shape, cx, cy, r float64, orientation int, filled bool) string {
	style := fmt.Sprintf(`fill="none" stroke="%s" stroke-width="%g"`, inkColor, strokeWidth)
	if filled {
		style = fmt.Sprintf(`fill="%s" stroke="%s" stroke-width="%g"`, inkColor, inkColor, strokeWidth)
	}
	inner := shapeGeometry(s, r)
	return fmt.Sprintf(`<g transform="translate(%g,%g) rotate(%d)">%s</g>`,
		cx, cy, canonicalOrientation(s, orientation), fmt.Sprintf(inner, style))
}

// shapeGeometry returns the shape's SVG element with a single %s placeholder for
// the style attributes, centred at the origin.
func shapeGeometry(s shape, r float64) string {
	switch s {
	case shapeCircle:
		return fmt.Sprintf(`<circle cx="0" cy="0" r="%g" %%s/>`, r)
	case shapeSquare:
		return fmt.Sprintf(`<rect x="%g" y="%g" width="%g" height="%g" %%s/>`, -r, -r, 2*r, 2*r)
	case shapeTriangle:
		return fmt.Sprintf(`<polygon points="%s" %%s/>`, polygonPoints(3, r, -90))
	default: // pentagon
		return fmt.Sprintf(`<polygon points="%s" %%s/>`, polygonPoints(5, r, -90))
	}
}

// polygonPoints returns n points evenly spaced on a circle of radius r starting
// at startDeg, formatted for an SVG polygon.
func polygonPoints(n int, r, startDeg float64) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		a := (startDeg + float64(i)*360/float64(n)) * math.Pi / 180
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.2f,%.2f", r*math.Cos(a), r*math.Sin(a))
	}
	return b.String()
}

// cellSVG draws a whole figure (all its repetitions) inside one 100x100 cell,
// offset by (dx,dy).
func cellSVG(f figure, dx, dy float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<g transform="translate(%g,%g)">`, dx, dy)
	r := radiusFor(f.count)
	for _, p := range layout(f.count) {
		b.WriteString(shapeSVG(f.shape, p[0], p[1], r, f.orientation, f.filled))
	}
	b.WriteString(`</g>`)
	return b.String()
}

// questionCellSVG draws a "?" placeholder cell (the missing element).
func questionCellSVG(dx, dy float64) string {
	return fmt.Sprintf(`<g transform="translate(%g,%g)"><text x="50" y="62" font-size="48" `+
		`text-anchor="middle" fill="%s">?</text></g>`, dx, dy, inkColor)
}

// svgDataURI wraps a body of SVG cells into a titled <svg> of the given size and
// encodes it as a base64 data-URI (unambiguous — no escaping pitfalls). The root
// carries explicit width/height (not just a viewBox): a viewBox alone gives an
// aspect ratio but no intrinsic size, so an <img> referencing it collapses to
// 0x0 in Chromium and the figure renders blank.
func svgDataURI(w, h float64, title, body string) string {
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g" role="img">`+
		`<title>%s</title>%s</svg>`, w, h, w, h, title, body)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

// figureURI renders a single figure as a standalone SVG data-URI (used for
// answer options).
func figureURI(f figure) string {
	return svgDataURI(cellSize, cellSize, f.describe(), cellSVG(f, 0, 0))
}

// stripURI renders a left-to-right sequence of figures, appending a "?" cell,
// as one SVG data-URI (used for the figure-series stimulus).
func stripURI(figs []figure) string {
	var b strings.Builder
	for i, f := range figs {
		b.WriteString(cellSVG(f, float64(i)*cellSize, 0))
	}
	b.WriteString(questionCellSVG(float64(len(figs))*cellSize, 0))
	return svgDataURI(float64(len(figs)+1)*cellSize, cellSize, "figure series, choose the next figure", b.String())
}

// gridURI renders a 3x3 grid with cell blankIdx replaced by a "?" (used for the
// matrix stimulus).
func gridURI(cells []figure, blankIdx int) string {
	var b strings.Builder
	for i, f := range cells {
		dx, dy := float64(i%3)*cellSize, float64(i/3)*cellSize
		if i == blankIdx {
			b.WriteString(questionCellSVG(dx, dy))
			continue
		}
		b.WriteString(cellSVG(f, dx, dy))
	}
	return svgDataURI(3*cellSize, 3*cellSize, "3 by 3 matrix, choose the missing tile", b.String())
}
