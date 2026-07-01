package nodekind

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// directedAcyclic reports whether the non-NearTo edge set of r is a DAG (the
// post-breakCycles guarantee that IPMV2.1/2.2/2.6 depend on).
func directedAcyclic(r *Result) bool {
	adj := make(map[int][]int, len(r.Nodes))
	for _, e := range r.Edges {
		if e.Link == model.NearTo {
			continue
		}
		adj[e.Src] = append(adj[e.Src], e.Tgt)
	}
	const white, gray, black = 0, 1, 2
	color := make(map[int]int, len(r.Nodes))
	var hasCycle bool
	var dfs func(u int)
	dfs = func(u int) {
		color[u] = gray
		for _, v := range adj[u] {
			switch color[v] {
			case gray:
				hasCycle = true
			case white:
				dfs(v)
			}
		}
		color[u] = black
	}
	for i := range r.Nodes {
		if color[i] == white {
			dfs(i)
		}
	}
	return !hasCycle
}

// breakCycles must drop exactly one edge from a 3-node LeadsTo cycle and leave
// the remaining directed edges acyclic — the headline DAG guarantee that the
// existing tests only check indirectly (by matching the word "broke").
func TestBreakCyclesDropsOneAndAcyclic(t *testing.T) {
	r := Solve(mg(
		me("a", "b", model.LeadsTo),
		me("b", "c", model.LeadsTo),
		me("c", "a", model.LeadsTo),
	))
	if len(r.Edges) != 2 {
		t.Fatalf("3-cycle should leave 2 edges, got %d", len(r.Edges))
	}
	if !directedAcyclic(r) {
		t.Error("surviving directed edges still contain a cycle")
	}
	var broke int
	for _, rec := range r.Trace {
		if rec.Phase == "cycle" && rec.Event == "break" {
			broke++
		}
	}
	if broke != 1 {
		t.Errorf("expected exactly one cycle-break trace record, got %d", broke)
	}
}

// Strict Solve's output must always pass the γ(3,4) type-pair table (IPMV1.3)
// under each endpoint's effective type — the dropInvalidEdges guarantee — and
// the directed subset must be acyclic, even for a graph mixing every link kind,
// a cycle, duplicates and a self-loop.
func TestSolveStrictOutputValidAndAcyclic(t *testing.T) {
	r := Solve(mg(
		me("deploy", "ship", model.LeadsTo),   // forces Event→Event
		me("ship", "deploy", model.LeadsTo),   // back-edge → broken by breakCycles
		me("crowbar", "door", model.PartOf),   // grey participants → Thing-preferred
		me("door", "safety", model.Expresses), // Expresses target → Concept-preferred
		me("x", "y", model.NearTo),            // grey same-kind pair
		me("x", "x", model.LeadsTo),           // self-loop → dropped by dedupe
	))
	for _, e := range r.Edges {
		if !typePairValid(e.Link, r.effType(e.Src), r.effType(e.Tgt)) {
			t.Errorf("surviving edge violates IPMV1.3: %s %s(%s)->%s(%s)",
				e.Link, r.Nodes[e.Src].Name, r.effType(e.Src), r.Nodes[e.Tgt].Name, r.effType(e.Tgt))
		}
		if e.Src == e.Tgt {
			t.Errorf("self-loop survived on %q", r.Nodes[e.Src].Name)
		}
	}
	if !directedAcyclic(r) {
		t.Error("strict output directed edges are not acyclic")
	}
}

// A self-loop is dropped (IPMV1.1) with a warning; the report exposes it.
func TestSolveDropsSelfLoopWithWarning(t *testing.T) {
	r := Solve(mg(
		me("a", "a", model.LeadsTo),
		me("a", "b", model.LeadsTo),
	))
	if len(r.Edges) != 1 {
		t.Fatalf("self-loop should be dropped, got %d edges", len(r.Edges))
	}
	var sawSelfLoop bool
	for _, w := range r.Warnings {
		if strings.Contains(w, "self-loop") {
			sawSelfLoop = true
		}
	}
	if !sawSelfLoop {
		t.Errorf("expected a self-loop warning, got %v", r.Warnings)
	}
}
