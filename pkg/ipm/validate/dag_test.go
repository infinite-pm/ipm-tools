package validate

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// --- IPMV2.1 PartOf DAG ---

func TestPartOfDag_FlagsCycle(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "a"), eventNode(2, "b"), eventNode(3, "c")},
		[]model.Edge{
			edge(4, 1, 2, model.PartOf),
			edge(5, 2, 3, model.PartOf),
			edge(6, 3, 1, model.PartOf),
		},
	)
	fs := PartOfDagCheck{}.Run(g)
	if len(fs) != 1 || fs[0].Severity != SeverityError {
		t.Fatalf("expected one cycle finding, got %v", fs)
	}
}

func TestPartOfDag_NoCycle(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "child"), eventNode(2, "mid"), eventNode(3, "parent")},
		[]model.Edge{
			edge(4, 1, 2, model.PartOf),
			edge(5, 2, 3, model.PartOf),
		},
	)
	if got := (PartOfDagCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("acyclic part-of should pass, got %v", got)
	}
}

// --- IPMV2.2 LeadsTo DAG ---

func TestLeadsToDag_FlagsCycle(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "a"), eventNode(2, "b")},
		[]model.Edge{
			edge(3, 1, 2, model.LeadsTo),
			edge(4, 2, 1, model.LeadsTo),
		},
	)
	if got := (LeadsToDagCheck{}).Run(g); len(got) != 1 {
		t.Fatalf("expected one cycle finding for 2-node loop, got %v", got)
	}
}

func TestLeadsToDag_NoCycle(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "a"), eventNode(2, "b"), eventNode(3, "c")},
		[]model.Edge{
			edge(4, 1, 2, model.LeadsTo),
			edge(5, 2, 3, model.LeadsTo),
		},
	)
	if got := (LeadsToDagCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("acyclic chain should pass, got %v", got)
	}
}

// --- IPMV2.6 happens-before composition ---

func TestHappensBefore_FlagsCompositeCycle(t *testing.T) {
	// a --::P--> A, A --> b, b --> a creates: a hb b (via rule 2), b hb a (direct).
	// Cycle: a → b → a.
	g := graphWith(
		[]model.Node{
			eventNode(1, "A"),
			eventNode(2, "a"),
			eventNode(3, "b"),
		},
		[]model.Edge{
			edge(4, 2, 1, model.PartOf),  // a part-of A
			edge(5, 1, 3, model.LeadsTo), // A --> b ⇒ a hb b (sub-event constraint)
			edge(6, 3, 2, model.LeadsTo), // b --> a directly
		},
	)
	fs := HappensBeforeCheck{}.Run(g)
	if len(fs) == 0 {
		t.Fatalf("expected at least one composite cycle finding, got %v", fs)
	}
	for _, f := range fs {
		if f.Severity != SeverityError {
			t.Errorf("expected error severity, got %s", f.Severity)
		}
	}
}

func TestHappensBefore_AcyclicPasses(t *testing.T) {
	// Pure LeadsTo chain: a → b → c. No PartOf. No cycle.
	g := graphWith(
		[]model.Node{eventNode(1, "a"), eventNode(2, "b"), eventNode(3, "c")},
		[]model.Edge{
			edge(4, 1, 2, model.LeadsTo),
			edge(5, 2, 3, model.LeadsTo),
		},
	)
	if got := (HappensBeforeCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("simple chain should pass, got %v", got)
	}
}
