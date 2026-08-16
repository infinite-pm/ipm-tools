package layoutdiff

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// graph builds a minimal two-node graph with one routed edge. Helpers take
// the fields a test varies and leave the rest fixed, so a test body reads as
// the difference it is about.
func graph(nodes []layout.Node, edges []layout.Edge, w, h int) *layout.Graph {
	return &layout.Graph{
		Version: "test",
		Nodes:   nodes,
		Edges:   edges,
		Meta:    layout.Meta{Bounds: layout.Bounds{Width: w, Height: h}},
	}
}

func node(id, typ, label string, x, y, w, h int) layout.Node {
	return layout.Node{ID: id, Type: typ, Label: label, X: x, Y: y, Width: w, Height: h}
}

func edge(from, to, base, sSide string, sPos float64, tSide string, tPos float64, bends ...layout.Position) layout.Edge {
	return layout.Edge{
		From: from, To: to, Base: base,
		Route: &layout.EdgeRouteJSON{
			Source: layout.PortJSON{Side: sSide, Position: sPos},
			Target: layout.PortJSON{Side: tSide, Position: tPos},
			Bends:  bends,
		},
	}
}

func twoNode(y2 int) *layout.Graph {
	return graph(
		[]layout.Node{
			node("1", "event", "A", 0, 0, 120, 60),
			node("2", "event", "B", 0, y2, 120, 60),
		},
		[]layout.Edge{edge("1", "2", "leadsto", "bottom", 0.5, "top", 0.5)},
		120, y2+60,
	)
}

func kinds(r Report) []string {
	var out []string
	for _, c := range r.Changes {
		out = append(out, c.Kind)
	}
	return out
}

func has(r Report, kind string) bool {
	_, ok := r.Counts[kind]
	return ok
}

func TestIdenticalGraphsProduceNoChanges(t *testing.T) {
	g := twoNode(200)
	rep := Diff(g, twoNode(200), Options{})
	if !rep.Identical() {
		t.Fatalf("identical graphs reported %d changes: %v", len(rep.Changes), kinds(rep))
	}
	if rep.Tier != TierNone || rep.Score != 0 {
		t.Fatalf("tier=%v score=%v, want none/0", rep.Tier, rep.Score)
	}
}

// The whole-canvas shift is the difference that must NOT be reported: one
// node growing pushes everything after it, and without removing that shift
// every diagram reads as "everything moved".
func TestWholeCanvasShiftIsNotAMove(t *testing.T) {
	oldG := twoNode(200)
	newG := twoNode(200)
	for i := range newG.Nodes {
		newG.Nodes[i].X += 40
		newG.Nodes[i].Y += 40
	}
	rep := Diff(oldG, newG, Options{})
	if has(rep, KindNodeMoved) {
		t.Fatalf("a pure translation was reported as movement: %v", kinds(rep))
	}
	if rep.TranslationX != 40 || rep.TranslationY != 40 {
		t.Fatalf("translation = (%d,%d), want (40,40)", rep.TranslationX, rep.TranslationY)
	}
}

func TestMoveRelativeToTheCanvasIsReported(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	// Both nodes shift 40 right; the second ALSO steps 60 down. The canvas
	// shift is (+40, 0) — the dy vote splits {0, +60} and ties toward zero —
	// so only the second node's residual +60 should survive.
	newG.Nodes[0].X += 40
	newG.Nodes[1].X += 40
	newG.Nodes[1].Y += 60
	rep := Diff(oldG, newG, Options{})
	if rep.Counts[KindNodeMoved] != 1 {
		t.Fatalf("want exactly 1 move, got %d: %v", rep.Counts[KindNodeMoved], kinds(rep))
	}
	var c Change
	for _, ch := range rep.Changes {
		if ch.Kind == KindNodeMoved {
			c = ch
		}
	}
	if c.Ref != "#2" || !strings.Contains(c.Detail, "dy=+60") || strings.Contains(c.Detail, "dx=+40") {
		t.Fatalf("move detail = %q on %s, want the residual dy only", c.Detail, c.Ref)
	}
	if rep.Tier != TierGeometry {
		t.Fatalf("tier = %v, want geometry", rep.Tier)
	}
}

