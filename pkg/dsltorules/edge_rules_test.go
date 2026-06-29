package dsltorules

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

func TestCenteredBetweenRule(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "pre", Type: "event", X: 40, Y: 120, Width: 120, Height: 60},  // cy 150
			{ID: "a", Type: "event", X: 220, Y: 260, Width: 120, Height: 60},   // cy 290
			{ID: "sh", Type: "thing", X: 400, Y: 190, Width: 120, Height: 60},  // cy 220 = midpoint
			{ID: "off", Type: "thing", X: 400, Y: 260, Width: 120, Height: 60}, // cy 290 — on a's row
		},
	}
	pass, err := Parse(1, "#sh is vertically-centered-between #pre,#a")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := pass.Apply(layout); err != nil {
		t.Fatalf("sh sits at the midpoint; rule should pass, got %v", err)
	}
	fail, _ := Parse(1, "#off is vertically-centered-between #pre,#a")
	if err := fail.Apply(layout); err == nil {
		t.Fatalf("off sits on a's row, not the midpoint; rule should fail")
	}
}

func TestEdgeMinSlopeRule(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
		Edges: []layouttest.Edge{
			{From: "a", To: "b", Points: []layouttest.Point{{X: 0, Y: 0}, {X: 360, Y: 40}}},   // slope 0.111
			{From: "a", To: "c", Points: []layouttest.Point{{X: 0, Y: 0}, {X: 360, Y: 120}}},  // slope 0.333
			{From: "a", To: "d", Points: []layouttest.Point{{X: 100, Y: 0}, {X: 100, Y: 80}}}, // vertical
		},
	}
	shallow, err := Parse(1, "edge #a,#b has min-slope=0.2")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := shallow.Apply(layout); err == nil {
		t.Fatalf("slope 0.111 should fail min-slope=0.2")
	}
	steep, _ := Parse(1, "edge #a,#c has min-slope=0.2")
	if err := steep.Apply(layout); err != nil {
		t.Fatalf("slope 0.333 should pass, got %v", err)
	}
	vert, _ := Parse(1, "edge #a,#d has min-slope=0.2")
	if err := vert.Apply(layout); err != nil {
		t.Fatalf("vertical edge should always pass, got %v", err)
	}
	if _, err := Parse(1, "edge #a,#b has min-slope=-1"); err == nil {
		t.Fatalf("negative slope should be rejected at parse time")
	}
}

func TestEdgeStraightRule(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		Edges: []layouttest.Edge{
			{From: "a", To: "b", Points: []layouttest.Point{{X: 160, Y: 150}, {X: 220, Y: 150}}},
			{From: "a", To: "c", Points: []layouttest.Point{{X: 160, Y: 150}, {X: 220, Y: 190}}},
		},
	}
	h, err := Parse(1, "edge #a,#b is horizontal")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := h.Apply(layout); err != nil {
		t.Fatalf("a->b is horizontal, got %v", err)
	}
	tilted, _ := Parse(1, "edge #a,#c is horizontal")
	if err := tilted.Apply(layout); err == nil {
		t.Fatalf("a->c is tilted; horizontal assertion should fail")
	}
	v, err := Parse(1, "edge #a,#b is vertical")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := v.Apply(layout); err == nil {
		t.Fatalf("a->b is horizontal; vertical assertion should fail")
	}
	if _, err := Parse(1, "edge #a,#b is diagonal"); err == nil {
		t.Fatalf("invalid axis should be rejected at parse time")
	}
}

func TestNodeNoStraddleRule(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "a", Type: "event", X: 0, Y: 0, Width: 120, Height: 60},
			{ID: "b", Type: "event", X: 0, Y: 400, Width: 120, Height: 60},
			{ID: "mid", Type: "concept", X: 30, Y: 200, Width: 120, Height: 60},   // on the a->b line
			{ID: "side", Type: "concept", X: 400, Y: 200, Width: 120, Height: 60}, // clear of it
		},
		Edges: []layouttest.Edge{
			{From: "a", To: "b", Points: []layouttest.Point{{X: 60, Y: 60}, {X: 60, Y: 400}}},
		},
	}
	bad, err := Parse(1, "node #mid does not straddle edge #a,#b")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := bad.Apply(layout); err == nil {
		t.Fatalf("mid lies on the a->b segment; rule should fail")
	}
	good, err := Parse(1, "node #side does not straddle edge #a,#b")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := good.Apply(layout); err != nil {
		t.Fatalf("side is clear of the edge; rule should pass, got %v", err)
	}
}

