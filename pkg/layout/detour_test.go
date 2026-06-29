package layout

import "testing"

// box is a positioned node at grid-ish coordinates.
func box(id string, x, y int) Node {
	return Node{ID: id, Type: "event", X: x, Y: y, Width: 120, Height: 60}
}

// An edge with a clear straight line must be left exactly as it was: this pass
// only removes edges that cut a box, and touching a clean edge would mean
// second-guessing the engine that placed it.
func TestDetourLeavesClearEdgesAlone(t *testing.T) {
	g := &Graph{
		Nodes: []Node{box("a", 0, 0), box("b", 0, 300)},
		Edges: []Edge{{From: "a", To: "b", Base: "leadsto"}},
	}
	if n := DetourBlockedEdges(g); n != 0 {
		t.Fatalf("rerouted %d edges, want 0", n)
	}
	if g.Edges[0].Route != nil && len(g.Edges[0].Route.Bends) != 0 {
		t.Fatalf("clear edge gained bends: %v", g.Edges[0].Route.Bends)
	}
}

// The case this exists for: a box sits between the endpoints, so the straight
// line cuts it. The result must be a path that cuts nothing.
func TestDetourClearsABlockedEdge(t *testing.T) {
	g := &Graph{
		Nodes: []Node{box("a", 0, 0), box("blocker", 0, 150), box("b", 0, 300)},
		Edges: []Edge{{From: "a", To: "b", Base: "expresses"}},
	}
	// Precondition: the straight line really is blocked, or the test proves nothing.
	routes := RoutesOf(g)
	x1, y1 := EdgePortPoint(g.Nodes[0], g.Nodes[2], routes[0].Source)
	x2, y2 := EdgePortPoint(g.Nodes[2], g.Nodes[0], routes[0].Target)
	if !segmentCutsBox(x1, y1, x2, y2, g.Nodes[1]) {
		t.Fatal("fixture straight line does not cut the blocker")
	}

	if n := DetourBlockedEdges(g); n != 1 {
		t.Fatalf("rerouted %d edges, want 1", n)
	}
	bends := g.Edges[0].Route.Bends
	if len(bends) == 0 {
		t.Fatal("blocked edge got no bends")
	}
	pts := append([][2]int{{x1, y1}}, positionsToPts(bends)...)
	pts = append(pts, [2]int{x2, y2})
	for i := 0; i+1 < len(pts); i++ {
		if segmentCutsBox(pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1], g.Nodes[1]) {
			t.Fatalf("detoured path still cuts the blocker: %v", pts)
		}
	}
}

// Boxed in on every side, there is no clean path. Bending the line anyway would
// produce a route that is both crooked AND still through a box — strictly worse
// to look at than the straight one. Leave it.
func TestDetourLeavesAHopelessEdgeStraight(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			box("a", 0, 0), box("b", 0, 300),
			box("c1", -130, 100), box("c2", 0, 150), box("c3", 130, 100), box("c4", 130, 200),
			box("c5", -130, 200),
		},
		Edges: []Edge{{From: "a", To: "b", Base: "expresses"}},
	}
	before := g.Edges[0].Route
	DetourBlockedEdges(g)
	if g.Edges[0].Route != before && len(g.Edges[0].Route.Bends) > 0 {
		// Any bends written must at least be clean; a blocked detour is a bug.
		routes := RoutesOf(g)
		x1, y1 := EdgePortPoint(g.Nodes[0], g.Nodes[1], routes[0].Source)
		x2, y2 := EdgePortPoint(g.Nodes[1], g.Nodes[0], routes[0].Target)
		pts := append([][2]int{{x1, y1}}, positionsToPts(g.Edges[0].Route.Bends)...)
		pts = append(pts, [2]int{x2, y2})
		obstacles := g.Nodes[2:]
		if pathCuts(pts, obstacles, "a", "b") {
			t.Fatalf("wrote a detour that still cuts a box: %v", pts)
		}
	}
}

// A container shell encloses its members by construction, so it must not count
// as an obstacle — otherwise every edge inside a container looks blocked and
// there is no clean route to be found.
func TestDetourIgnoresContainerShells(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			box("a", 0, 0), box("b", 0, 300),
			{ID: "shell-1", Type: "event", X: -100, Y: -100, Width: 400, Height: 600,
				Container: &Container{ChildNodeIDs: []string{"a", "b"}}},
		},
		Edges: []Edge{{From: "a", To: "b", Base: "leadsto"}},
	}
	if n := DetourBlockedEdges(g); n != 0 {
		t.Fatalf("rerouted %d edges, want 0 — the shell was treated as an obstacle", n)
	}
}