// A side flip is what a reader notices first, so it must outrank any amount
// of stepping. This pins the weight ordering, not just the classification.
func TestPortSideFlipOutranksMovement(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	newG.Edges[0].Route.Source.Side = "left"

	movedOld, movedNew := twoNode(200), twoNode(200)
	movedNew.Nodes[1].Y += 200 // ten grid steps

	flip := Diff(oldG, newG, Options{})
	move := Diff(movedOld, movedNew, Options{})

	if flip.Tier != TierStructural {
		t.Fatalf("side flip tier = %v, want structural", flip.Tier)
	}
	if !has(flip, KindPortSide) {
		t.Fatalf("side flip not classified: %v", kinds(flip))
	}
	if flip.Score <= move.Score {
		t.Fatalf("side flip scored %v, a 200px move scored %v — structural must win",
			flip.Score, move.Score)
	}
}

func TestPortSlideOnTheSameSideIsGeometry(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	newG.Edges[0].Route.Target.Position = 0.75
	rep := Diff(oldG, newG, Options{})
	if !has(rep, KindPortSlide) || has(rep, KindPortSide) {
		t.Fatalf("a slide along one side was misclassified: %v", kinds(rep))
	}
	if rep.Tier != TierGeometry {
		t.Fatalf("tier = %v, want geometry", rep.Tier)
	}
	if !strings.Contains(rep.Changes[0].Detail, "target-position=0.75") {
		t.Fatalf("detail = %q, want the rule-DSL vocabulary", rep.Changes[0].Detail)
	}
}

func TestNodeAndEdgeSetChanges(t *testing.T) {
	oldG := twoNode(200)
	newG := twoNode(200)
	newG.Nodes = append(newG.Nodes, node("3", "thing", "C", 300, 0, 120, 60))
	newG.Edges = append(newG.Edges, edge("3", "1", "expresses", "left", 0.5, "right", 0.5))
	rep := Diff(oldG, newG, Options{})
	if rep.Counts[KindNodeAdded] != 1 || rep.Counts[KindEdgeAdded] != 1 {
		t.Fatalf("added node/edge not reported: %v", rep.Counts)
	}

	back := Diff(newG, oldG, Options{})
	if back.Counts[KindNodeRemoved] != 1 || back.Counts[KindEdgeRemoved] != 1 {
		t.Fatalf("the reverse diff must report removals: %v", back.Counts)
	}
}

func TestVisibilityFlipIsStructural(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	newG.Edges[0].Visibility = "stubbed"
	rep := Diff(oldG, newG, Options{})
	if !has(rep, KindVisibility) || rep.Tier != TierStructural {
		t.Fatalf("visibility flip = %v (tier %v)", kinds(rep), rep.Tier)
	}
	if !strings.Contains(rep.Changes[0].Detail, "full → stubbed") {
		t.Fatalf("detail = %q", rep.Changes[0].Detail)
	}
}

func TestBendCountAndBendDriftAreDifferentKinds(t *testing.T) {
	base := twoNode(200)
	base.Edges[0].Route.Bends = []layout.Position{{X: 60, Y: 130}}

	more := twoNode(200)
	more.Edges[0].Route.Bends = []layout.Position{{X: 60, Y: 120}, {X: 80, Y: 140}}
	if rep := Diff(base, more, Options{}); !has(rep, KindBendCount) {
		t.Fatalf("a changed bend COUNT must be structural: %v", kinds(rep))
	}

	drifted := twoNode(200)
	drifted.Edges[0].Route.Bends = []layout.Position{{X: 80, Y: 130}}
	rep := Diff(base, drifted, Options{})
	if !has(rep, KindBendMoved) || has(rep, KindBendCount) {
		t.Fatalf("a moved bend must be geometry: %v", kinds(rep))
	}
}

// An ID that names a different KIND of node is not the same node. Pairing
// them would report a nonsense "moved" instead of the truth.
func TestIDReuseByADifferentKindIsAddRemove(t *testing.T) {
	oldG := graph([]layout.Node{node("1", "event", "A", 0, 0, 120, 60)}, nil, 120, 60)
	newG := graph([]layout.Node{node("1", "thing", "A", 0, 0, 120, 60)}, nil, 120, 60)
	rep := Diff(oldG, newG, Options{})
	if rep.Counts[KindNodeAdded] != 1 || rep.Counts[KindNodeRemoved] != 1 {
		t.Fatalf("kind change on one ID = %v, want an add and a remove", rep.Counts)
	}
}

