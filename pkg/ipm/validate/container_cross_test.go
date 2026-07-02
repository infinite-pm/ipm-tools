package validate

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// The motivating UC-3 case: probe leads-to stalled, stalled is part-of
// task, and probe is outside task. The observation is anchored to the
// observed-thing's container while the observer dangles outside.
func TestLeadsToContainerCross_FlagsObserverFindingAsymmetry(t *testing.T) {
	g := graphWith(
		[]model.Node{
			eventNode(1, "probe-health-9"),
			eventNode(2, "stalled-9"),
			eventNode(3, "task-007"),
		},
		[]model.Edge{
			edge(4, 1, 2, model.LeadsTo), // probe → stalled
			edge(5, 2, 3, model.PartOf),  // stalled --::P--> task
		},
	)
	fs := LeadsToContainerCrossCheck{}.Run(g)
	if len(fs) != 1 || fs[0].Severity != SeverityWarning {
		t.Fatalf("expected one warning, got %v", fs)
	}
	if fs[0].Code != "IPMV2.8" {
		t.Errorf("expected IPMV2.8, got %s", fs[0].Code)
	}
}

// Same container: A and B both part-of P, A leads to B. Clean cascade.
func TestLeadsToContainerCross_SameContainerOK(t *testing.T) {
	g := graphWith(
		[]model.Node{
			eventNode(1, "a"),
			eventNode(2, "b"),
			eventNode(3, "P"),
		},
		[]model.Edge{
			edge(4, 1, 2, model.LeadsTo),
			edge(5, 1, 3, model.PartOf),
			edge(6, 2, 3, model.PartOf),
		},
	)
	if got := (LeadsToContainerCrossCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("same-container cascade should pass, got %v", got)
	}
}

// A leads-to the container of B (predecessor of the whole container).
// Suppresses the warning even though A is not part-of P.
func TestLeadsToContainerCross_AReachesContainerOK(t *testing.T) {
	g := graphWith(
		[]model.Node{
			eventNode(1, "a"),
			eventNode(2, "b"),
			eventNode(3, "P"),
		},
		[]model.Edge{
			edge(4, 1, 2, model.LeadsTo), // a → b
			edge(5, 2, 3, model.PartOf),  // b part-of P
			edge(6, 1, 3, model.LeadsTo), // a → P (predecessor of container)
		},
	)
	if got := (LeadsToContainerCrossCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("predecessor-of-container should pass, got %v", got)
	}
}

// B has no parent: no container to cross, no warning.
func TestLeadsToContainerCross_NoContainerOK(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "a"), eventNode(2, "b")},
		[]model.Edge{edge(3, 1, 2, model.LeadsTo)},
	)
	if got := (LeadsToContainerCrossCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("no container — should pass, got %v", got)
	}
}

// Multi-level: B is part-of P part-of G; A is part-of G but not part-of P.
// A → B "dips into" P from above. Worth warning.
func TestLeadsToContainerCross_MultiLevelDipFlags(t *testing.T) {
	g := graphWith(
		[]model.Node{
			eventNode(1, "a"),
			eventNode(2, "b"),
			eventNode(3, "P"),
			eventNode(4, "G"),
		},
		[]model.Edge{
			edge(5, 1, 2, model.LeadsTo),
			edge(6, 2, 3, model.PartOf), // b part-of P
			edge(7, 3, 4, model.PartOf), // P part-of G
			edge(8, 1, 4, model.PartOf), // a part-of G (but not part-of P)
		},
	)
	fs := LeadsToContainerCrossCheck{}.Run(g)
	if len(fs) != 1 {
		t.Fatalf("expected one warning (a in G but not in P, leads into b in P), got %v", fs)
	}
}
