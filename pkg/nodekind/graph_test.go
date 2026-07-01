package nodekind

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// ToGraph materializes a Result back into a model.IpmGraph. Lock down the ID
// arithmetic (nodes 1..N, edges len(Nodes)+i+1) and that each edge's
// Source/Target reference the right 1-based node IDs, since this mapping feeds
// production (pkg/mdembed/render.go) and is off-by-one-prone.
func TestToGraphIDsAndEdgeRefs(t *testing.T) {
	// deploy --LeadsTo--> ship (forced Event→Event), wheel --PartOf--> car.
	r := Solve(mg(
		me("deploy", "ship", model.LeadsTo),
		me("wheel", "car", model.PartOf),
	))
	g := ToGraph(r)

	if len(g.Nodes) != len(r.Nodes) {
		t.Fatalf("node count: got %d, want %d", len(g.Nodes), len(r.Nodes))
	}
	if len(g.Edges) != len(r.Edges) {
		t.Fatalf("edge count: got %d, want %d", len(g.Edges), len(r.Edges))
	}

	// Node IDs are 1..N in Result order; Name/Type/Candidates carry through.
	idByName := map[string]int{}
	for i, n := range g.Nodes {
		wantID := i + 1
		if n.ID != wantID {
			t.Errorf("node %d (%q) ID = %d, want %d", i, n.Name, n.ID, wantID)
		}
		if n.Name != r.Nodes[i].Name {
			t.Errorf("node %d Name = %q, want %q", i, n.Name, r.Nodes[i].Name)
		}
		if n.Type != r.Nodes[i].Type {
			t.Errorf("node %q Type = %s, want %s", n.Name, n.Type, r.Nodes[i].Type)
		}
		idByName[n.Name] = n.ID
	}

	// Edge IDs continue after the nodes (len(Nodes)+i+1) and Source/Target are
	// the 1-based IDs of the resolved endpoints with Dir/SstLinkType preserved.
	for i, e := range g.Edges {
		wantID := len(r.Nodes) + i + 1
		if e.ID != wantID {
			t.Errorf("edge %d ID = %d, want %d", i, e.ID, wantID)
		}
		re := r.Edges[i]
		if e.Source != re.Src+1 || e.Target != re.Tgt+1 {
			t.Errorf("edge %d refs = (%d,%d), want (%d,%d)", i, e.Source, e.Target, re.Src+1, re.Tgt+1)
		}
		if e.Dir != re.Dir || e.SstLinkType != re.Link {
			t.Errorf("edge %d dir/link = (%s,%s), want (%s,%s)", i, e.Dir, e.SstLinkType, re.Dir, re.Link)
		}
	}

	// Concretely: the LeadsTo edge must point deploy → ship by ID.
	var leads *model.Edge
	for i := range g.Edges {
		if g.Edges[i].SstLinkType == model.LeadsTo {
			leads = &g.Edges[i]
		}
	}
	if leads == nil {
		t.Fatal("LeadsTo edge missing after ToGraph")
	}
	if leads.Source != idByName["deploy"] || leads.Target != idByName["ship"] {
		t.Errorf("LeadsTo edge = (%d→%d), want (%d→%d)",
			leads.Source, leads.Target, idByName["deploy"], idByName["ship"])
	}
}
