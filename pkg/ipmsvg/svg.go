package ipmsvg

// Package ipmsvg renders minimal SVG previews from layout graphs.
// Produces static SVG with the standard ipm styling (colors, fonts, shadows).
//
// Spec: gl:docs/ipmsvg-gen.md
// Layout model: gl:pkg/layout/generate.go
// Color palette and styling: gl:docs/dev/layout-gen/layout-alg.md#L581-L583 (events), gl:docs/dev/layout-gen/layout-alg.md#L366-L368 (things), gl:docs/dev/layout-gen/layout-alg.md#L581-L583 (concepts)
// CLI tool: gl:cmd/ipmsvg-gen/main.go

import (
	"fmt"
	"html"
	"math"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layout7"
	"github.com/infinite-pm/ipm-tools/pkg/nodehints"
)

// Generator produces minimal SVG previews from layout graphs.
//
// Stub rendering is NOT an option: an edge whose Visibility the layout
// classified "stubbed" always renders as a numbered stub pair (short
// segment + fade + pair-number badge at each end, the full edge kept
// hidden as class "edge-full" for hover reveal). The flat SVG and the
// canvas agree on what is hidden — classify once, consume everywhere.
type Generator struct {
	// Future: font family, stroke widths, color overrides
}

// NewGenerator creates a new SVG generator.
func NewGenerator() (*Generator, error) {
	return &Generator{}, nil
}

