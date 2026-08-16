package layoutdiff

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// The overlay is a SEPARATE layer appended to a finished SVG, never a
// modification of one. pkg/ipmsvg stays the single renderer of a diagram —
// the audit draws on top of its output the way GenerateWithHints appends its
// decoration block. Two consequences worth keeping: the overlay scales with
// the diagram because it shares its coordinate system, and switching it off
// is one CSS rule, which is what makes the report's plain⇄highlighted flap
// free.
const (
	colWorse   = "#e03131" // removed, or an invariant that appeared
	colBetter  = "#2f9e44" // added, or an invariant that went away
	colChanged = "#7048e8" // same thing, drawn differently
	colMoved   = "#f08c00" // same thing, moved
)

// OverlayClass is the class on the appended group. The report toggles
// `opacity` on it; anything else consuming these SVGs can ignore it.
const OverlayClass = "audit-overlay"

// OverlaySVG appends an overlay layer, drawn from rep, to an SVG rendered
// from the NEW graph. Coordinates in rep are already in that graph's space.
// Returns svg unchanged when there is nothing to draw.
func OverlaySVG(svg []byte, rep Report) []byte {
	if len(rep.Changes) == 0 {
		return svg
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  <g class=%q fill=\"none\">\n", OverlayClass)
	for _, c := range rep.Changes {
		drawChange(&b, c)
	}
	b.WriteString("  </g>\n")

	out := string(svg)
	if i := strings.LastIndex(out, "</svg>"); i >= 0 {
		return []byte(out[:i] + b.String() + out[i:])
	}
	return svg
}

func drawChange(b *strings.Builder, c Change) {
	switch c.Kind {
	case KindNodeAdded:
		rect(b, c.New, colBetter, false)
		tag(b, c.New, colBetter, "new")
	case KindNodeRemoved:
		rect(b, c.Old, colWorse, true)
		tag(b, c.Old, colWorse, "was here")
	case KindNodeMoved:
		rect(b, c.Old, colMoved, true)
		rect(b, c.New, colMoved, false)
		arrow(b, c.Old, c.New, colMoved)
		tag(b, c.New, colMoved, c.Detail)
	case KindNodeResized:
		rect(b, c.Old, colChanged, true)
		rect(b, c.New, colChanged, false)
		tag(b, c.New, colChanged, c.Detail)
	case KindRelabelled, KindContainerShell:
		rect(b, c.New, colChanged, false)
		tag(b, c.New, colChanged, c.Detail)
	case KindEdgeAdded:
		path(b, c.NewPath, colBetter, false, 6)
	case KindEdgeRemoved:
		path(b, c.OldPath, colWorse, true, 4)
	case KindPortSide, KindBendCount, KindVisibility, KindDeferred:
		path(b, c.OldPath, colChanged, true, 4)
		path(b, c.NewPath, colChanged, false, 7)
		chip(b, c.At, colChanged, c.Detail)
	case KindBendMoved:
		path(b, c.OldPath, colMoved, true, 4)
		path(b, c.NewPath, colMoved, false, 7)
	case KindPortSlide:
		chip(b, c.At, colMoved, c.Detail)
	case KindFindingAdded:
		for i := range c.Boxes {
			rect(b, &c.Boxes[i], colWorse, false)
		}
	case KindFindingFixed:
		for i := range c.Boxes {
			rect(b, &c.Boxes[i], colBetter, true)
		}
	}
}

func rect(b *strings.Builder, box *Box, color string, ghost bool) {
	if box == nil {
		return
	}
	dash := ""
	opacity := "1"
	if ghost {
		dash = ` stroke-dasharray="6 4"`
		opacity = "0.75"
	}
	fmt.Fprintf(b, "    <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"6\" ry=\"6\" stroke=\"%s\" stroke-width=\"3\"%s opacity=\"%s\"/>\n",
		box.X-3, box.Y-3, box.W+6, box.H+6, color, dash, opacity)
}

func path(b *strings.Builder, pts []layout.Position, color string, ghost bool, width int) {
	if len(pts) < 2 {
		return
	}
	var sb strings.Builder
	for i, p := range pts {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%d,%d", p.X, p.Y)
	}
	dash := ""
	opacity := "0.55"
	if ghost {
		dash = ` stroke-dasharray="8 5"`
		opacity = "0.9"
	}
	fmt.Fprintf(b, "    <polyline points=\"%s\" stroke=\"%s\" stroke-width=\"%d\" stroke-linecap=\"round\" stroke-linejoin=\"round\"%s opacity=\"%s\"/>\n",
		sb.String(), color, width, dash, opacity)
}

// arrow draws old-centre → new-centre so a move reads as a direction, not as
// two rectangles the reader has to pair up themselves.
func arrow(b *strings.Builder, from, to *Box, color string) {
	if from == nil || to == nil {
		return
	}
	x1, y1 := from.X+from.W/2, from.Y+from.H/2
	x2, y2 := to.X+to.W/2, to.Y+to.H/2
	if x1 == x2 && y1 == y2 {
		return
	}
	fmt.Fprintf(b, "    <line x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" stroke=\"%s\" stroke-width=\"3\" opacity=\"0.9\"/>\n",
		x1, y1, x2, y2, color)
	fmt.Fprintf(b, "    <circle cx=\"%d\" cy=\"%d\" r=\"5\" fill=\"%s\" stroke=\"none\"/>\n", x2, y2, color)
}

func tag(b *strings.Builder, box *Box, color, text string) {
	if box == nil || text == "" {
		return
	}
	label(b, box.X-3, box.Y-9, color, text)
}

func chip(b *strings.Builder, at *layout.Position, color, text string) {
	if at == nil || text == "" {
		return
	}
	label(b, at.X+8, at.Y-6, color, text)
}

func label(b *strings.Builder, x, y int, color, text string) {
	w := 7*len(text) + 10
	fmt.Fprintf(b, "    <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"18\" rx=\"4\" ry=\"4\" fill=\"%s\" stroke=\"none\" opacity=\"0.92\"/>\n",
		x, y-14, w, color)
	fmt.Fprintf(b, "    <text x=\"%d\" y=\"%d\" font-family=\"Helvetica,Inter,Segoe UI,Arial\" font-size=\"12\" font-weight=\"bold\" fill=\"#ffffff\" stroke=\"none\">%s</text>\n",
		x+5, y-1, html.EscapeString(text))
}

// findingBoxRe matches the `[x,y WxH]` geometry layoutcheck prints beside a
// node in a finding message ("reads as paired: \"Freeze\" [220,360 120x60] …").
// Best effort by construction: a message with no geometry simply gets no
// marker, which is why the overlay never depends on it being there.
var findingBoxRe = regexp.MustCompile(`\[(-?\d+),(-?\d+) (\d+)x(\d+)\]`)

func findingBoxes(finding string) []Box {
	var out []Box
	for _, m := range findingBoxRe.FindAllStringSubmatch(finding, -1) {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		w, _ := strconv.Atoi(m[3])
		h, _ := strconv.Atoi(m[4])
		out = append(out, Box{X: x, Y: y, W: w, H: h})
	}
	return out
}
