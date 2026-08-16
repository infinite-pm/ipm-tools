package layout

// routeShrink: obstacle boxes shrink by this much per side before
// intersection tests, so an edge running along a border is not "through"
// the node.
const routeShrink = 2

// RoutesOf returns the edge routes of a graph: the emitted ones (every
// engine output carries explicit routes). A route-less edge — a
// hand-written layout.json, or one a consumer synthesized — falls back to
// its FromPort/ToPort pins (the Edge contract: a pin overrides the
// auto-computed port), else to centre ports resolved by geometry. A pin
// set AFTER the route was emitted is not read here — see ApplyPortPins.
func RoutesOf(g *Graph) []EdgeRoute {
	routes := make([]EdgeRoute, len(g.Edges))
	for i := range g.Edges {
		r := g.Edges[i].Route
		if r == nil {
			routes[i] = EdgeRoute{
				Source: EdgePort{Side: "center", Position: 0.5},
				Target: EdgePort{Side: "center", Position: 0.5},
			}
			if p := g.Edges[i].FromPort; p != nil {
				routes[i].Source = *p
			}
			if p := g.Edges[i].ToPort; p != nil {
				routes[i].Target = *p
			}
			continue
		}
		routes[i] = EdgeRoute{
			Source: EdgePort{Side: r.Source.Side, Position: r.Source.Position},
			Target: EdgePort{Side: r.Target.Side, Position: r.Target.Position},
			Bends:  r.Bends,
		}
	}
	return routes
}

// segmentCutsBox tests the segment against the box shrunk by routeShrink per
// side (grazing a border is not a cut) — same tolerance as the sweep metric.
func segmentCutsBox(x1, y1, x2, y2 int, n Node) bool {
	bx, by := n.X+routeShrink, n.Y+routeShrink
	bw, bh := n.Width-2*routeShrink, n.Height-2*routeShrink
	if bw <= 0 || bh <= 0 {
		return false
	}
	// Liang–Barsky style clipping on the parametric segment.
	dx, dy := float64(x2-x1), float64(y2-y1)
	t0, t1 := 0.0, 1.0
	clip := func(pp, qq float64) bool {
		if pp == 0 {
			return qq >= 0
		}
		r := qq / pp
		if pp < 0 {
			if r > t1 {
				return false
			}
			if r > t0 {
				t0 = r
			}
		} else {
			if r < t0 {
				return false
			}
			if r < t1 {
				t1 = r
			}
		}
		return true
	}
	if !clip(-dx, float64(x1-bx)) || !clip(dx, float64(bx+bw-x1)) ||
		!clip(-dy, float64(y1-by)) || !clip(dy, float64(by+bh-y1)) {
		return false
	}
	return t0 < t1
}

// segmentsCross reports a proper crossing between two segments (shared
// endpoints do not count — fan legs meet at ports legitimately).
func segmentsCross(ax1, ay1, ax2, ay2, bx1, by1, bx2, by2 int) bool {
	if (ax1 == bx1 && ay1 == by1) || (ax1 == bx2 && ay1 == by2) ||
		(ax2 == bx1 && ay2 == by1) || (ax2 == bx2 && ay2 == by2) {
		return false
	}
	o := func(px, py, qx, qy, rx, ry int) int {
		v := (qx-px)*(ry-py) - (qy-py)*(rx-px)
		switch {
		case v > 0:
			return 1
		case v < 0:
			return -1
		}
		return 0
	}
	o1 := o(ax1, ay1, ax2, ay2, bx1, by1)
	o2 := o(ax1, ay1, ax2, ay2, bx2, by2)
	o3 := o(bx1, by1, bx2, by2, ax1, ay1)
	o4 := o(bx1, by1, bx2, by2, ax2, ay2)
	return o1 != o2 && o3 != o4 && o1 != 0 && o2 != 0 && o3 != 0 && o4 != 0
}

// ApplyPortPins materialises every FromPort/ToPort pin into the edge's
// emitted Route, creating the Route (from RoutesOf) when there is none, and
// returns how many ports it changed. For a consumer that pins ports AFTER
// the engine routed — the zoom canvas pins a lifted edge to the row its
// hidden child sits on, spreads a shell's fan, and synthesizes shell edges
// with only a pin — this is what makes the pin real: every downstream
// reader (OrderSharedPorts, DetourBlockedEdges, the SVG renderer) reads the
// Route, so an un-materialised pin is silently a centre port. Bends are
// left as they are; a consumer that moved nodes drops them first.
func ApplyPortPins(g *Graph) int {
	if g == nil {
		return 0
	}
	routes := RoutesOf(g)
	changed := 0
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.FromPort == nil && e.ToPort == nil {
			continue
		}
		if e.Route == nil {
			e.Route = &EdgeRouteJSON{
				Source: PortJSON{Side: routes[i].Source.Side, Position: routes[i].Source.Position},
				Target: PortJSON{Side: routes[i].Target.Side, Position: routes[i].Target.Position},
			}
			changed++
			continue
		}
		if p := e.FromPort; p != nil && (e.Route.Source.Side != p.Side || e.Route.Source.Position != p.Position) {
			e.Route.Source = PortJSON{Side: p.Side, Position: p.Position}
			changed++
		}
		if p := e.ToPort; p != nil && (e.Route.Target.Side != p.Side || e.Route.Target.Position != p.Position) {
			e.Route.Target = PortJSON{Side: p.Side, Position: p.Position}
			changed++
		}
	}
	return changed
}