// Generate returns a minimal SVG preview for a layout graph.
// Implements styling from gl:docs/dev/layout-gen/layout-alg.md (events, things, concepts sections)
func (g *Generator) Generate(graph *layout.Graph) ([]byte, error) {
	if graph == nil {
		return nil, fmt.Errorf("nil graph")
	}
	width := graph.Meta.Bounds.Width
	height := graph.Meta.Bounds.Height
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}

	nodes := make([]layout.Node, len(graph.Nodes))
	copy(nodes, graph.Nodes)
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})

	nodeByID := make(map[string]layout.Node, len(nodes))
	for _, n := range nodes {
		nodeByID[n.ID] = n
	}

	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\">\n", width, height, width, height)
	b.WriteString("  <defs>\n")
	b.WriteString("  </defs>\n")

	// Render container shells first (bottom layer) behind everything
	b.WriteString("  <g id=\"container-shells\">\n")
	for _, n := range nodes {
		if n.Container == nil {
			continue
		}
		style := containerStyleFor(n)
		radius := 10
		fmt.Fprintf(&b, "    <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"%d\" ry=\"%d\" fill=\"%s\" stroke=\"%s\" stroke-width=\"2\" stroke-dasharray=\"8 4\" opacity=\"0.6\"/>\n", n.X, n.Y, n.Width, n.Height, radius, radius, style.Fill, style.Stroke)

		// Container shell label (e.g. e2/e4) shown near top-center.
		// This keeps parent event identity visible when children are also shown.
		label := strings.TrimSpace(n.Label)
		if label != "" {
			fmt.Fprintf(&b, "    <text x=\"%d\" y=\"%d\" text-anchor=\"middle\" dominant-baseline=\"middle\" font-family=\"Helvetica,Inter,Segoe UI,Arial\" font-size=\"14\" font-weight=\"bold\" fill=\"%s\">%s</text>\n",
				n.X+n.Width/2, n.Y+16, style.Stroke, html.EscapeString(label))
		}
	}
	b.WriteString("  </g>\n")

	// Render node shadows (skip container nodes — they use the shell above)
	b.WriteString("  <g id=\"node-shadows\">\n")
	for _, n := range nodes {
		if n.Container != nil {
			continue
		}
		radius := 6
		if n.Width <= 40 || n.Height <= 40 {
			radius = 4
		}
		fmt.Fprintf(&b, "    <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"%d\" ry=\"%d\" fill=\"#000000\" stroke=\"#000000\" stroke-width=\"2\" opacity=\"0.25\" transform=\"translate(2,3)\"/>\n", n.X, n.Y, n.Width, n.Height, radius, radius)
	}
	b.WriteString("  </g>\n")

	b.WriteString("  <g id=\"edges\">\n")

	// Group edges by from/to pair to handle parallel edges
	type edgePair struct {
		from, to string
	}
	edgeGroups := make(map[edgePair][]layout.Edge)
	for _, e := range graph.Edges {
		pair := edgePair{e.From, e.To}
		edgeGroups[pair] = append(edgeGroups[pair], e)
	}

	// The layout owns edge geometry: consume the emitted routes (computed in
	// Generate); only graphs from pre-route layout.json files fall back to a
	// fresh computation inside RoutesOf.
	routes := layout.RoutesOf(graph)
	// Same for stub polylines: prefer the emitted ones, compute as fallback.
	var stubFallback map[int]layout.EdgeStubs
	stubsFor := func(idx int) ([]layout.Position, []layout.Position, bool) {
		if r := graph.Edges[idx].Route; r != nil && len(r.SourceStub) == 2 && len(r.TargetStub) == 2 {
			return r.SourceStub, r.TargetStub, true
		}
		if stubFallback == nil {
			stubFallback = layout.ComputeEdgeStubs(graph, routes)
		}
		if s, ok := stubFallback[idx]; ok && len(s.Source) == 2 && len(s.Target) == 2 {
			return s.Source, s.Target, true
		}
		return nil, nil, false
	}

	// Pair codes are assigned in READING ORDER: hidden edges are
	// numbered top to bottom by their EVENT
	// endpoint, so a reader scanning the event flow meets the codes in order.
	// Numbering STARTS AT 2, not 1: a node's first connection is always drawn
	// (never hidden), so "1" is reserved for that always-visible edge and the
	// hidden chips begin at 2.
	pairCodes := map[int]string{}
	{
		type chipAnchor struct {
			edge, y, x int
		}
		anchors := make([]chipAnchor, 0, 4)
		for i, e := range graph.Edges {
			if e.Visibility != "stubbed" {
				continue
			}
			srcStub, tgtStub, ok := stubsFor(i)
			if !ok {
				continue
			}
			ax, ay := srcStub[1].X, srcStub[1].Y
			if tgtStub[1].Y < ay || (tgtStub[1].Y == ay && tgtStub[1].X < ax) {
				ax, ay = tgtStub[1].X, tgtStub[1].Y
			}
			// Anchor the code order on the EVENT endpoint's center, so the codes
			// run top-to-bottom along the event flow a reader scans — not by the
			// shared non-event node's badge fan (a thing fanning
			// hidden edges to several events numbered the TOPMOST event last,
			// because its badge sat lower on the thing's border than the others').
			// The topmost event wins when both ends are events; fall back to the
			// topmost badge when neither end is an event.
			haveEv := false
			for _, id := range []string{e.From, e.To} {
				if n, ok := nodeByID[id]; ok && n.Type == "event" {
					cy, cx := n.Y+n.Height/2, n.X+n.Width/2
					if !haveEv || cy < ay {
						ax, ay, haveEv = cx, cy, true
					}
				}
			}
			anchors = append(anchors, chipAnchor{edge: i, y: ay, x: ax})
		}
		sort.Slice(anchors, func(a, b int) bool {
			if anchors[a].y != anchors[b].y {
				return anchors[a].y < anchors[b].y
			}
			if anchors[a].x != anchors[b].x {
				return anchors[a].x < anchors[b].x
			}
			return anchors[a].edge < anchors[b].edge
		})
		for k, a := range anchors {
			pairCodes[a.edge] = stubPairCode(k + 2) // start at 2; "1" = the always-visible first edge
		}
	}
	for edgeIdx, e := range graph.Edges {
		from, okFrom := nodeByID[e.From]
		to, okTo := nodeByID[e.To]
		if !okFrom || !okTo {
			continue
		}

		// Determine offset for parallel edges
		pair := edgePair{e.From, e.To}
		parallelEdges := edgeGroups[pair]
		edgeIndex := 0
		for i, pe := range parallelEdges {
			if pe.Base == e.Base && pe.Style == e.Style {
				edgeIndex = i
				break
			}
		}
		edgeCount := len(parallelEdges)

		route := layout.EdgeRoute{}
		if edgeIdx < len(routes) {
			route = routes[edgeIdx]
		}
		x1, y1 := layout.EdgePortPoint(from, to, route.Source)
		x2, y2 := layout.EdgePortPoint(to, from, route.Target)

		// Apply offset for parallel edges
		// Spread edges across 0.2-0.8 range perpendicular to edge direction
		if edgeCount > 1 {
			fx := float64(x1)
			fy := float64(y1)
			tx := float64(x2)
			ty := float64(y2)
			dx := tx - fx
			dy := ty - fy
			len := math.Hypot(dx, dy)
			if len > 0 {
				// Perpendicular vector (rotate 90 degrees)
				perpX := -dy / len
				perpY := dx / len

				// Calculate offset: spread evenly across 0.2-0.8 range
				// For 1 edge: offset = 0.5 (center)
				// For 2 edges: offsets = 0.33, 0.67
				// For 3 edges: offsets = 0.25, 0.5, 0.75
				// For 4 edges: offsets = 0.2, 0.4, 0.6, 0.8
				var offsetRatio float64
				if edgeCount == 1 {
					offsetRatio = 0.5
				} else {
					// Spread evenly from 0.2 to 0.8
					offsetRatio = 0.2 + float64(edgeIndex)*(0.6/float64(edgeCount-1))
				}

				// Convert ratio to distance: 0.5 = center, < 0.5 = left, > 0.5 = right
				// Use smaller node dimension as reference for offset distance
				maxOffset := float64(min(from.Width, from.Height, to.Width, to.Height)) / 2.0
				offsetDist := (offsetRatio - 0.5) * maxOffset * 3.0

				x1 = int(float64(x1) + perpX*offsetDist)
				y1 = int(float64(y1) + perpY*offsetDist)
				x2 = int(float64(x2) + perpX*offsetDist)
				y2 = int(float64(y2) + perpY*offsetDist)
			}
		}

		style := edgeStyleFor(e)
		fx := float64(x1)
		fy := float64(y1)
		tx := float64(x2)
		ty := float64(y2)
		if pointInsideNode(from, fx, fy) {
			fx, fy = clipInteriorPointToBoundary(fx, fy, tx, ty, from)
		}
		if pointInsideNode(to, tx, ty) {
			tx, ty = clipInteriorPointToBoundary(tx, ty, fx, fy, to)
		}
		dx := tx - fx
		dy := ty - fy
		// Hold each arrow/line tip an even, visible distance OFF the box it
		// touches. Arrowheads stay BELOW the nodes, so a clean
		// TRANSPARENT gap — not an over-border halo — is what keeps a same-colored
		// arrow from merging into the box it points at. Pull each endpoint back
		// along the edge until it clears its node's border by borderGap measured
		// PERPENDICULAR to that border, so even a shallow, near-grazing approach is
		// pulled back far enough to clear the box; maxPull caps the along-edge slide
		// so a near-parallel edge never detaches far down its own length.
		const borderGap = 5.0 // clear perpendicular gap from the node border
		const maxPull = 16.0  // cap on the along-edge pullback
		// The pull-back slides each endpoint ALONG ITS OWN first/last segment:
		// on a BENT edge the source's segment runs toward the FIRST bend, not
		// toward the target port — pulling along the straight chord dragged
		// the start point off the first leg's line, so a perfectly vertical
		// first leg rendered with a slight tilt (y→E's
		// vertical drop started 2.4px to the side of its bend).
		sdirX, sdirY := dx, dy
		tdirX, tdirY := dx, dy
		if len(route.Bends) > 0 {
			fb := route.Bends[0]
			lb := route.Bends[len(route.Bends)-1]
			if vx, vy := float64(fb.X)-fx, float64(fb.Y)-fy; vx != 0 || vy != 0 {
				sdirX, sdirY = vx, vy
			}
			if vx, vy := tx-float64(lb.X), ty-float64(lb.Y); vx != 0 || vy != 0 {
				tdirX, tdirY = vx, vy
			}
		}
		if d := math.Hypot(sdirX, sdirY); d > 0 {
			fx, fy = pullBackFromBorder(fx, fy, sdirX/d, sdirY/d, route.Source.Side, borderGap, maxPull)
		}
		if d := math.Hypot(tdirX, tdirY); d > 0 {
			tx, ty = pullBackFromBorder(tx, ty, -tdirX/d, -tdirY/d, route.Target.Side, borderGap, maxPull)
		}
		dx = tx - fx
		dy = ty - fy
		chordLen := math.Hypot(dx, dy)
		if chordLen == 0 {
			continue
		}
		ux := dx / chordLen
		uy := dy / chordLen
		headInset := 6.75
		lineStartX := fx
		lineStartY := fy
		lineEndX := tx
		lineEndY := ty
		if style.ArrowStart {
			lineStartX = fx + ux*headInset
			lineStartY = fy + uy*headInset
		}
		if style.ArrowEnd {
			lineEndX = tx - ux*headInset
			lineEndY = ty - uy*headInset
		}
		dash := ""
		if style.Dash != "" {
			dash = fmt.Sprintf(" stroke-dasharray=\"%s\"", style.Dash)
		}
		linecap := ""
		if style.LineCap != "" {
			linecap = fmt.Sprintf(" stroke-linecap=\"%s\"", style.LineCap)
		}
		// Wrap each edge so the frontend can identify it (hover/click/tooltip) and
		// tie the SVG element back to stateView.Edges[edgeIdx]. data-edge-idx is the
		// index into graph.Edges; base/from/to carry the link type and endpoints.
		fmt.Fprintf(&b, "    <g class=\"edge\" data-edge-idx=\"%d\" data-edge-base=\"%s\" data-edge-from=\"%s\" data-edge-to=\"%s\">\n",
			edgeIdx, html.EscapeString(e.Base), html.EscapeString(e.From), html.EscapeString(e.To))
		// Cross-component Expresses/NearTo edges render as stubs: the full edge is
		// kept (hidden, class edge-full) for hover-reveal, and short stub
		// decorations are drawn at each end.
		// The CLASS comes from the layout (Edge.Visibility, the canonical
		// canvas predicate emitted by Generate) and the flat render honours
		// it by DEFAULT (numbered hidden edges):
		// what the canvas hides, the SVG shows as a numbered stub pair —
		// drawing the full chord instead produced border-hugging lines whose
		// connectivity nobody could read.
		stub := e.Visibility == "stubbed"
		var srcStub, tgtStub []layout.Position
		if stub {
			var ok bool
			srcStub, tgtStub, ok = stubsFor(edgeIdx)
			stub = ok
		}
		if stub {
			// Native <title> on the "?" badges: hover to see where the hidden edge leads.
			arrow := "→" // →
			if e.Base == "nearto" {
				arrow = "↔" // ↔
			}
			badgeTitle := stubRelationName(e.Base)
			if e.Label != "" {
				badgeTitle += " (" + e.Label + ")"
			}
			badgeTitle += ": " + stubNodeName(from) + " " + arrow + " " + stubNodeName(to)
			// Hold each stub's node-border point an even gap OFF the border, the
			// same way a normal edge's tip is pulled back (pullBackFromBorder
			// above) — so the dashed line and the arrowhead sit in a clean
			// transparent gap instead of over the node border (otherwise
			// the hidden-edge arrow renders on top of the box outline).
			const stubBorderGap, stubMaxPull = 5.0, 16.0
			sbx, sby := float64(srcStub[0].X), float64(srcStub[0].Y)
			tbx, tby := float64(tgtStub[0].X), float64(tgtStub[0].Y)
			scx, scy := float64(srcStub[1].X), float64(srcStub[1].Y)
			tcx, tcy := float64(tgtStub[1].X), float64(tgtStub[1].Y)
			if ox, oy := scx-sbx, scy-sby; ox != 0 || oy != 0 {
				d := math.Hypot(ox, oy)
				sbx, sby = pullBackFromBorder(sbx, sby, ox/d, oy/d, route.Source.Side, stubBorderGap, stubMaxPull)
			}
			if ox, oy := tcx-tbx, tcy-tby; ox != 0 || oy != 0 {
				d := math.Hypot(ox, oy)
				tbx, tby = pullBackFromBorder(tbx, tby, ox/d, oy/d, route.Target.Side, stubBorderGap, stubMaxPull)
			}
			writeStubEdge(&b, sbx, sby, tbx, tby, scx, scy, tcx, tcy,
				e.Label, badgeTitle, pairCodes[edgeIdx], style)
		} else if len(route.Bends) > 0 {
			// Polyline route (routing PLAN): the body follows the bend
			// waypoints; arrowheads and head insets use the FIRST and LAST
			// segment directions instead of the straight-chord direction.
			bends := route.Bends
			fb := bends[0]
			lb := bends[len(bends)-1]
			sdx, sdy := float64(fb.X)-fx, float64(fb.Y)-fy
			if d := math.Hypot(sdx, sdy); d > 0 {
				sdx, sdy = sdx/d, sdy/d
			}
			edx, edy := tx-float64(lb.X), ty-float64(lb.Y)
			if d := math.Hypot(edx, edy); d > 0 {
				edx, edy = edx/d, edy/d
			}
			px, py := fx, fy
			if style.ArrowStart {
				px, py = fx+sdx*headInset, fy+sdy*headInset
			}
			qx, qy := tx, ty
			if style.ArrowEnd {
				qx, qy = tx-edx*headInset, ty-edy*headInset
			}
			pts := fmt.Sprintf("%.2f,%.2f", px, py)
			for _, bd := range bends {
				pts += fmt.Sprintf(" %d,%d", bd.X, bd.Y)
			}
			pts += fmt.Sprintf(" %.2f,%.2f", qx, qy)
			if style.LineCap == "round" && style.Dash != "" {
				wx, wy := px, py
				total := 0.0
				for _, bd := range bends {
					total += math.Hypot(float64(bd.X)-wx, float64(bd.Y)-wy)
					wx, wy = float64(bd.X), float64(bd.Y)
				}
				total += math.Hypot(qx-wx, qy-wy)
				dash = fmt.Sprintf(" stroke-dasharray=\"%s\"", fitDotDash(style.Dash, total))
			}
			fmt.Fprintf(&b, "    <polyline points=\"%s\" fill=\"none\" stroke=\"%s\" stroke-width=\"%d\"%s%s />\n", pts, style.Stroke, style.Width, linecap, dash)
			if style.ArrowStart {
				writeArrowHead(&b, fx, fy, -sdx, -sdy, style.Stroke, style.Width)
			}
			if style.ArrowEnd {
				writeArrowHead(&b, tx, ty, edx, edy, style.Stroke, style.Width)
			}
			if e.Label != "" {
				writeEdgeLabel(&b, e.Label, int(fx), int(fy), fb.X, fb.Y, style)
			}
		} else {
			if style.LineCap == "round" && style.Dash != "" {
				l := math.Hypot(lineEndX-lineStartX, lineEndY-lineStartY)
				dash = fmt.Sprintf(" stroke-dasharray=\"%s\"", fitDotDash(style.Dash, l))
			}
			fmt.Fprintf(&b, "    <line x1=\"%.2f\" y1=\"%.2f\" x2=\"%.2f\" y2=\"%.2f\" stroke=\"%s\" stroke-width=\"%d\"%s%s />\n", lineStartX, lineStartY, lineEndX, lineEndY, style.Stroke, style.Width, linecap, dash)
			if style.ArrowStart {
				writeArrowHead(&b, fx, fy, -ux, -uy, style.Stroke, style.Width)
			}
			if style.ArrowEnd {
				writeArrowHead(&b, tx, ty, ux, uy, style.Stroke, style.Width)
			}

			if e.Label != "" {
				writeEdgeLabel(&b, e.Label, int(lineStartX), int(lineStartY), int(lineEndX), int(lineEndY), style)
			}
		}
		b.WriteString("    </g>\n")
	}
	b.WriteString("  </g>\n")

	// Render node bodies on top of edges so edge lines are hidden under nodes
	// (but arrowheads near boundaries remain visible above the shadows)
	// Skip container nodes — they rendered as shells in the container-shells layer.
	b.WriteString("  <g id=\"nodes\">\n")
	for _, n := range nodes {
		if n.Container != nil {
			continue
		}
		style := nodeStyleFor(n)
		radius := 6
		if n.Width <= 40 || n.Height <= 40 {
			radius = 4
		}

		// Wrap rect + text in <g> with <title> so browser tooltip triggers
		// anywhere on the node body, not just on the text.
		if n.Tooltip != "" {
			fmt.Fprintf(&b, "    <g>\n")
			fmt.Fprintf(&b, "      <title>%s</title>\n", html.EscapeString(n.Tooltip))
		}

		if len(n.Candidates) > 0 {
			// Undecided (::?…) node: the dashed border cycles through the
			// candidate kinds' colors, primary first — e.g. ::?te → a green
			// (thing) dash, gap, orange (event) dash, gap, … so the box itself
			// reads "undecided between these kinds", echoing the corner
			// swatches. Distinct from container shells' "8 4" dash.
			writeUndecidedBorder(&b, n, radius, style.Fill)
		} else {
			fmt.Fprintf(&b, "    <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"%d\" ry=\"%d\" fill=\"%s\" stroke=\"%s\" stroke-width=\"2\"/>\n", n.X, n.Y, n.Width, n.Height, radius, radius, style.Fill, style.Stroke)
		}

		label := n.Label
		if strings.TrimSpace(label) == "" {
			label = fmt.Sprintf("%s:%s", n.Type, n.ID)
		}
		label = html.EscapeString(label)
		fontSize := 16
		if len(label) > 30 {
			fontSize = 14
		}
		writeNodeLabel(&b, label, n, style, fontSize)

		if len(n.Candidates) > 0 {
			drawCandidateSwatches(&b, n)
		}

		if n.Tooltip != "" {
			fmt.Fprintf(&b, "    </g>\n")
		}
	}
	b.WriteString("  </g>\n")

	b.WriteString("</svg>\n")
	return []byte(b.String()), nil
}

