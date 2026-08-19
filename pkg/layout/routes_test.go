package layout

import "testing"

// A route-less edge that carries a port PIN is not a centre-to-centre edge:
// the zoom canvas synthesizes a thing→shell edge with only ToPort set (the
// shell's left border, near its top). Read as centre→centre, its straight
// line ran from the thing's centre into the shell's centre — through every
// box inside the shell — and the detour pass bent it around them, drawing a
// line into the shell and back out to a border it never meant to leave.
func TestRoutesOfHonoursPinsWithoutARoute(t *testing.T) {
	shell := Node{ID: "shell-1", Type: "event", X: 200, Y: 40, Width: 520, Height: 440,
		Container: &Container{}}
	inner := box("inner", 380, 220) // sits on the centre→centre line
	thing := Node{ID: "t", Type: "thing", X: 40, Y: 76, Width: 120, Height: 60}
	g := &Graph{
		Nodes: []Node{thing, shell, inner},
		Edges: []Edge{{From: "t", To: "shell-1", Base: "partof",
			ToPort: &EdgePort{Side: "left", Position: 0.15}}},
	}
	r := RoutesOf(g)
	if r[0].Target.Side != "left" || r[0].Target.Position != 0.15 {
		t.Fatalf("pinned target read as %+v, want left@0.15", r[0].Target)
	}
	if n := DetourBlockedEdges(g); n != 0 {
		t.Fatalf("a pinned edge whose straight is clear was detoured (%d), route %+v", n, g.Edges[0].Route)
	}
}

// A pin set AFTER the engine routed is materialised into the Route by
// ApplyPortPins — the emitted route is what every downstream reader uses.
func TestApplyPortPinsOverridesAnEmittedRoute(t *testing.T) {
	g := &Graph{
		Nodes: []Node{box("a", 0, 0), box("b", 300, 0)},
		Edges: []Edge{{From: "a", To: "b", Base: "partof",
			Route: &EdgeRouteJSON{
				Source: PortJSON{Side: "right", Position: 0.5},
				Target: PortJSON{Side: "left", Position: 0.5}},
			FromPort: &EdgePort{Side: "right", Position: 0.8}}},
	}
	if n := ApplyPortPins(g); n != 1 {
		t.Fatalf("changed %d ports, want 1", n)
	}
	if got := g.Edges[0].Route.Source; got.Side != "right" || got.Position != 0.8 {
		t.Fatalf("source route %+v, want right@0.8", got)
	}
	if got := g.Edges[0].Route.Target; got.Side != "left" || got.Position != 0.5 {
		t.Fatalf("unpinned target moved: %+v", got)
	}
	// route-less + pinned: a Route is created from the pin
	g2 := &Graph{Nodes: []Node{box("a", 0, 0), box("b", 300, 0)},
		Edges: []Edge{{From: "a", To: "b", ToPort: &EdgePort{Side: "left", Position: 0.15}}}}
	if n := ApplyPortPins(g2); n != 1 || g2.Edges[0].Route == nil || g2.Edges[0].Route.Target.Position != 0.15 {
		t.Fatalf("route-less pin not materialised: n=%d route=%+v", n, g2.Edges[0].Route)
	}
}
