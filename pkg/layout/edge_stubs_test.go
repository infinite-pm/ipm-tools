package layout

import (
	"math"
	"testing"
)

// The facing-border contract: a stub's chip hangs off
// the border that FACES the partner — for a vertically stacked tie with a
// CLEAR column between them, the SOURCE chip sits just above its node's top
// and the TARGET chip just below the partner's bottom (their closest borders),
// never out a side reading as a corner start. (When a node blocks the column
// the chips move to a symmetric clear side instead — see
// TestStubChipsMoveToSymmetricClearSide.)
func TestStubChipHangsOffFacingBorder(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			// 'a' below, partner 'p' directly above, same column, nothing
			// between — the straight chip→chip ghost is clear, so the chips
			// hang off the facing borders (a's top, p's bottom).
			{ID: "a", Type: "event", X: 280, Y: 580, Width: 120, Height: 60},
			{ID: "p", Type: "concept", X: 280, Y: 120, Width: 120, Height: 60},
		},
		Edges: []Edge{
			{From: "a", To: "p", Base: "expresses", Style: "expresses", Visibility: "stubbed", Deferred: true},
		},
	}
	routes := RoutesOf(g)
	stubs := ComputeEdgeStubs(g, routes)
	st, ok := stubs[0]
	if !ok {
		t.Fatalf("edge 0 should have stub geometry")
	}
	// Source (a, partner above): near-point on a's TOP border, chip above it.
	if ny := st.Source[0].Y; ny != 580 {
		t.Errorf("source stub should leave a's top border (y=580), got y=%d", ny)
	}
	if cy := st.Source[1].Y; cy >= 580 {
		t.Errorf("source chip should sit ABOVE a (y<580), got y=%d", cy)
	}
	// Target (p, partner below): near-point on p's BOTTOM border, chip below.
	if ny := st.Target[0].Y; ny != 180 {
		t.Errorf("target stub should leave p's bottom border (y=180), got y=%d", ny)
	}
	if cy := st.Target[1].Y; cy <= 180 {
		t.Errorf("target chip should sit BELOW p (y>180), got y=%d", cy)
	}
	// The stub line must be visible: chip at least 16px from its border point.
	if d := math.Abs(float64(st.Source[1].Y - st.Source[0].Y)); d < 16 {
		t.Errorf("source stub length %.1f < 16px — chip crowds the node", d)
	}
}

// When a node blocks the column between two stacked ties, the straight
// chip→chip ghost would pierce it. Both chips move to a shared clear SIDE
// (symmetric: source-left ⇒ target-left) so the hidden body runs in a clear
// lane beside the obstacle. 'mid' blocks the column, 'right'
// blocks the right side, so the only clear lane is the LEFT.
func TestStubChipsMoveToSymmetricClearSide(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "a", Type: "event", X: 280, Y: 580, Width: 120, Height: 60},
			{ID: "mid", Type: "event", X: 300, Y: 360, Width: 80, Height: 40},
			{ID: "right", Type: "event", X: 460, Y: 350, Width: 120, Height: 60},
			{ID: "p", Type: "concept", X: 280, Y: 120, Width: 120, Height: 60},
		},
		Edges: []Edge{
			{From: "a", To: "p", Base: "expresses", Style: "expresses", Visibility: "stubbed", Deferred: true},
		},
	}
	routes := RoutesOf(g)
	stubs := ComputeEdgeStubs(g, routes)
	st, ok := stubs[0]
	if !ok {
		t.Fatalf("edge 0 should have stub geometry")
	}
	// Both chips left of the column (x < a.X=280): symmetric, ghost on the left.
	if cx := st.Source[1].X; cx >= 280 {
		t.Errorf("source chip should move LEFT of the column (x<280), got x=%d", cx)
	}
	if cx := st.Target[1].X; cx >= 280 {
		t.Errorf("target chip should move LEFT too (symmetric, x<280), got x=%d", cx)
	}
	// The ghost (source chip → target chip) must clear 'mid' (x300-380, y360-400).
	sx, sy := float64(st.Source[1].X), float64(st.Source[1].Y)
	tx, ty := float64(st.Target[1].X), float64(st.Target[1].Y)
	for i := 1; i < 24; i++ {
		f := float64(i) / 24
		px, py := sx+(tx-sx)*f, sy+(ty-sy)*f
		if 300 < px && px < 380 && 360 < py && py < 400 {
			t.Fatalf("ghost line should not pierce 'mid', but passes through (%.0f,%.0f)", px, py)
		}
	}
}

// A chip must not sit on a VISIBLE edge's line
// ("normal edge should not collide with hidden") — it ladders or re-ports
// until the spot is clear of passing edges too.
func TestStubChipAvoidsVisibleEdges(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "a", Type: "event", X: 40, Y: 40, Width: 120, Height: 60},
			{ID: "mid", Type: "event", X: 340, Y: 220, Width: 120, Height: 60},
			{ID: "b", Type: "concept", X: 640, Y: 440, Width: 120, Height: 60},
			// A visible vertical leadsto chain crossing the stub region just
			// right-below of a, where the default chip spot lands.
			{ID: "v1", Type: "event", X: 150, Y: 140, Width: 120, Height: 60},
			{ID: "v2", Type: "event", X: 150, Y: 320, Width: 120, Height: 60},
		},
		Edges: []Edge{
			{From: "a", To: "b", Base: "expresses", Style: "expresses", Visibility: "stubbed", Deferred: true},
			{From: "v1", To: "v2", Base: "leadsto", Style: "leadsto"},
		},
	}
	routes := RoutesOf(g)
	stubs := ComputeEdgeStubs(g, routes)
	st, ok := stubs[0]
	if !ok {
		t.Fatalf("edge 0 should have stub geometry")
	}
	// The v1->v2 visible line is vertical at x=210, y in [200,320].
	cx, cy := float64(st.Source[1].X), float64(st.Source[1].Y)
	if cx+stubChipHalf > 210-2 && cx-stubChipHalf < 210+2 && cy+stubChipHalf > 200 && cy-stubChipHalf < 320 {
		t.Errorf("source chip (%v,%v) sits on the visible v1->v2 line", cx, cy)
	}
}