// GenerateWithHints is like Generate but overlays per-node decoration symbols
// from the provided hints map. Nodes absent from hints render normally.
// pkg/ipmsvg has no knowledge of what decorations mean — it only draws them.
func (g *Generator) GenerateWithHints(graph *layout.Graph, hints nodehints.GraphHints) ([]byte, error) {
	svg, err := g.Generate(graph)
	if err != nil {
		return nil, err
	}
	if len(hints) == 0 {
		return svg, nil
	}

	// Decoration value → display symbol.
	symbolFor := map[string]string{
		"zoom-in":  "\u2193", // ↓
		"zoom-out": "\u2191", // ↑
		"collapse": "\u2212", // −
		"expand":   "+",
	}

	// Build a lookup of node geometry by ID.
	nodeByID := make(map[string]layout.Node, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodeByID[n.ID] = n
	}

	const (
		r     = 5 // circle badge radius (expand/collapse)
		sq    = 5 // square badge half-size (zoom-in/zoom-out)
		inset = 2 // px from node rect edge so badge sits fully inside
	)

	// Iterate hints in sorted node-ID order so the overlay block is
	// deterministic (mirroring Generate's node sort); Go map iteration order
	// is randomized and would otherwise produce byte-different SVG.
	nodeIDs := make([]string, 0, len(hints))
	for nodeID := range hints {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)

	var overlays strings.Builder
	for _, nodeID := range nodeIDs {
		nh := hints[nodeID]
		sym, ok := symbolFor[nh.Decoration]
		if !ok {
			continue
		}
		n, ok := nodeByID[nodeID]
		if !ok {
			continue
		}
		cx := n.X + n.Width - r - inset
		cy := n.Y + r + inset
		switch nh.Decoration {
		case "zoom-in", "zoom-out":
			// Rounded square badge for zoom navigation.
			fmt.Fprintf(&overlays,
				"  <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"2\" ry=\"2\" fill=\"#ffffff\" fill-opacity=\"0.5\" stroke=\"#555555\" stroke-opacity=\"0.6\" stroke-width=\"1\"/>\n",
				cx-sq, cy-sq, sq*2, sq*2)
		default:
			// Circle badge for expand/collapse.
			fmt.Fprintf(&overlays,
				"  <circle cx=\"%d\" cy=\"%d\" r=\"%d\" fill=\"#ffffff\" fill-opacity=\"0.5\" stroke=\"#555555\" stroke-opacity=\"0.6\" stroke-width=\"1\"/>\n",
				cx, cy, r)
		}
		fmt.Fprintf(&overlays,
			"  <text x=\"%d\" y=\"%d\" font-size=\"9\" fill=\"#333333\" fill-opacity=\"0.8\" text-anchor=\"middle\" dominant-baseline=\"central\">%s</text>\n",
			cx, cy, sym)
	}

	if overlays.Len() == 0 {
		return svg, nil
	}

	// Insert overlay <text> elements just before </svg>.
	result := strings.Replace(string(svg), "</svg>\n", overlays.String()+"</svg>\n", 1)
	return []byte(result), nil
}