func TestNodeEdgeGapRule(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "a", Type: "event", X: 0, Y: 0, Width: 120, Height: 60},
			{ID: "b", Type: "event", X: 0, Y: 400, Width: 120, Height: 60},
			{ID: "near", Type: "event", X: 70, Y: 200, Width: 120, Height: 60}, // 10px right of the a->b line
			{ID: "far", Type: "event", X: 200, Y: 200, Width: 120, Height: 60}, // 140px clear
		},
		Edges: []layouttest.Edge{
			{From: "a", To: "b", Points: []layouttest.Point{{X: 60, Y: 60}, {X: 60, Y: 400}}},
		},
	}
	bad, err := Parse(1, "node #near keeps gap=25 from edge #a,#b")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := bad.Apply(layout); err == nil {
		t.Fatalf("edge passes 10px from near; gap=25 rule should fail")
	}
	good, err := Parse(1, "node #near keeps gap=5 from edge #a,#b")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := good.Apply(layout); err != nil {
		t.Fatalf("10px clearance satisfies gap=5, got %v", err)
	}
	farGood, err := Parse(1, "node #far keeps gap=25 from edge #a,#b")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := farGood.Apply(layout); err != nil {
		t.Fatalf("far is 140px clear; rule should pass, got %v", err)
	}
	if _, err := Parse(1, "node #far keeps gap=0 from edge #a,#b"); err == nil {
		t.Fatalf("gap=0 must be rejected")
	}
}

func TestEdgeEndpointPositionRule(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "a", Type: "event"}, {ID: "b", Type: "event"},
		},
		Edges: []layouttest.Edge{
			{From: "a", To: "b", SourcePosition: 0.5, TargetPosition: 0.35},
		},
	}
	pass, err := Parse(1, "edge #a,#b has target-position=0.35")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := pass.Apply(layout); err != nil {
		t.Fatalf("target-position=0.35 should pass, got %v", err)
	}
	bad, _ := Parse(1, "edge #a,#b has target-position=0.65")
	if err := bad.Apply(layout); err == nil {
		t.Fatalf("target-position=0.65 should fail (actual 0.35)")
	}
	src, _ := Parse(1, "edge #a,#b has source-position=0.5")
	if err := src.Apply(layout); err != nil {
		t.Fatalf("source-position=0.5 should pass, got %v", err)
	}
	if _, err := Parse(1, "edge #a,#b has target-position=1.5"); err == nil {
		t.Fatalf("out-of-range position should be rejected at parse time")
	}
}

func TestEdgeEndpointSideRule(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "a", Type: "event", X: 0, Y: 0, Width: 120, Height: 60},
			{ID: "b", Type: "event", X: 90, Y: 200, Width: 120, Height: 60},
		},
		Edges: []layouttest.Edge{
			{From: "a", To: "b", Style: "leadsto", SourceSide: "bottom", TargetSide: "top"},
		},
	}

	rule, err := Parse(1, "edge #a,#b has target-side=top")
	if err != nil {
		t.Fatalf("parse target-side: %v", err)
	}
	if err := rule.Apply(layout); err != nil {
		t.Fatalf("target-side=top should pass, got %v", err)
	}

	bad, err := Parse(1, "edge #a,#b has target-side=left")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := bad.Apply(layout); err == nil {
		t.Fatalf("target-side=left should fail (actual top)")
	}

	src, err := Parse(1, "edge #a,#b has source-side=bottom")
	if err != nil {
		t.Fatalf("parse source-side: %v", err)
	}
	if err := src.Apply(layout); err != nil {
		t.Fatalf("source-side=bottom should pass, got %v", err)
	}

	if _, err := Parse(1, "edge #a,#b has target-side=sideways"); err == nil {
		t.Fatalf("invalid side should be rejected at parse time")
	}
}

