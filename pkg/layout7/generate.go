package layout7

import (
	"sort"
	"strconv"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// Version identifies graphs produced by this engine.
const Version = "26.07-v7"

// Options tune the engine for a consumer's needs. The zero value is the
// plain flat layout every renderer got before options existed.
type Options struct {
	// Containers makes a composite event's part-of sub-grid claim its
	// vertical band EXCLUSIVELY: the spine neighbours above and below the
	// composite are pushed clear of the grid's full span instead of tucking
	// beside it in the spine column.
	//
	// OFF (default) is the compact flat layout: v7P8 grows a row gap only
	// where the two neighbourhoods x-OVERLAP, so a sub-grid hanging in its
	// own column costs the spine nothing. That is right for a flat diagram
	// and wrong for one drawn with container SHELLS — a shell is the bbox of
	// {composite ∪ its part-of subtree}, so whatever tucked in beside the
	// grid ends up enclosed by a container it is not a member of.
	//
	// ON is the "reserve shell margin when spacing a composite" rule. It costs
	// vertical space in exact proportion to the sub-grid's height — that
	// room is what makes the shell exclusive.
	Containers bool
	// Shells emits a container box (Container != nil, RenderKind
	// "<type>-container") around every ROOT composite that has part-of
	// members present in the graph, and treats it as a real box: the
	// component's tiling and rings keep their gap from it, other edges route
	// around it, member edges cross it freely. Implies Containers. This is
	// what the zoom canvas draws as an open composite; with it the canvas
	// can lay a state out with the engine alone ("shells in the core").
	Shells bool
	// Anchor is a SOFT arrangement anchor: node id -> box centre from a
	// reference layout (the zoom canvas passes its all-open layout). Wherever
	// the engine has a free choice — the order of wrapped tiles, the flank a
	// tied component rings on when crossings tie — it keeps the arrangement
	// the anchor had, so two states of one document read the same way
	// (wip/zoom-frame-routing/design.md: arrangement stability, "a reader
	// must not be confused by a click"). Positions themselves are still this
	// layout's; nothing is stamped. Nil: the grammar decides alone.
	Anchor map[string][2]int
}

// ShellPad is the air between an open composite's content and its shell.
const ShellPad = 20

// Generate lays out an IPM graph by the v7 principles and returns the
// shared ipm-simple-graph structure (pkg/layout.Graph) — every edge
// carries an explicit route and visibility, so downstream consumers need
// no engine awareness.
//
// Pipeline (see doc.go for the principle map):
//
//	normalize → membership (v7P1/P7) → groups (v7P4/P5) → skeleton (v7P3/P6)
//	→ place (v7P8/P6) → assemble (v7P2) → route (v7P9) → emit
func Generate(doc *model.IpmGraph) (*layout.Graph, error) {
	return GenerateWithOptions(doc, Options{})
}

// GenerateWithOptions is Generate with the engine options applied.
func GenerateWithOptions(doc *model.IpmGraph, opts Options) (*layout.Graph, error) {
	g, err := normalize(doc)
	if err != nil {
		return nil, err
	}
	g.opts = opts
	if opts.Shells {
		g.opts.Containers = true
	}
	m := g.resolveMembership()
	gp := g.buildGroups(m)
	sp := g.buildSkeleton(gp)
	g.addShellNodes(sp)
	g.place(m, gp, sp)
	g.assemble()
	routes := g.route()
	g.stubCorridorDemands(routes)
	// v7P8 §4 demand loop, first slice: route() posted S/E corridor
	// demands (a lane too close to a flow arrowhead) — grow the boundary
	// gaps and re-solve ONCE (demands only grow; the loop is bounded).
	if len(g.sExtra)+len(g.eExtra)+len(g.rowExtra) > 0 {
		g.place(m, gp, sp)
		g.assemble()
		routes = g.route()
	}
	return g.emit(routes), nil
}

func emitType(n *node) (string, []string) {
	if n.boundary {
		return "boundary", nil
	}
	var cands []string
	for _, c := range n.emitCandidates {
		cands = append(cands, strings.ToLower(string(c)))
	}
	t := strings.ToLower(string(n.emitType))
	if t == "" {
		switch n.kind {
		case KindEvent:
			t = "event"
		case KindThing:
			t = "thing"
		case KindConcept:
			t = "concept"
		}
	}
	return t, cands
}

func emitBase(r Rel) string {
	switch r {
	case RelLeadsTo:
		return "leadsto"
	case RelPartOf:
		return "partof"
	case RelExpresses:
		return "expresses"
	}
	return "nearto"
}

// emit converts the placed, routed graph into the shared output structure.
func (g *graph) emit(routes []routed) *layout.Graph {
	out := &layout.Graph{
		Version: Version,
		Nodes:   make([]layout.Node, 0, len(g.nodes)),
		Edges:   make([]layout.Edge, 0, len(g.edges)),
	}

	maxX, maxY := 0, 0
	// shells first: their wrapped nodes get ParentNodeIDs
	parentOf := map[string]string{}
	for _, n := range g.nodes {
		if !n.shell || !n.placed {
			continue
		}
		sn, ids := g.emitShell(n)
		for _, id := range ids {
			parentOf[id] = sn.ID
		}
		out.Nodes = append(out.Nodes, sn)
		if n.x+n.w > maxX {
			maxX = n.x + n.w
		}
		if n.y+n.h > maxY {
			maxY = n.y + n.h
		}
	}
	for _, n := range g.nodes {
		if n.shell {
			continue
		}
		t, cands := emitType(n)
		var parents []string
		if p, ok := parentOf[strconv.Itoa(n.id)]; ok {
			parents = []string{p}
		}
		out.Nodes = append(out.Nodes, layout.Node{
			ID:            strconv.Itoa(n.id),
			Type:          t,
			Label:         n.name,
			Alias:         n.alias,
			Tooltip:       n.tooltip,
			X:             n.x,
			Y:             n.y,
			Width:         n.w,
			Height:        n.h,
			Candidates:    cands,
			ParentNodeIDs: parents,
		})
		if n.x+n.w > maxX {
			maxX = n.x + n.w
		}
		if n.y+n.h > maxY {
			maxY = n.y + n.h
		}
	}

	idOf := func(idx int) string { return strconv.Itoa(g.nodes[idx].id) }
	// visible polylines, for chip clearance (v7P8: a stub chip keeps a
	// visible gap from EDGES too — diagonals included); placed chip zones
	// accumulate so sibling chips never stack on one point
	var chipZones [][4]int
	var visLines [][][2]int
	for _, e := range g.edges {
		r := routes[e.idx]
		if r.stubbed {
			continue
		}
		fn, tn := g.nodes[e.from], g.nodes[e.to]
		sx, sy := layout.EdgePortPoint(g.layoutNode(fn), layout.Node{}, r.src)
		tx, ty := layout.EdgePortPoint(g.layoutNode(tn), layout.Node{}, r.tgt)
		pts := [][2]int{{sx, sy}}
		for _, b := range r.bends {
			pts = append(pts, [2]int{b.X, b.Y})
		}
		visLines = append(visLines, append(pts, [2]int{tx, ty}))
	}
	for _, e := range g.edges {
		r := routes[e.idx]
		dir := "fwd"
		if e.undir {
			dir = "undir"
		}
		route := &layout.EdgeRouteJSON{
			Source: layout.PortJSON{Side: r.src.Side, Position: r.src.Position},
			Target: layout.PortJSON{Side: r.tgt.Side, Position: r.tgt.Position},
			Bends:  r.bends,
		}
		visibility := ""
		if r.stubbed {
			visibility = "stubbed"
			var sPort, tPort layout.EdgePort
			route.SourceStub, sPort = g.stubLine(g.nodes[e.from], g.nodes[e.to], r.src, visLines, &chipZones)
			route.TargetStub, tPort = g.stubLine(g.nodes[e.to], g.nodes[e.from], r.tgt, visLines, &chipZones)
			route.Source = layout.PortJSON{Side: sPort.Side, Position: sPort.Position}
			route.Target = layout.PortJSON{Side: tPort.Side, Position: tPort.Position}
		}
		out.Edges = append(out.Edges, layout.Edge{
			From:       idOf(e.from),
			To:         idOf(e.to),
			Dir:        dir,
			Base:       emitBase(e.rel),
			Style:      emitBase(e.rel),
			Route:      route,
			Visibility: visibility,
		})
	}

	// The canvas holds the EDGES too (v7P8: a lane at the content's edge
	// was drawn ON the border and read as clipped): bounds cover every
	// bend and stub point, and the whole geometry shifts so nothing sits
	// closer to the canvas edge than the margin.
	minX, minY := Margin, Margin
	grow := func(x, y int) {
		if x < minX {
			minX = x
		}
		if y < minY {
			minY = y
		}
		if x > maxX {
			maxX = x
		}
		if y > maxY {
			maxY = y
		}
	}
	for _, e := range out.Edges {
		if e.Route == nil {
			continue
		}
		for _, b := range e.Route.Bends {
			grow(b.X, b.Y)
		}
		for _, p := range e.Route.SourceStub {
			grow(p.X, p.Y)
		}
		for _, p := range e.Route.TargetStub {
			grow(p.X, p.Y)
		}
	}
	if dx, dy := Margin-minX, Margin-minY; dx > 0 || dy > 0 {
		if dx < 0 {
			dx = 0
		}
		if dy < 0 {
			dy = 0
		}
		for i := range out.Nodes {
			out.Nodes[i].X += dx
			out.Nodes[i].Y += dy
		}
		for _, e := range out.Edges {
			if e.Route == nil {
				continue
			}
			for i := range e.Route.Bends {
				e.Route.Bends[i].X += dx
				e.Route.Bends[i].Y += dy
			}
			for i := range e.Route.SourceStub {
				e.Route.SourceStub[i].X += dx
				e.Route.SourceStub[i].Y += dy
			}
			for i := range e.Route.TargetStub {
				e.Route.TargetStub[i].X += dx
				e.Route.TargetStub[i].Y += dy
			}
		}
		maxX += dx
		maxY += dy
	}
	out.Meta = layout.Meta{
		Bounds: layout.Bounds{
			Width:   maxX + Margin,
			Height:  maxY + Margin,
			MarginX: Margin,
			MarginY: Margin,
		},
		Constants: layout.Constants{Grid: GridStep},
	}
	return out
}

// stubCorridorDemands (v7P8 §4: "let's make e4 → E a
// bit longer due to chip 3") runs a DRY stub placement between route
// and the demand re-solve: a chip whose chosen flank faces the node's
// own S/E boundary cap with less than the chip's FULL FORM of room
// posts a boundary demand — the corridor lengthens and the layout
// re-solves once, so the chip lands with its full reach and air
// instead of squeezing beside the cap's arrow.
func (g *graph) stubCorridorDemands(routes []routed) {
	var visLines [][][2]int
	for _, e := range g.edges {
		r := routes[e.idx]
		if r.stubbed {
			continue
		}
		fn, tn := g.nodes[e.from], g.nodes[e.to]
		sx, sy := layout.EdgePortPoint(g.layoutNode(fn), layout.Node{}, r.src)
		tx, ty := layout.EdgePortPoint(g.layoutNode(tn), layout.Node{}, r.tgt)
		pts := [][2]int{{sx, sy}}
		for _, b := range r.bends {
			pts = append(pts, [2]int{b.X, b.Y})
		}
		visLines = append(visLines, append(pts, [2]int{tx, ty}))
	}
	// full chip form: capped reach + number badge + visible gap
	fullForm := Clearance - GridStep/2 + 20 + GridStep/2
	var dryChips [][4]int
	check := func(ni int, port layout.EdgePort, otherI int) {
		n := g.nodes[ni]
		_, chosen := g.stubLine(n, g.nodes[otherI], port, visLines, &dryChips)
		if chosen.Side != "top" && chosen.Side != "bottom" {
			return
		}
		for bi := range g.nodes {
			b := g.nodes[bi]
			if !b.boundary || b.comp != n.comp || !b.placed {
				continue
			}
			if b.x >= n.x+n.w || n.x >= b.x+b.w {
				continue
			}
			if chosen.Side == "bottom" && b.y >= n.y+n.h {
				if gap := b.y - (n.y + n.h); gap < fullForm {
					if need := gridUp(fullForm - gap); need > g.eExtra[n.comp] {
						g.eExtra[n.comp] = need
					}
				}
			}
			if chosen.Side == "top" && b.y+b.h <= n.y {
				if gap := n.y - (b.y + b.h); gap < fullForm {
					if need := gridUp(fullForm - gap); need > g.sExtra[n.comp] {
						g.sExtra[n.comp] = need
					}
				}
			}
		}
	}
	for _, e := range g.edges {
		r := routes[e.idx]
		if !r.stubbed {
			continue
		}
		check(e.from, r.src, e.to)
		check(e.to, r.tgt, e.from)
	}
}

// stubLine is the short numbered stump a hidden edge leaves at an endpoint
// (v7P9). Like every mark, a stub keeps clear of node boxes AND drawn
// edges (v7P8's visible gap — a chip sitting on a diagonal reads as part
// of it): the preferred side is the routed port's, but a side whose gap
// is filled by a neighbour or whose chip zone an edge passes through
// yields to the clearest side, and the stub shrinks to at most half the
// free gap (a chip needs air around it, not just a line).
func (g *graph) stubLine(n *node, other *node, p layout.EdgePort, visLines [][][2]int, chips *[][4]int) ([]layout.Position, layout.EdgePort) {
	freeGap := func(side string) int {
		free := 1 << 30
		for _, o := range g.nodes {
			if o == n || !o.placed {
				continue
			}
			switch side {
			case "left":
				if o.y < n.y+n.h && n.y < o.y+o.h && o.x+o.w <= n.x {
					if d := n.x - (o.x + o.w); d < free {
						free = d
					}
				}
			case "right":
				if o.y < n.y+n.h && n.y < o.y+o.h && o.x >= n.x+n.w {
					if d := o.x - (n.x + n.w); d < free {
						free = d
					}
				}
			case "top":
				if o.x < n.x+n.w && n.x < o.x+o.w && o.y+o.h <= n.y {
					if d := n.y - (o.y + o.h); d < free {
						free = d
					}
				}
			default:
				if o.x < n.x+n.w && n.x < o.x+o.w && o.y >= n.y+n.h {
					if d := o.y - (n.y + n.h); d < free {
						free = d
					}
				}
			}
		}
		return free
	}
	// Sides ordered by FACING preference: the two chips of one hidden
	// edge point TOWARD each other by default, so the
	// eye pairs the numbers across the gap. The routed side joins the
	// ladder next, then the rest — clearance can still override.
	fdx := (other.x + other.w/2) - (n.x + n.w/2)
	fdy := (other.y + other.h/2) - (n.y + n.h/2)
	iabs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	hs, vs := "right", "bottom"
	if fdx < 0 {
		hs = "left"
	}
	if fdy < 0 {
		vs = "top"
	}
	var sides []string
	if iabs(fdx) >= iabs(fdy) {
		sides = []string{hs, vs}
	} else {
		sides = []string{vs, hs}
	}
	for _, sd := range []string{p.Side, "right", "left", "bottom", "top"} {
		dup := false
		for _, have := range sides {
			if have == sd {
				dup = true
				break
			}
		}
		if !dup {
			sides = append(sides, sd)
		}
	}
	zone := func(sd string, pos float64, ln int) [4]int {
		x, y := layout.EdgePortPoint(g.layoutNode(n), layout.Node{},
			layout.EdgePort{Side: sd, Position: pos})
		x0, y0, x1, y1 := x, y, x, y
		switch sd {
		case "left":
			x0 -= ln + 20
		case "right":
			x1 += ln + 20
		case "top":
			y0 -= ln + 20
		default:
			y1 += ln + 20
		}
		return [4]int{x0 - 8, y0 - 8, x1 + 8, y1 + 8}
	}
	chipClear := func(z [4]int) bool {
		// against LINES the zone wears an extra half grid step: an
		// arrowhead landing just past the 8px chip margin still reads
		// as under the chip (the t-shirt chip over the
		// arrow into its below-neighbour)
		m := GridStep / 2
		for _, pl := range visLines {
			for i := 0; i+1 < len(pl); i++ {
				if segIntersectsBox(pl[i], pl[i+1], z[0]-m, z[1]-m, z[2]+m, z[3]+m) {
					return false
				}
			}
		}
		// SIBLING CHIPS too: two hidden edges leaving one node must not
		// stack their chips on one point — the later one steps aside
		for _, c := range *chips {
			if z[0] < c[2] && c[0] < z[2] && z[1] < c[3] && c[1] < z[3] {
				return false
			}
		}
		return true
	}
	posesFor := func(sd string) []float64 {
		base := []float64{0.5, 0.3, 0.7}
		if sd == p.Side {
			base = []float64{p.Position, 0.3, 0.7}
		}
		// chips ORDER by their partner's direction along the side
		// (the e2 chip sits left of the e11 chip when e2 IS
		// left of e11) — otherwise the hidden pair's phantom lines cross
		d := fdx
		if sd == "left" || sd == "right" {
			d = fdy
		}
		want := 0.5
		if d < -GridStep {
			want = 0.25
		} else if d > GridStep {
			want = 0.75
		}
		sort.SliceStable(base, func(a, b int) bool {
			da, db := base[a]-want, base[b]-want
			if da < 0 {
				da = -da
			}
			if db < 0 {
				db = -db
			}
			return da < db
		})
		return base
	}
	bestSide, bestLen, bestClear := p.Side, 0, false
	bestPos := p.Position
	for _, sd := range sides {
		// the chip LINE plus its ~20px number badge plus the visible gap
		// must all fit the flank's free room — half-the-gap sizing parked
		// the badge exactly on the neighbour's border
		l := freeGap(sd) - 20 - GridStep/2
		// open-space reach: one and a half grid steps
		// (the application chip at a full clearance read "too far —
		// should be 25% less")
		if l > Clearance-GridStep/2 {
			l = Clearance - GridStep/2
		}
		// a SQUEEZED flank centres the badge in the gap instead of
		// hugging its own border with an invisible stub
		// (workload resource's chip read "too short, or not
		// existent" in the 40px stack gap)
		if c := freeGap(sd) / 2; l < c && c <= Clearance-GridStep/2 {
			l = c
		}
		found := false
		for _, ps := range posesFor(sd) {
			if chipClear(zone(sd, ps, l)) {
				if l >= 16 { // a readable chip, off every line and chip
					bestSide, bestLen, bestPos = sd, l, ps
					found = true
				} else if !bestClear || l > bestLen {
					bestSide, bestLen, bestPos, bestClear = sd, l, ps, true
				}
				break
			}
		}
		if found {
			break
		}
		if !bestClear && l > bestLen {
			bestSide, bestLen, bestPos = sd, l, posesFor(sd)[0]
		}
	}
	port := layout.EdgePort{Side: bestSide, Position: bestPos}
	*chips = append(*chips, zone(bestSide, bestPos, bestLen))
	x, y := layout.EdgePortPoint(layout.Node{X: n.x, Y: n.y, Width: n.w, Height: n.h}, layout.Node{}, port)
	dx, dy := 0, 0
	switch bestSide {
	case "left":
		dx = -bestLen
	case "right":
		dx = bestLen
	case "top":
		dy = -bestLen
	default:
		dy = bestLen
	}
	return []layout.Position{{X: x, Y: y}, {X: x + dx, Y: y + dy}}, port
}