// Render returns a minimal SVG preview for a layout graph.
// Convenience wrapper for Generator.Generate().
func Render(graph *layout.Graph) ([]byte, error) {
	gen, err := NewGenerator()
	if err != nil {
		return nil, err
	}
	return gen.Generate(graph)
}

// GenerateFromIPM generates SVG from IPM model by running layout internally.
// Convenience wrapper that composes layout generation with SVG rendering.
func GenerateFromIPM(doc *model.IpmGraph) ([]byte, error) {
	layoutGraph, err := layout7.Generate(doc)
	if err != nil {
		return nil, fmt.Errorf("layout generation: %w", err)
	}
	gen, err := NewGenerator()
	if err != nil {
		return nil, err
	}
	return gen.Generate(layoutGraph)
}

func writeArrowHead(b *strings.Builder, tipX, tipY, ux, uy float64, stroke string, width int) {
	headLen := 9.0
	headHalf := 4.5
	px := -uy
	py := ux
	baseX := tipX - ux*headLen
	baseY := tipY - uy*headLen
	leftX := baseX + px*headHalf
	leftY := baseY + py*headHalf
	rightX := baseX - px*headHalf
	rightY := baseY - py*headHalf
	notchX := tipX - ux*6.75
	notchY := tipY - uy*6.75
	fmt.Fprintf(b, "    <path d=\"M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f Z\" fill=\"%s\" stroke=\"%s\" stroke-width=\"%d\" stroke-miterlimit=\"10\"/>\n", tipX, tipY, leftX, leftY, notchX, notchY, rightX, rightY, stroke, stroke, width)
}