// A boundary marker renumbers when the component count changes; matching by
// label keeps it one node instead of an add/remove pair.
func TestRenumberedBoundaryIsMatchedByLabel(t *testing.T) {
	oldG := graph([]layout.Node{
		node("1", "event", "A", 0, 100, 120, 60),
		node("9", "boundary", "S", 40, 0, 40, 40),
	}, nil, 120, 160)
	newG := graph([]layout.Node{
		node("1", "event", "A", 0, 100, 120, 60),
		node("12", "boundary", "S", 40, 0, 40, 40),
	}, nil, 120, 160)
	rep := Diff(oldG, newG, Options{})
	if has(rep, KindNodeAdded) || has(rep, KindNodeRemoved) {
		t.Fatalf("a renumbered boundary must not read as add+remove: %v", kinds(rep))
	}
	if !rep.Identical() {
		t.Fatalf("nothing moved, yet: %v", kinds(rep))
	}
}

func TestBoundsChangeIsReportedOnItsOwn(t *testing.T) {
	rep := Diff(twoNode(200), twoNode(260), Options{})
	if !has(rep, KindBoundsChanged) {
		t.Fatalf("a taller canvas was not reported: %v", kinds(rep))
	}
}

// Ranking is the tool's whole purpose: a diagram whose invariants got worse
// must sort before one that only moved.
func TestInvariantRegressionOutranksEverything(t *testing.T) {
	// Two boxes on top of each other: layoutcheck reports a node overlap.
	oldG := graph([]layout.Node{
		node("1", "event", "A", 0, 0, 120, 60),
		node("2", "event", "B", 0, 200, 120, 60),
	}, nil, 120, 260)
	newG := graph([]layout.Node{
		node("1", "event", "A", 0, 0, 120, 60),
		node("2", "event", "B", 10, 10, 120, 60),
	}, nil, 130, 260)

	rep := Diff(oldG, newG, Options{})
	if rep.Tier != TierInvariant {
		t.Fatalf("tier = %v, want invariant (findings: +%v)", rep.Tier, rep.FindingsAdded)
	}
	if len(rep.FindingsAdded) == 0 || !has(rep, KindFindingAdded) {
		t.Fatalf("the new overlap was not reported: %+v", rep.Counts)
	}
	if rep.Score < 1000 {
		t.Fatalf("score = %v, want an overlap to dominate", rep.Score)
	}
}

// A diagram that got BETTER is worth showing and not worth ranking: an audit
// orders by what needs attention.
func TestFixedFindingIsReportedButNotScored(t *testing.T) {
	overlapping := graph([]layout.Node{
		node("1", "event", "A", 0, 0, 120, 60),
		node("2", "event", "B", 10, 10, 120, 60),
	}, nil, 130, 70)
	clean := graph([]layout.Node{
		node("1", "event", "A", 0, 0, 120, 60),
		node("2", "event", "B", 10, 200, 120, 60),
	}, nil, 130, 260)

	rep := Diff(overlapping, clean, Options{})
	if len(rep.FindingsFixed) == 0 {
		t.Fatal("the removed overlap was not reported as fixed")
	}
	for _, c := range rep.Changes {
		if c.Kind == KindFindingFixed && c.Weight != 0 {
			t.Fatalf("a fix scored %v; fixes must not outrank regressions", c.Weight)
		}
	}
}

func TestSkipFindingsLeavesGeometryIntact(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	newG.Nodes[1].Y += 60
	rep := Diff(oldG, newG, Options{SkipFindings: true})
	if !has(rep, KindNodeMoved) {
		t.Fatalf("geometry lost with SkipFindings: %v", kinds(rep))
	}
	if rep.OldFindings != 0 || rep.NewFindings != 0 {
		t.Fatal("SkipFindings must not run the invariant pass")
	}
}

func TestChangesAreSortedBySeverityThenWeight(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	newG.Nodes[1].Y += 60                    // geometry
	newG.Edges[0].Route.Source.Side = "left" // structural
	rep := Diff(oldG, newG, Options{})
	if len(rep.Changes) < 2 {
		t.Fatalf("expected both changes, got %v", kinds(rep))
	}
	for i := 1; i < len(rep.Changes); i++ {
		if rep.Changes[i-1].Tier > rep.Changes[i].Tier {
			t.Fatalf("changes out of severity order: %v", kinds(rep))
		}
	}
}
