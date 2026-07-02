package validate

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// --- IPMV2.9 leads-to must connect whole events, not sub-parts ---

// The motivating late-side-branch case: e1 --> e2 --> e3, but e2 is part-of
// e8. Both leads-to edges touching e2 are wrong; they should connect e8.
func TestLeadsToWholeEvent_FlagsSubpartEndpoint(t *testing.T) {
	g := graphWith(
		[]model.Node{
			eventNode(1, "e1"),
			eventNode(2, "e2"),
			eventNode(3, "e3"),
			eventNode(8, "e8"),
		},
		[]model.Edge{
			edge(10, 1, 2, model.LeadsTo), // e1 --> e2  (e2 is a sub-part target)
			edge(11, 2, 3, model.LeadsTo), // e2 --> e3  (e2 is a sub-part source)
			edge(12, 2, 8, model.PartOf),  // e2 --::P--> e8
		},
	)
	fs := LeadsToWholeEventCheck{}.Run(g)
	if len(fs) != 2 {
		t.Fatalf("expected 2 findings (e2 as target, e2 as source), got %d: %v", len(fs), fs)
	}
	for _, f := range fs {
		if f.Code != "IPMV2.9" {
			t.Errorf("expected IPMV2.9, got %s", f.Code)
		}
		if f.Severity != SeverityWarning {
			t.Errorf("expected warning severity, got %s", f.Severity)
		}
	}
}

// Two top-level events with no part-of parents: clean leads-to, no finding.
func TestLeadsToWholeEvent_TopLevelOK(t *testing.T) {
	g := graphWith(
		[]model.Node{
			eventNode(1, "e1"),
			eventNode(2, "e2"),
		},
		[]model.Edge{
			edge(3, 1, 2, model.LeadsTo),
		},
	)
	if got := (LeadsToWholeEventCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("top-level leads-to should pass, got %v", got)
	}
}

// Transitive nesting: y part-of m part-of z. A leads-to into y must resolve to
// the outermost ancestor z, and still flag.
func TestLeadsToWholeEvent_TransitiveSubpartFlags(t *testing.T) {
	g := graphWith(
		[]model.Node{
			eventNode(1, "a"),
			eventNode(2, "y"),
			eventNode(3, "m"),
			eventNode(4, "z"),
		},
		[]model.Edge{
			edge(5, 1, 2, model.LeadsTo), // a --> y
			edge(6, 2, 3, model.PartOf),  // y --::P--> m
			edge(7, 3, 4, model.PartOf),  // m --::P--> z
		},
	)
	fs := LeadsToWholeEventCheck{}.Run(g)
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding for transitive subpart, got %d: %v", len(fs), fs)
	}
	if fs[0].Code != "IPMV2.9" {
		t.Errorf("expected IPMV2.9, got %s", fs[0].Code)
	}
	// The message must name the outermost ancestor z, not the intermediate m.
	if want := "\"z\""; !strings.Contains(fs[0].Message, want) {
		t.Errorf("message should resolve outermost ancestor %s, got: %s", want, fs[0].Message)
	}
	if strings.Contains(fs[0].Message, "the whole event \"m\"") {
		t.Errorf("message should not name intermediate ancestor m as the whole event: %s", fs[0].Message)
	}
}

// Sibling sub-events sequenced INSIDE one container share the same outermost
// whole — the legitimate pattern IPMV2.7 recommends — so it must NOT flag.
func TestLeadsToWholeEvent_SameContainerSiblingChainOK(t *testing.T) {
	g := graphWith(
		[]model.Node{
			eventNode(1, "z"),
			eventNode(2, "z_a"),
			eventNode(3, "z_b"),
		},
		[]model.Edge{
			edge(4, 2, 3, model.LeadsTo), // z_a --> z_b (both sub-parts of z)
			edge(5, 2, 1, model.PartOf),  // z_a --::P--> z
			edge(6, 3, 1, model.PartOf),  // z_b --::P--> z
		},
	)
	if got := (LeadsToWholeEventCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("same-container sibling leads-to should pass (legit sub-event chain), got %v", got)
	}
}

// Leads-to between two top-level events that merely have sub-parts of their
// own is fine — only the endpoint being a sub-part matters.
func TestLeadsToWholeEvent_WholeEventsWithChildrenOK(t *testing.T) {
	g := graphWith(
		[]model.Node{
			eventNode(1, "whole1"),
			eventNode(2, "whole2"),
			eventNode(3, "child1"),
			eventNode(4, "child2"),
		},
		[]model.Edge{
			edge(5, 1, 2, model.LeadsTo), // whole1 --> whole2 (both top-level)
			edge(6, 3, 1, model.PartOf),  // child1 --::P--> whole1
			edge(7, 4, 2, model.PartOf),  // child2 --::P--> whole2
		},
	)
	if got := (LeadsToWholeEventCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("leads-to between whole events should pass, got %v", got)
	}
}
