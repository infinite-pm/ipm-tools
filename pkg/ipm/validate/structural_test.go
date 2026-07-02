package validate

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// Structural-invariant tests fabricate IpmGraph values directly to bypass
// the parser, which would catch many of these at parse time. The validator
// is the source of truth for graphs produced by any means (graphJSON,
// programmatic, third-party).

// graphWith builds a minimal IpmGraph from explicit nodes and edges.
func graphWith(nodes []model.Node, edges []model.Edge) *model.IpmGraph {
	return &model.IpmGraph{
		Version: "test",
		Nodes:   nodes,
		Edges:   edges,
		Src: model.Src{
			Positions: model.Positions{
				Nodes: map[int][2]int{},
				Edges: map[int]model.EdgePos{},
			},
		},
	}
}

func eventNode(id int, name string) model.Node {
	return model.Node{ID: id, Name: name, Type: model.Event}
}
func thingNode(id int, name string) model.Node {
	return model.Node{ID: id, Name: name, Type: model.Thing}
}
func conceptNode(id int, name string) model.Node {
	return model.Node{ID: id, Name: name, Type: model.Concept}
}

func edge(id, src, tgt int, rel model.SstLinkType) model.Edge {
	return model.Edge{
		ID:          id,
		Source:      src,
		Target:      tgt,
		Dir:         model.DirFwd,
		SstLinkType: rel,
	}
}

// --- IPMV1.1 Self-loop ---

func TestSelfLoop_Flags(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "ev")},
		[]model.Edge{edge(2, 1, 1, model.LeadsTo)},
	)
	fs := SelfLoopCheck{}.Run(g)
	if len(fs) != 1 || fs[0].Severity != SeverityError {
		t.Fatalf("expected one error finding, got %v", fs)
	}
}

func TestSelfLoop_NoFalsePositive(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "a"), eventNode(2, "b")},
		[]model.Edge{edge(3, 1, 2, model.LeadsTo)},
	)
	if got := (SelfLoopCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("expected no findings, got %v", got)
	}
}

// --- IPMV1.2 Duplicate edge ---

func TestDuplicatePair_FlagsTwoEdgesBetweenSamePair(t *testing.T) {
	g := graphWith(
		[]model.Node{thingNode(1, "x"), eventNode(2, "y")},
		[]model.Edge{
			edge(3, 1, 2, model.PartOf),
			edge(4, 1, 2, model.Expresses), // semantically wrong too, but tests dup
		},
	)
	fs := DuplicatePairCheck{}.Run(g)
	if len(fs) != 1 || fs[0].Severity != SeverityError {
		t.Fatalf("expected one error finding, got %v", fs)
	}
}

func TestDuplicatePair_UndirectedSymmetric(t *testing.T) {
	// Edge 1→2 and 2→1 are the same unordered pair.
	g := graphWith(
		[]model.Node{eventNode(1, "a"), eventNode(2, "b")},
		[]model.Edge{
			edge(3, 1, 2, model.LeadsTo),
			edge(4, 2, 1, model.LeadsTo),
		},
	)
	fs := DuplicatePairCheck{}.Run(g)
	if len(fs) != 1 {
		t.Fatalf("expected one finding for reverse-pair duplicate, got %v", fs)
	}
}

// --- IPMV1.3 Type-pair conformance ---

func TestTypePair_LeadsToBetweenEvents_OK(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "a"), eventNode(2, "b")},
		[]model.Edge{edge(3, 1, 2, model.LeadsTo)},
	)
	if got := (TypePairCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("expected no findings, got %v", got)
	}
}

func TestTypePair_LeadsToFromThing_Flagged(t *testing.T) {
	g := graphWith(
		[]model.Node{thingNode(1, "x"), eventNode(2, "y")},
		[]model.Edge{edge(3, 1, 2, model.LeadsTo)},
	)
	fs := TypePairCheck{}.Run(g)
	if len(fs) != 1 || fs[0].Severity != SeverityError {
		t.Fatalf("expected error, got %v", fs)
	}
}

func TestTypePair_PartOfTargetConcept_Flagged(t *testing.T) {
	g := graphWith(
		[]model.Node{thingNode(1, "x"), conceptNode(2, "C")},
		[]model.Edge{edge(3, 1, 2, model.PartOf)},
	)
	fs := TypePairCheck{}.Run(g)
	if len(fs) != 1 {
		t.Fatalf("expected one finding for thing → concept PartOf, got %v", fs)
	}
}

func TestTypePair_PartOfEventToThing_Flagged(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "ev"), thingNode(2, "x")},
		[]model.Edge{edge(3, 1, 2, model.PartOf)},
	)
	if got := (TypePairCheck{}).Run(g); len(got) != 1 {
		t.Fatalf("expected one finding for event → thing PartOf, got %v", got)
	}
}

func TestTypePair_ExpressesTargetNotConcept_Flagged(t *testing.T) {
	g := graphWith(
		[]model.Node{thingNode(1, "x"), thingNode(2, "y")},
		[]model.Edge{edge(3, 1, 2, model.Expresses)},
	)
	if got := (TypePairCheck{}).Run(g); len(got) != 1 {
		t.Fatalf("expected one finding for thing-expresses-thing, got %v", got)
	}
}

func TestTypePair_NearToDifferentTypes_Flagged(t *testing.T) {
	g := graphWith(
		[]model.Node{thingNode(1, "x"), conceptNode(2, "C")},
		[]model.Edge{edge(3, 1, 2, model.NearTo)},
	)
	if got := (TypePairCheck{}).Run(g); len(got) != 1 {
		t.Fatalf("expected one finding for cross-type NearTo, got %v", got)
	}
}

func TestTypePair_ExpressesConceptToConcept_OK(t *testing.T) {
	g := graphWith(
		[]model.Node{conceptNode(1, "sub"), conceptNode(2, "super")},
		[]model.Edge{edge(3, 1, 2, model.Expresses)},
	)
	if got := (TypePairCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("concept-expresses-concept (specialization) should pass, got %v", got)
	}
}

func TestTypePair_ExpressesEventToEvent_OK(t *testing.T) {
	// Spec γ(3,4) link type #3: `e4 --::X--> e5` (event expresses event) is valid.
	g := graphWith(
		[]model.Node{eventNode(1, "e4"), eventNode(2, "e5")},
		[]model.Edge{edge(3, 1, 2, model.Expresses)},
	)
	if got := (TypePairCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("event-expresses-event (spec link type #3) should pass, got %v", got)
	}
}