func TestEdgeNoCrossRule(t *testing.T) {
	// Two edges that strictly cross: (0,0)->(100,100) and (0,100)->(100,0).
	crossing := &layouttest.Layout{
		Nodes: []layouttest.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
		Edges: []layouttest.Edge{
			{From: "a", To: "b", Points: []layouttest.Point{{X: 0, Y: 0}, {X: 100, Y: 100}}},
			{From: "c", To: "d", Points: []layouttest.Point{{X: 0, Y: 100}, {X: 100, Y: 0}}},
		},
	}
	rule, err := Parse(1, "edge #a,#b does not cross edge #c,#d")
	if err != nil {
		t.Fatalf("parse no-cross: %v", err)
	}
	if err := rule.Apply(crossing); err == nil {
		t.Fatalf("crossing edges should fail the no-cross rule")
	}

	// Parallel, non-crossing.
	parallel := &layouttest.Layout{
		Nodes: []layouttest.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
		Edges: []layouttest.Edge{
			{From: "a", To: "b", Points: []layouttest.Point{{X: 0, Y: 0}, {X: 0, Y: 100}}},
			{From: "c", To: "d", Points: []layouttest.Point{{X: 50, Y: 0}, {X: 50, Y: 100}}},
		},
	}
	if err := rule.Apply(parallel); err != nil {
		t.Fatalf("parallel edges should pass the no-cross rule, got %v", err)
	}

	// Sharing a common endpoint (a fork) must NOT count as a crossing.
	shared := &layouttest.Layout{
		Nodes: []layouttest.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
		Edges: []layouttest.Edge{
			{From: "a", To: "b", Points: []layouttest.Point{{X: 0, Y: 0}, {X: 100, Y: 100}}},
			{From: "c", To: "d", Points: []layouttest.Point{{X: 0, Y: 0}, {X: -100, Y: 100}}},
		},
	}
	if err := rule.Apply(shared); err != nil {
		t.Fatalf("edges sharing an endpoint should not be flagged as crossing, got %v", err)
	}
}

// A multi-ID rule referencing a nonexistent node must FAIL loudly — the
// selector used to silently drop missing IDs, shrinking assertions to
// vacuous truths (v6-assessment harness-integrity item).
func TestMultiIDRuleFailsOnMissingNode(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "a", Type: "event", X: 40, Y: 120, Width: 120, Height: 60},
			{ID: "b", Type: "event", X: 220, Y: 120, Width: 120, Height: 60},
		},
	}
	ok, err := Parse(1, "all #a,#b have same y")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ok.Apply(layout); err != nil {
		t.Fatalf("both nodes exist and align; rule should pass, got %v", err)
	}
	missing, err := Parse(1, "all #a,#ghost have same y")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	err = missing.Apply(layout)
	if err == nil {
		t.Fatalf("rule referencing #ghost must fail, not pass vacuously")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error should name the missing id, got: %v", err)
	}
}

func TestPositionalRuleRejectsBadDirection(t *testing.T) {
	// A bad direction must be rejected at parse time, not deferred to Apply.
	if _, err := Parse(1, "#x is sideways #y"); err == nil {
		t.Fatalf("expected parse error for invalid direction 'sideways'")
	}
	// Each valid direction must still parse.
	for _, dir := range []string{"above", "below", "left-of", "right-of"} {
		if _, err := Parse(1, "#x is "+dir+" #y"); err != nil {
			t.Fatalf("valid direction %q should parse, got: %v", dir, err)
		}
	}
}

// A parent-scope class rule over a diagram with none of that class must
// pass vacuously, not fail the fixture (the event-less
// taxonomy fixture met `each type=event has min-gap-to-others`). ID
// references stay strict — see the #ghost test above.
func TestTypeSelectorVacuousOnEmptyClass(t *testing.T) {
	layout := &layouttest.Layout{Nodes: []layouttest.Node{
		{ID: "a", Type: "concept", X: 0, Y: 0, Width: 120, Height: 60},
	}}
	rule, err := Parse(1, "each type=event has min-gap-to-others>=10")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := rule.Apply(layout); err != nil {
		t.Fatalf("class rule on empty class must pass vacuously, got: %v", err)
	}
}