// writeStubEdge renders the two short "stub" decorations for a cross-component
// edge: one uniform dashed segment leaving each endpoint to a small numbered
// fitDotDash re-spaces a DOT pattern ("0 <gap>") so the dots land
// exactly on BOTH endpoints: the period stretches or shrinks to the
// nearest divisor of the drawn length — a fixed period swallowed the
// last dot short of the far node (the tA --- tB
// near-to missed one dot beside tB).
func fitDotDash(dashSpec string, length float64) string {
	var dot, gap float64
	if n, _ := fmt.Sscanf(dashSpec, "%f %f", &dot, &gap); n != 2 || dot != 0 || gap <= 0 || length <= gap {
		return dashSpec
	}
	k := math.Round(length / gap)
	if k < 1 {
		k = 1
	}
	return fmt.Sprintf("0 %.3f", length/k)
}

// badge marking that the edge continues (the full edge is the hidden edge-full
// sibling, revealed on hover). (fx,fy)→(tx,ty) are the full-edge endpoints;
// (sFadeX,sFadeY) and (tFadeX,tFadeY) are the badge/fade anchor points at the
// source and target ends respectively. Each side is wrapped in <g class="edge-stub">.
func writeStubEdge(b *strings.Builder, fx, fy, tx, ty, sFadeX, sFadeY, tFadeX, tFadeY float64, label, badgeTitle, pairNo string, style edgeStyle) {
	// The stub GEOMETRY (badge points, slot fanning, reach clamping) arrives
	// from the layout (pkg/layout ComputeEdgeStubs — the layout-owns-geometry
	// contract); this writer only paints: one uniform dashed stub from each node
	// to its badge, the badges, the hidden full edge.
	const badgeHalf = 7.0

	dash := ""
	if style.Dash != "" {
		dash = fmt.Sprintf(" stroke-dasharray=\"%s\"", style.Dash)
	}
	linecap := ""
	if style.LineCap != "" {
		linecap = fmt.Sprintf(" stroke-linecap=\"%s\"", style.LineCap)
	}
	// One solid, uniform-color dashed line per stub: no trailing
	// lighter fade near the badge — the numbered chip already signals a hidden edge.
	line := func(x1, y1, x2, y2 float64) {
		fmt.Fprintf(b, "        <line x1=\"%.2f\" y1=\"%.2f\" x2=\"%.2f\" y2=\"%.2f\" stroke=\"%s\" stroke-width=\"%d\"%s%s />\n",
			x1, y1, x2, y2, style.Stroke, style.Width, linecap, dash)
	}

	// Ghost line — the nearly-invisible companion that lets the eye trace
	// where the hidden edge goes without the clutter
	// ("lighter color, minimal weight, in the background,
	// connecting the labels/numbers directly, no bending"). A straight
	// hairline in the edge family's color at low opacity, badge to badge;
	// edges paint before nodes, so it passes UNDER every box.
	fmt.Fprintf(b, "      <line class=\"edge-ghost\" x1=\"%.2f\" y1=\"%.2f\" x2=\"%.2f\" y2=\"%.2f\" stroke=\"%s\" stroke-width=\"1\" stroke-opacity=\"0.22\" />\n",
		sFadeX, sFadeY, tFadeX, tFadeY, style.Stroke)

	// Hidden full edge — drawn BADGE-to-BADGE (rectangle → rectangle), not node to
	// node, so revealing it continues the path the stubs start (node → badge →
	// [full] → badge → node) with no crossing diagonal. Label rides its midpoint.
	// opacity is a presentation ATTRIBUTE: a raw/flat SVG hides the line, while
	// the canvas's CSS reveal rules (.edge-selected .edge-full { opacity: 1 })
	// override it on hover — one render serves both.
	b.WriteString("      <g class=\"edge-full\" opacity=\"0\">\n")
	line(sFadeX, sFadeY, tFadeX, tFadeY)
	if label != "" {
		writeEdgeLabel(b, label, int(sFadeX), int(sFadeY), int(tFadeX), int(tFadeY), style)
	}
	b.WriteString("      </g>\n")

	// Source stub: one dashed line node → badge.
	b.WriteString("      <g class=\"edge-stub\" data-stub-end=\"source\">\n")
	line(fx, fy, sFadeX, sFadeY)
	writeStubBadge(b, sFadeX, sFadeY, badgeHalf, style.Stroke, badgeTitle, pairNo)
	b.WriteString("      </g>\n")

	// Target stub: one dashed line badge → node (+ arrowhead if directed). The
	// approach direction is from the (slotted) badge toward the node.
	b.WriteString("      <g class=\"edge-stub\" data-stub-end=\"target\">\n")
	udx, udy := tx-tFadeX, ty-tFadeY
	if d := math.Hypot(udx, udy); d > 0 {
		udx, udy = udx/d, udy/d
	}
	tEndX, tEndY := tx, ty
	if style.ArrowEnd {
		tEndX, tEndY = tx-udx*6.75, ty-udy*6.75 // leave room for the arrowhead
	}
	line(tFadeX, tFadeY, tEndX, tEndY)
	if style.ArrowEnd {
		writeArrowHead(b, tx, ty, udx, udy, style.Stroke, style.Width)
	}
	writeStubBadge(b, tFadeX, tFadeY, badgeHalf, style.Stroke, badgeTitle, pairNo)
	b.WriteString("      </g>\n")
}

