package nodekind

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

func nodeByName(r *Result, name string) *ResolvedNode {
	for i := range r.Nodes {
		if r.Nodes[i].Name == name {
			return &r.Nodes[i]
		}
	}
	return nil
}

// Strict Solve leaves under-constrained nodes grey, but orders each candidate set
// by the role preference so the PRIMARY candidate is the likely kind: an Expresses
// target leads with Concept (::?ce), a PartOf participant with Thing (::?te).
func TestPreferenceOrderingStrict(t *testing.T) {
	r := Solve(mg(
		me("server", "reliability", model.Expresses), // reliability = Expresses target
		me("wheel", "car", model.PartOf),             // PartOf participants
	))
	cases := map[string]model.NodeType{
		"reliability": model.Concept, // Expresses target → Concept-preferred
		"wheel":       model.Thing,   // PartOf → Thing-preferred
		"car":         model.Thing,
		"server":      model.Thing, // Expresses source → Thing-preferred
	}
	for name, wantPrimary := range cases {
		n := nodeByName(r, name)
		if n == nil || n.Type != model.Unresolved || len(n.Candidates) == 0 {
			t.Errorf("%s: want grey with candidates, got %v", name, n)
			continue
		}
		if n.Candidates[0] != wantPrimary {
			t.Errorf("%s: primary candidate = %s, want %s (%v)", name, n.Candidates[0], wantPrimary, n.Candidates)
		}
	}
}

// SolveWithDefaults collapses every still-grey node to its role-preferred kind, so
// the graph is fully decided: an Expresses target → Concept, everything else →
// Thing. Event is never a default — only LeadsTo forces it (an event must have a
// causal/leads-to link). Every emitted edge stays γ(3,4)-valid.
func TestSolveWithDefaultsResolves(t *testing.T) {
	r := SolveWithDefaults(mg(
		me("server", "reliability", model.Expresses), // tXc
		me("wheel", "car", model.PartOf),             // tPt (container is a Thing, not an Event)
		me("lone", "other", model.NearTo),            // no leads-to → Thing, not Event
		me("deploy", "ship", model.LeadsTo),          // forced Event (the only Event-maker)
	))
	want := map[string]model.NodeType{
		"server": model.Thing, "reliability": model.Concept,
		"wheel": model.Thing, "car": model.Thing,
		"lone": model.Thing, "other": model.Thing,
		"deploy": model.Event, "ship": model.Event,
	}
	for name, k := range want {
		n := nodeByName(r, name)
		if n == nil || n.Type != k {
			t.Errorf("%s: got %v, want %s", name, n, k)
		}
	}
	for i := range r.Nodes {
		if r.Nodes[i].Type == model.Unresolved {
			t.Errorf("defaults mode left %q grey", r.Nodes[i].Name)
		}
	}
	// every edge must be a legal γ(3,4) pair under the decided kinds
	kindOf := func(idx int) model.NodeType { return r.Nodes[idx].Type }
	for _, e := range r.Edges {
		if !typePairValid(e.Link, kindOf(e.Src), kindOf(e.Tgt)) {
			t.Errorf("invalid edge after defaults: %s %s->%s", e.Link, kindOf(e.Src), kindOf(e.Tgt))
		}
	}
}
