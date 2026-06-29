package layout

import "testing"

// Two edges leaving the same side of one node, with their ports assigned the
// opposite way round from their partners, cross each other immediately. After
// ordering they must not — and the SET of slots must be untouched, since the
// engine's spacing is not this pass's to change.
func TestOrderSharedPortsUncrossesAFan(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "src", Type: "concept", X: 0, Y: 100, Width: 120, Height: 60},
			{ID: "hi", Type: "concept", X: 400, Y: 0, Width: 120, Height: 60},
			{ID: "lo", Type: "concept", X: 400, Y: 300, Width: 120, Height: 60},
		},
		// Swapped on purpose: the UPPER partner gets the LOWER slot.
		Edges: []Edge{
			{From: "src", To: "hi", Base: "expresses", Route: &EdgeRouteJSON{
				Source: PortJSON{Side: "right", Position: 0.75},
				Target: PortJSON{Side: "left", Position: 0.5}}},
			{From: "src", To: "lo", Base: "expresses", Route: &EdgeRouteJSON{
				Source: PortJSON{Side: "right", Position: 0.25},
				Target: PortJSON{Side: "left", Position: 0.5}}},
		},
	}
	if crossedAtSrc(g) != true {
		t.Fatal("fixture does not cross to begin with — the test would prove nothing")
	}
	if n := OrderSharedPorts(g); n == 0 {
		t.Fatal("nothing was reordered")
	}
	if crossedAtSrc(g) {
		t.Fatal("edges still cross after ordering")
	}
	// The upper partner now owns the upper slot.
	if g.Edges[0].Route.Source.Position != 0.25 || g.Edges[1].Route.Source.Position != 0.75 {
		t.Fatalf("slots not permuted as expected: hi=%v lo=%v",
			g.Edges[0].Route.Source.Position, g.Edges[1].Route.Source.Position)
	}
}

// A fan already in the right order must be left exactly alone.
func TestOrderSharedPortsLeavesACorrectFanAlone(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "src", Type: "concept", X: 0, Y: 100, Width: 120, Height: 60},
			{ID: "hi", Type: "concept", X: 400, Y: 0, Width: 120, Height: 60},
			{ID: "lo", Type: "concept", X: 400, Y: 300, Width: 120, Height: 60},
		},
		Edges: []Edge{
			{From: "src", To: "hi", Base: "expresses", Route: &EdgeRouteJSON{
				Source: PortJSON{Side: "right", Position: 0.25},
				Target: PortJSON{Side: "left", Position: 0.5}}},
			{From: "src", To: "lo", Base: "expresses", Route: &EdgeRouteJSON{
				Source: PortJSON{Side: "right", Position: 0.75},
				Target: PortJSON{Side: "left", Position: 0.5}}},
		},
	}
	if n := OrderSharedPorts(g); n != 0 {
		t.Fatalf("reordered %d endpoints in an already-correct fan", n)
	}
}

// The pass permutes slots within a side; it must never move an edge to a
// different side, which would change the drawing rather than untangle it.
func TestOrderSharedPortsKeepsSides(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "src", Type: "event", X: 0, Y: 100, Width: 120, Height: 60},
			{ID: "a", Type: "concept", X: 400, Y: 0, Width: 120, Height: 60},
			{ID: "b", Type: "concept", X: 400, Y: 300, Width: 120, Height: 60},
			{ID: "c", Type: "concept", X: -300, Y: 0, Width: 120, Height: 60},
		},
		Edges: []Edge{
			{From: "src", To: "a", Route: &EdgeRouteJSON{
				Source: PortJSON{Side: "right", Position: 0.8}, Target: PortJSON{Side: "left", Position: 0.5}}},
			{From: "src", To: "b", Route: &EdgeRouteJSON{
				Source: PortJSON{Side: "right", Position: 0.2}, Target: PortJSON{Side: "left", Position: 0.5}}},
			{From: "src", To: "c", Route: &EdgeRouteJSON{
				Source: PortJSON{Side: "left", Position: 0.5}, Target: PortJSON{Side: "right", Position: 0.5}}},
		},
	}
	OrderSharedPorts(g)
	want := []string{"right", "right", "left"}
	for i, w := range want {
		if got := g.Edges[i].Route.Source.Side; got != w {
			t.Errorf("edge %d source side %q, want %q", i, got, w)
		}
	}
}

// crossedAtSrc reports whether the two edges' straight lines intersect.
func crossedAtSrc(g *Graph) bool {
	routes := RoutesOf(g)
	pt := func(i int) (int, int, int, int) {
		var from, to Node
		for _, n := range g.Nodes {
			if n.ID == g.Edges[i].From {
				from = n
			}
			if n.ID == g.Edges[i].To {
				to = n
			}
		}
		x1, y1 := EdgePortPoint(from, to, routes[i].Source)
		x2, y2 := EdgePortPoint(to, from, routes[i].Target)
		return x1, y1, x2, y2
	}
	ax1, ay1, ax2, ay2 := pt(0)
	bx1, by1, bx2, by2 := pt(1)
	return segmentsCross(ax1, ay1, ax2, ay2, bx1, by1, bx2, by2)
}