// stubPairAlphabet is the code alphabet for hidden-edge pair badges: digits
// 1-9 first, then capitals, SKIPPING the lookalikes:
// 0 is unused and B(~8), G(~6), I(~1), O(~0), Q(~0), S(~5), Z(~2) are out,
// so no code can be misread as another. ONE sequence numbers every hidden
// edge in the diagram regardless of edge family/color — a code is unique
// across the whole render.
const stubPairAlphabet = "123456789ACDEFHJKLMNPRTUVWXY"

// stubPairCode converts the 1-based ordinal of a hidden edge to its badge
// code: all single-char codes first (28), then two-char codes over the same
// alphabet (784), then three, ... — short codes for the common case, no
// upper limit.
func stubPairCode(n int) string {
	base := len(stubPairAlphabet)
	width, span, start := 1, base, 1
	for n >= start+span {
		start += span
		span *= base
		width++
	}
	idx := n - start
	code := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		code[i] = stubPairAlphabet[idx%base]
		idx /= base
	}
	return string(code)
}

// stubRelationName is the display name for an edge base, used in badge tooltips.
func stubRelationName(base string) string {
	switch base {
	case "expresses":
		return "Expresses"
	case "nearto":
		return "NearTo"
	case "leadsto":
		return "LeadsTo"
	case "partof":
		return "PartOf"
	}
	return base
}

// stubNodeName is the human-readable label for a node in badge tooltips (the
// unwrapped original where available).
func stubNodeName(n layout.Node) string {
	if n.LabelOriginal != "" {
		return n.LabelOriginal
	}
	return n.Label
}

// writeStubBadge draws a small rounded badge centred at (cx,cy) carrying the
// hidden edge's PAIR NUMBER — the same number at both ends, so the reader can
// match where a hidden edge re-emerges. title, if
// non-empty, becomes a native <title> tooltip on the badge (hover to see
// where the hidden edge leads) — the rest of the stub stays tooltip-free.
func writeStubBadge(b *strings.Builder, cx, cy, half float64, stroke, title, pairNo string) {
	if len(pairNo) > 1 {
		half += 2 // two-digit pairs get a slightly wider box
	}
	b.WriteString("        <g class=\"edge-badge\">")
	if title != "" {
		fmt.Fprintf(b, "<title>%s</title>", html.EscapeString(title))
	}
	fmt.Fprintf(b, "<rect x=\"%.2f\" y=\"%.2f\" width=\"%.2f\" height=\"%.2f\" rx=\"3\" fill=\"#ffffff\" stroke=\"%s\" stroke-width=\"1.5\" /><text x=\"%.2f\" y=\"%.2f\" text-anchor=\"middle\" dominant-baseline=\"central\" font-size=\"%.0f\" font-family=\"Helvetica,Arial,sans-serif\" fill=\"%s\">%s</text></g>\n",
		cx-half, cy-half, half*2, half*2, stroke, cx, cy, 11.0, stroke, html.EscapeString(pairNo))
}

// pullBackFromBorder slides an edge endpoint that sits on `side` of a node back
// along the edge — (ox,oy) is the unit edge direction pointing AWAY from that
// node — until it clears the border by gap pixels measured PERPENDICULAR to the
// border. A near-grazing approach would need a long slide to clear the box, so
// the along-edge distance is capped at maxPull (it then clears by a little less
// than gap, but the tip never detaches far down the edge). Keeps arrow tips an
// even distance off every box regardless of the angle they arrive at.
func pullBackFromBorder(x, y, ox, oy float64, side string, gap, maxPull float64) (float64, float64) {
	perp := 1.0 // fraction of the slide that is perpendicular to this border
	switch side {
	case "top", "bottom":
		perp = math.Abs(oy)
	case "left", "right":
		perp = math.Abs(ox)
	}
	pull := maxPull
	if perp > 1e-3 {
		pull = math.Min(gap/perp, maxPull)
	}
	return x + ox*pull, y + oy*pull
}

func pointInsideNode(node layout.Node, x, y float64) bool {
	left := float64(node.X)
	right := float64(node.X + node.Width)
	top := float64(node.Y)
	bottom := float64(node.Y + node.Height)
	return x > left && x < right && y > top && y < bottom
}

func clipInteriorPointToBoundary(interiorX, interiorY, towardX, towardY float64, node layout.Node) (float64, float64) {
	dx := towardX - interiorX
	dy := towardY - interiorY
	if dx == 0 && dy == 0 {
		return interiorX, interiorY
	}

	left := float64(node.X)
	right := float64(node.X + node.Width)
	top := float64(node.Y)
	bottom := float64(node.Y + node.Height)

	bestT := math.Inf(1)
	if dx > 0 {
		bestT = (right - interiorX) / dx
	} else if dx < 0 {
		bestT = (left - interiorX) / dx
	}
	if dy > 0 {
		t := (bottom - interiorY) / dy
		if t >= 0 && t < bestT {
			bestT = t
		}
	} else if dy < 0 {
		t := (top - interiorY) / dy
		if t >= 0 && t < bestT {
			bestT = t
		}
	}
	if math.IsInf(bestT, 1) || bestT < 0 {
		return interiorX, interiorY
	}
	return interiorX + dx*bestT, interiorY + dy*bestT
}

type nodeStyle struct {
	Fill   string
	Stroke string
	Text   string
	Bold   bool
}

type edgeStyle struct {
	Stroke     string
	Width      int
	Dash       string
	LineCap    string // "" | "round" | "square" | "butt"
	ArrowStart bool
	ArrowEnd   bool
}