// A chip never sits BEHIND its node relative to the partner
// ("bottom and crossing source node itself is too bad — it must
// be right top or top right corner"): when the assigned port faces away
// from the partner, the edge re-ports to a side facing it, corner-biased.
func TestStubChipNeverBehindTheNode(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			// Partner up-right; mid blocks the chord so the edge stays hidden.
			{ID: "w", Type: "thing", X: 40, Y: 400, Width: 120, Height: 60},
			{ID: "mid", Type: "event", X: 240, Y: 220, Width: 120, Height: 60},
			{ID: "p", Type: "concept", X: 520, Y: 40, Width: 120, Height: 60},
		},
		Edges: []Edge{
			// Force a BOTTOM port — the wrong side for an up-right partner.
			{From: "w", To: "p", Base: "expresses", Style: "expresses", Visibility: "stubbed", Deferred: true,
				FromPort: &EdgePort{Side: "bottom", Position: 0.5}},
		},
	}
	routes := RoutesOf(g)
	stubs := ComputeEdgeStubs(g, routes)
	st, ok := stubs[0]
	if !ok {
		t.Fatalf("edge 0 should have stub geometry")
	}
	cx, cy := st.Source[1].X, st.Source[1].Y
	if cy > 400+60 {
		t.Errorf("source chip (%d,%d) hangs below the node while the partner is up-right — the ghost would cross the node", cx, cy)
	}
	if cx < 40 {
		t.Errorf("source chip (%d,%d) sits left of the node while the partner is up-right", cx, cy)
	}
}

// A chip whose default spot lands on a foreign box ladders OUTWARD along the
// chord (a longer stub line), not sideways.
func TestStubChipLaddersPastABox(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "a", Type: "event", X: 40, Y: 40, Width: 120, Height: 60},
			// Blocker sits right where the default chip spot would land on
			// the horizontal chord out of a's right side.
			{ID: "blk", Type: "event", X: 200, Y: 50, Width: 60, Height: 40},
			{ID: "b", Type: "concept", X: 940, Y: 40, Width: 120, Height: 60},
		},
		Edges: []Edge{
			{From: "a", To: "b", Base: "expresses", Style: "expresses", Visibility: "stubbed", Deferred: true},
		},
	}
	routes := RoutesOf(g)
	stubs := ComputeEdgeStubs(g, routes)
	st, ok := stubs[0]
	if !ok {
		t.Fatalf("edge 0 should have stub geometry")
	}
	cx, cy := float64(st.Source[1].X), float64(st.Source[1].Y)
	if cx+stubChipHalf > 200 && 260 > cx-stubChipHalf && cy+stubChipHalf > 50 && 90 > cy-stubChipHalf {
		t.Errorf("source chip (%v,%v) sits on the blocker box — must ladder past it", cx, cy)
	}
}

// Two stub edges of DIFFERENT types sharing one node side get their own
// separated chips, grouped/ordered by type ("each type of
// edge must have its own chip or stack of chips" — not interleaved/crowded).
func TestStubChipsSeparateByType(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "x", Type: "event", X: 40, Y: 80, Width: 120, Height: 60},
			{ID: "pe", Type: "concept", X: 700, Y: 20, Width: 120, Height: 60},
			{ID: "pn", Type: "thing", X: 700, Y: 200, Width: 120, Height: 60},
		},
		Edges: []Edge{
			{From: "x", To: "pe", Base: "expresses", Style: "expresses", Visibility: "stubbed"},
			{From: "x", To: "pn", Base: "nearto", Style: "nearto", Visibility: "stubbed"},
		},
	}
	routes := RoutesOf(g)
	stubs := ComputeEdgeStubs(g, routes)
	exp, ok0 := stubs[0]
	near, ok1 := stubs[1]
	if !ok0 || !ok1 {
		t.Fatalf("both stub edges need geometry")
	}
	// Both leave x on the same (right) side.
	if routes[0].Source.Side != routes[1].Source.Side {
		t.Fatalf("both stubs should share a side, got %s and %s", routes[0].Source.Side, routes[1].Source.Side)
	}
	ec, nc := exp.Source[1], near.Source[1]
	// Distinct, separated chips — never crammed on top of each other.
	if d := math.Hypot(float64(ec.X-nc.X), float64(ec.Y-nc.Y)); d < 2*stubChipHalf {
		t.Errorf("mixed-type chips overlap (%v vs %v), gap %.0f", ec, nc, d)
	}
	// Type order is deterministic: expresses sits before (above) near-to.
	if ec.Y >= nc.Y {
		t.Errorf("expresses chip (y=%d) should sit above the near-to chip (y=%d)", ec.Y, nc.Y)
	}
}