// containerStyleFor returns renderer styling for a container shell based on the node's type.
func containerStyleFor(n layout.Node) nodeStyle {
	switch n.Type {
	case "event":
		return nodeStyle{Fill: "#fff5eb", Stroke: "#ff8000", Text: "#ff8000", Bold: true}
	default:
		return nodeStyle{Fill: "#f5f5f5", Stroke: "#444444", Text: "#222222", Bold: false}
	}
}

// nodeStyleFor returns renderer styling for a node based on its semantic type.
// Color palette: gl:docs/dev/layout-gen/layout-alg.md#L581-L583 (events), gl:docs/dev/layout-gen/layout-alg.md#L366-L368 (things), gl:docs/dev/layout-gen/layout-alg.md#L581-L583 (concepts)
func nodeStyleFor(n layout.Node) nodeStyle {
	if strings.HasPrefix(n.Type, "boundary") {
		return nodeStyle{Fill: "#ffe6cc", Stroke: "#ff8000", Text: "#ff8000", Bold: true}
	}
	switch n.Type {
	case "event":
		return nodeStyle{Fill: "#ffe6cc", Stroke: "#ff8000", Text: "#ff8000", Bold: true}
	case "thing":
		return nodeStyle{Fill: "#d5e8d4", Stroke: "#82b366", Text: "#009900", Bold: false}
	case "concept":
		return nodeStyle{Fill: "#dae8fc", Stroke: "#6c8ebf", Text: "#3399ff", Bold: false}
	case "unresolved":
		return nodeStyle{Fill: "#ececec", Stroke: "#9e9e9e", Text: "#616161", Bold: false}
	default:
		return nodeStyle{Fill: "#ffffff", Stroke: "#444444", Text: "#222222", Bold: false}
	}
}

// undecidedDash is the length of one candidate-color segment of an undecided
// node's border; undecidedGap is the transparent gap that follows it. Kept
// close to the old single-color "6 4" cue so the border still reads "dashed".
const (
	undecidedDash = 6
	undecidedGap  = 4
)

// writeUndecidedBorder paints the dashed border of an undecided (::?…) node so
// each candidate kind's color takes its own dash(es) around the box, weighted
// toward the primary. The PRIMARY candidate gets TWO adjacent
// dashes per period and every other candidate one, so the most-likely kind
// reads stronger — the doubled dash takes the PRIMARY's color (thing=green,
// event=orange, concept=blue): ::?te → green green orange (2-1), ::?etc →
// orange orange green blue (2-1-1). One base fill rect sits under N stroked rects of identical
// geometry; a per-candidate stroke-dashoffset drops each color into its own
// dash slot(s) so they never paint over one another. Slots = candidates + 1
// (the primary occupies two).
func writeUndecidedBorder(b *strings.Builder, n layout.Node, radius int, fill string) {
	rect := func(stroke, dashArray string, offset int) {
		fmt.Fprintf(b, "    <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"%d\" ry=\"%d\" fill=\"%s\" stroke=\"%s\" stroke-width=\"2\" stroke-dasharray=\"%s\" stroke-dashoffset=\"%d\"/>\n",
			n.X, n.Y, n.Width, n.Height, radius, radius, "none", stroke, dashArray, offset)
	}
	// Base fill, no stroke — the colored dash layers draw the border on top.
	fmt.Fprintf(b, "    <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"%d\" ry=\"%d\" fill=\"%s\" stroke=\"none\"/>\n",
		n.X, n.Y, n.Width, n.Height, radius, radius, fill)
	slot := undecidedDash + undecidedGap
	period := (len(n.Candidates) + 1) * slot // primary takes two slots
	slotIdx := 0                             // next free dash slot
	for i, kind := range n.Candidates {
		stroke := nodeStyleFor(layout.Node{Type: kind}).Stroke
		if i == 0 {
			// Primary: two adjacent dashes (dash, gap, dash), then one long gap
			// spanning the rest of the period.
			tailGap := period - 2*undecidedDash - undecidedGap
			rect(stroke, fmt.Sprintf("%d %d %d %d", undecidedDash, undecidedGap, undecidedDash, tailGap), 0)
			slotIdx = 2
			continue
		}
		// One dash in its own slot; the rest of the period is a single gap.
		rect(stroke, fmt.Sprintf("%d %d", undecidedDash, period-undecidedDash), -slotIdx*slot)
		slotIdx++
	}
}

// drawCandidateSwatches renders one mini node-style chip per candidate kind in
// the node's bottom-right corner, primary (Candidates[0]) leftmost and with a
// heavier border. Each chip reuses that kind's palette so it reads as "this is
// what the node would look like as a …". Called only when len(Candidates) > 0.
func drawCandidateSwatches(b *strings.Builder, n layout.Node) {
	const s, gap, inset = 12, 3, 5 // chip side, gap, corner inset
	k := len(n.Candidates)
	need := k*s + (k-1)*gap + 2*inset
	// Too small for the row of chips → single "?" badge in the corner instead.
	if n.Width < need || n.Height < s+2*inset {
		fmt.Fprintf(b, "    <text x=\"%d\" y=\"%d\" text-anchor=\"end\" dominant-baseline=\"alphabetic\" font-size=\"11\" font-weight=\"bold\" fill=\"#616161\">?</text>\n",
			n.X+n.Width-inset, n.Y+n.Height-inset)
		return
	}
	leftmost := n.X + n.Width - inset - (k*s + (k-1)*gap)
	top := n.Y + n.Height - inset - s
	withLetters := n.Width > 40 && n.Height > 40
	for idx, kind := range n.Candidates {
		cs := nodeStyleFor(layout.Node{Type: kind}) // chip palette = that kind's node style
		x := leftmost + idx*(s+gap)
		sw := 1
		if idx == 0 {
			sw = 2 // primary: heavier, more-visible border
		}
		fmt.Fprintf(b, "    <rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"2\" ry=\"2\" fill=\"%s\" stroke=\"%s\" stroke-width=\"%d\"/>\n",
			x, top, s, s, cs.Fill, cs.Stroke, sw)
		if withLetters && len(kind) > 0 {
			fmt.Fprintf(b, "    <text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" dominant-baseline=\"central\" font-size=\"8\" fill=\"%s\">%s</text>\n",
				float64(x)+float64(s)/2, float64(top)+float64(s)/2, cs.Text, string(kind[0]))
		}
	}
}

// edgeStyleFor returns the ipm edge styling based on semantic type.
// Edge semantics: gl:docs/sst-gamma34.md
func edgeStyleFor(e layout.Edge) edgeStyle {
	key := strings.ToLower(strings.TrimSpace(e.Style))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(e.Base))
	}
	style := edgeStyle{Stroke: "#444444", Width: 3, ArrowEnd: true}
	switch key {
	case "leadsto":
		style.Stroke = "#ff8000"
	case "partof":
		style.Stroke = "#82b366"
	case "expresses":
		style.Stroke = "#6c8ebf"
		style.Dash = "9 6"
	case "nearto":
		style.Stroke = "#999999"
		// Dotted line: zero-length dashes + round caps render as circles
		// of stroke-width diameter; gap of 6 gives a clean dot spacing.
		style.Dash = "0 6"
		style.LineCap = "round"
		style.ArrowEnd = false
	}
	if e.Dir == "undir" {
		style.ArrowEnd = false
		style.ArrowStart = false
	}
	return style
}

func writeNodeLabel(b *strings.Builder, label string, n layout.Node, style nodeStyle, fontSize int) {
	lines := wrapText(label, maxLabelChars(n.Width, fontSize))
	if len(lines) == 0 {
		return
	}
	lineHeight := float64(fontSize) * 1.2
	cx := n.X + n.Width/2
	cy := n.Y + n.Height/2
	startY := float64(cy) - (lineHeight*float64(len(lines)-1))/2
	weight := "normal"
	if style.Bold {
		weight = "bold"
	}

	for i, line := range lines {
		y := startY + lineHeight*float64(i)
		fmt.Fprintf(b, "    <text x=\"%d\" y=\"%.0f\" text-anchor=\"middle\" dominant-baseline=\"middle\" font-family=\"Helvetica,Inter,Segoe UI,Arial\" font-size=\"%d\" font-weight=\"%s\" fill=\"%s\">%s</text>\n", cx, y, fontSize, weight, style.Text, line)
	}
}

func writeEdgeLabel(b *strings.Builder, label string, x1, y1, x2, y2 int, style edgeStyle) {
	text := strings.TrimSpace(label)
	if text == "" {
		return
	}

	midX := (x1 + x2) / 2
	midY := (y1 + y2) / 2
	fontSize := 11

	// Labels up to maxFullLabelLen chars are shown in full.
	// Longer labels are truncated to truncPrefixLen chars + "...".
	const maxFullLabelLen = 6
	const truncPrefixLen = 4
	displayText := text
	fullText := html.EscapeString(text)
	needsTooltip := len([]rune(text)) > maxFullLabelLen

	if needsTooltip {
		runes := []rune(text)
		displayText = strings.TrimRight(string(runes[:truncPrefixLen]), " ") + "..."
	}

	displayTextEscaped := html.EscapeString(displayText)

	// Keep the label readable over the edge with a white halo around the glyphs
	// (paint-order draws the stroke first, behind the fill). Unlike a rectangular
	// background, the halo follows the text outline, so it does not cut a wide
	// horizontal band across a diagonal/near-vertical edge — which would leave the
	// dashes above and below the label visibly offset.
	const labelFont = "Helvetica,Inter,Segoe UI,Arial"
	const halo = `paint-order="stroke" stroke="#ffffff" stroke-width="3" stroke-linejoin="round"`

	if needsTooltip {
		fmt.Fprintf(b, "    <text x=\"%d\" y=\"%d\" text-anchor=\"middle\" dominant-baseline=\"middle\" font-family=\"%s\" font-size=\"%d\" %s fill=\"%s\">\n", midX, midY, labelFont, fontSize, halo, style.Stroke)
		fmt.Fprintf(b, "      <title>%s</title>%s\n", fullText, displayTextEscaped)
		fmt.Fprintf(b, "    </text>\n")
	} else {
		fmt.Fprintf(b, "    <text x=\"%d\" y=\"%d\" text-anchor=\"middle\" dominant-baseline=\"middle\" font-family=\"%s\" font-size=\"%d\" %s fill=\"%s\">%s</text>\n", midX, midY, labelFont, fontSize, halo, style.Stroke, displayTextEscaped)
	}
}

// svgWrapUnit is one wrap token: glue units join the previous one without
// a space (the tail of a hyphen split).
type svgWrapUnit struct {
	s    string
	glue bool
}

// svgWrapUnits splits a label into wrap units: spaces separate words, and
// a word too long for a line splits AFTER its last hyphen that fits (each
// segment keeps its hyphen — kube-controller-manager wraps at the
// hyphens), hard-splitting only when no hyphen helps. KEEP IN SYNC with
// pkg/layout7/size.go's wrapUnits: the box was sized to break exactly
// here.
func svgWrapUnits(label string, maxChars int) []svgWrapUnit {
	var units []svgWrapUnit
	for _, w := range strings.Fields(label) {
		glue := false
		for w != "" {
			r := []rune(w)
			cut := -1
			for i := 0; i < len(r) && i < maxChars; i++ {
				if r[i] == '-' && i+1 < len(r) {
					cut = i + 1
				}
			}
			if len(r) <= maxChars {
				cut = len(r)
			} else if cut == -1 {
				cut = maxChars
			}
			units = append(units, svgWrapUnit{string(r[:cut]), glue})
			w = string(r[cut:])
			glue = true
		}
	}
	return units
}

func wrapText(label string, maxChars int) []string {
	if maxChars <= 0 {
		return []string{label}
	}
	units := svgWrapUnits(label, maxChars)
	if len(units) == 0 {
		return nil
	}
	var lines []string
	var current strings.Builder
	currentLen := 0
	for _, u := range units {
		ulen := len([]rune(u.s))
		sep := 1
		if u.glue {
			sep = 0
		}
		if currentLen == 0 {
			current.WriteString(u.s)
			currentLen = ulen
			continue
		}
		if currentLen+sep+ulen <= maxChars {
			if sep == 1 {
				current.WriteString(" ")
			}
			current.WriteString(u.s)
			currentLen += sep + ulen
			continue
		}
		lines = append(lines, current.String())
		current.Reset()
		current.WriteString(u.s)
		currentLen = ulen
	}
	if currentLen > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func maxLabelChars(width int, fontSize int) int {
	if width <= 0 || fontSize <= 0 {
		return 20
	}
	avgChar := float64(fontSize) * 0.6
	return int(math.Max(6, float64(width)/avgChar))
}
